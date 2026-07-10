package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var caddyBackupPattern = regexp.MustCompile(`^caddy\.backup-[0-9]{8}-[0-9]{6}(?:-[0-9]+)?$`)

func (service *Service) DeployCaddy(ctx context.Context, apply bool, reporter Reporter) (CaddyDeployResult, error) {
	return service.DeployCaddyWithOptions(ctx, CaddyDeployOptions{Apply: apply, Smoke: apply}, reporter)
}

func (service *Service) DeployCaddyWithOptions(ctx context.Context, options CaddyDeployOptions, reporter Reporter) (result CaddyDeployResult, resultErr error) {
	transactionLock, err := service.acquireCaddyTransactionLock()
	if err != nil {
		return result, err
	}
	defer releaseCaddyTransactionLock(transactionLock, reporter)

	return service.deployCaddyWithOptions(ctx, options, reporter)
}

func (service *Service) deployCaddyWithOptions(ctx context.Context, options CaddyDeployOptions, reporter Reporter) (result CaddyDeployResult, resultErr error) {
	if options.Apply {
		defer func() {
			status := "completed"
			summary := "Caddy config deployed and reloaded"
			if resultErr != nil {
				status = "failed"
				summary = "Caddy deploy failed"
				if result.RolledBack {
					summary = "Caddy deploy failed; previous config restored"
				}
			}
			recordOperation(reporter, OperationJournalEntry{
				Operation: "caddy_deploy",
				Status:    status,
				Target:    result.Backup,
				Summary:   summary,
			})
		}()
	}

	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return result, err
	}

	caddyPath := filepath.Join(repoPath, "caddy")
	if _, err := os.Stat(filepath.Join(caddyPath, "Caddyfile")); err != nil {
		return result, fmt.Errorf("caddy repo path is not valid: %w", err)
	}

	var smokeSelection caddySmokeSelection
	if options.Apply && options.Smoke {
		smokeSelection, err = service.changedManagedCaddyDomains(ctx)
		if err != nil {
			return result, err
		}
	}

	operationID := caddyOperationID()
	archivePath := filepath.Join(os.TempDir(), "boringctl-caddy-"+operationID+".tgz")
	remoteArchivePath := service.Config.Caddy.RemoteArchivePath + "." + operationID
	defer os.Remove(archivePath)

	report(reporter, "Packaging caddy config from git working tree")
	if err := runCommand(ctx, repoPath, []string{"tar", "--no-xattrs", "-C", caddyPath, "-czf", archivePath, "."}, []string{"COPYFILE_DISABLE=1"}); err != nil {
		return result, err
	}

	report(reporter, "Uploading package to Proxmox host")
	if err := service.runSCP(ctx, archivePath, service.Config.Caddy.ProxmoxSSHHost+":"+remoteArchivePath); err != nil {
		return result, err
	}
	defer service.cleanupCaddyStaging(ctx, remoteArchivePath, operationID, reporter)

	report(reporter, "Staging config inside caddy LXC")
	if err := service.runSSH(ctx, service.proxmoxStageCommand(remoteArchivePath, operationID)); err != nil {
		return result, err
	}

	report(reporter, "Validating staged config as caddy user")
	if err := service.runSSH(ctx, service.proxmoxValidateCommand(caddyValidatePath(operationID)+"/Caddyfile")); err != nil {
		return result, err
	}

	result = CaddyDeployResult{
		Validated: true,
		Summary:   "staged config validated",
	}
	if !options.Apply {
		return result, nil
	}

	report(reporter, "Backing up the live Caddy config")
	backup, err := service.createCaddyBackup(ctx)
	if err != nil {
		return result, err
	}
	result.Backup = backup
	report(reporter, fmt.Sprintf("Captured /etc/%s", backup))

	report(reporter, "Applying staged config and reloading Caddy")
	if err := service.runSSH(ctx, service.proxmoxApplyCommand(operationID)); err != nil {
		return service.recoverFailedCaddyDeploy(ctx, result, err, reporter)
	}

	result.Applied = true
	result.Summary = "caddy config deployed and reloaded"
	if options.Smoke && smokeSelection.Mode != caddySmokeNone {
		if smokeSelection.Mode == caddySmokeSelected {
			report(reporter, fmt.Sprintf("Smoke checking %d changed managed route(s)", len(smokeSelection.Domains)))
		} else {
			report(reporter, "Smoke checking all managed routes")
		}
		result.Smoke, err = service.SmokeCaddyDomains(ctx, smokeSelection.Domains)
		if err != nil {
			return service.recoverFailedCaddyDeploy(ctx, result, fmt.Errorf("smoke checks: %w", err), reporter)
		}
		if caddySmokeResultsFailed(result.Smoke) {
			return service.recoverFailedCaddyDeploy(ctx, result, fmt.Errorf("one or more Caddy routes failed smoke checks"), reporter)
		}
	} else if options.Smoke {
		report(reporter, "No affected managed routes to smoke check")
	}

	return result, nil
}

func caddyOperationID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

func caddyStagingPath(operationID string) string {
	return "/etc/caddy.new-" + operationID
}

func caddyValidatePath(operationID string) string {
	return "/etc/caddy.validate-" + operationID
}

func caddyContainerArchivePath(operationID string) string {
	return "/root/boringctl-caddy-" + operationID + ".tgz"
}

func (service *Service) cleanupCaddyStaging(ctx context.Context, remoteArchivePath string, operationID string, reporter Reporter) {
	cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelCleanup()
	if err := service.runSSH(cleanupContext, service.proxmoxCleanupStagingCommand(remoteArchivePath, operationID)); err != nil {
		report(reporter, fmt.Sprintf("Warning: Caddy staging cleanup failed: %v", err))
	}
}

func (service *Service) acquireCaddyTransactionLock() (*localFileLock, error) {
	journalPath, err := DefaultOperationJournalPath()
	if err != nil {
		return nil, err
	}
	lockDirectory := filepath.Dir(journalPath)
	if err := os.MkdirAll(lockDirectory, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(lockDirectory, 0o700); err != nil {
		return nil, err
	}

	return acquireLocalFileLock(filepath.Join(lockDirectory, "caddy.lock"))
}

func releaseCaddyTransactionLock(transactionLock *localFileLock, reporter Reporter) {
	if err := transactionLock.Close(); err != nil {
		report(reporter, fmt.Sprintf("Warning: Caddy transaction lock could not be released: %v", err))
	}
}

func (service *Service) recoverFailedCaddyDeploy(ctx context.Context, result CaddyDeployResult, deployErr error, reporter Reporter) (CaddyDeployResult, error) {
	report(reporter, fmt.Sprintf("Deploy failed; restoring /etc/%s", result.Backup))
	rollbackContext, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancelRollback()
	rollbackResult, rollbackErr := service.restoreCaddyBackup(rollbackContext, result.Backup, reporter)
	result.Applied = false
	result.RolledBack = rollbackResult.Applied
	result.RollbackSummary = rollbackResult.Summary
	if rollbackErr != nil {
		result.Summary = "caddy deploy failed and automatic rollback failed"
		return result, fmt.Errorf("caddy deploy failed: %w; automatic rollback of %s failed: %v", deployErr, result.Backup, rollbackErr)
	}

	result.Summary = "caddy deploy failed; previous config restored"
	return result, fmt.Errorf("caddy deploy failed: %w; restored %s", deployErr, result.Backup)
}

func (service *Service) createCaddyBackup(ctx context.Context) (string, error) {
	output, err := service.runSSHOutput(ctx, service.proxmoxCreateBackupCommand())
	if err != nil {
		return "", err
	}

	backup := strings.TrimSpace(output)
	if !caddyBackupPattern.MatchString(backup) {
		return "", fmt.Errorf("remote Caddy backup returned invalid name %q", backup)
	}

	return backup, nil
}

func (service *Service) CaddyGitChanges(ctx context.Context) ([]string, error) {
	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return nil, err
	}

	output, err := runCommandOutput(ctx, repoPath, []string{"git", "status", "--short", "--", "caddy"})
	if err != nil {
		return nil, err
	}

	var changes []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			changes = append(changes, line)
		}
	}

	return changes, nil
}

func (service *Service) ListCaddyBackups(ctx context.Context) ([]string, error) {
	output, err := service.runSSHOutput(ctx, service.proxmoxListBackupsCommand())
	if err != nil {
		return nil, err
	}

	var backups []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if caddyBackupPattern.MatchString(line) {
			backups = append(backups, line)
		}
	}

	sort.Strings(backups)
	return backups, nil
}

func (service *Service) RollbackCaddy(ctx context.Context, backup string, reporter Reporter) (result CaddyRollbackResult, resultErr error) {
	backup = strings.TrimSpace(backup)
	if !caddyBackupPattern.MatchString(backup) {
		return CaddyRollbackResult{}, fmt.Errorf("invalid caddy backup name %q", backup)
	}
	transactionLock, err := service.acquireCaddyTransactionLock()
	if err != nil {
		return result, err
	}
	defer releaseCaddyTransactionLock(transactionLock, reporter)

	defer func() {
		status := "completed"
		summary := "Caddy backup restored and reloaded"
		if resultErr != nil {
			status = "failed"
			summary = "Caddy rollback failed"
		}
		recordOperation(reporter, OperationJournalEntry{
			Operation: "caddy_rollback",
			Status:    status,
			Target:    backup,
			Summary:   summary,
		})
	}()

	return service.restoreCaddyBackup(ctx, backup, reporter)
}

func (service *Service) restoreCaddyBackup(ctx context.Context, backup string, reporter Reporter) (CaddyRollbackResult, error) {
	report(reporter, fmt.Sprintf("Restoring /etc/%s", backup))
	if err := service.runSSH(ctx, service.proxmoxRollbackCommand(backup, caddyOperationID())); err != nil {
		return CaddyRollbackResult{Backup: backup}, err
	}

	return CaddyRollbackResult{Backup: backup, Applied: true, Summary: "caddy backup restored and reloaded"}, nil
}

func (service *Service) runSCP(ctx context.Context, source string, destination string) error {
	args := append([]string{}, service.Config.Caddy.SSHOptions...)
	args = append(args, source, destination)
	return runCommand(ctx, "", append([]string{"scp"}, args...), nil)
}

func (service *Service) runSSH(ctx context.Context, remoteCommand string) error {
	args := append([]string{}, service.Config.Caddy.SSHOptions...)
	args = append(args, service.Config.Caddy.ProxmoxSSHHost, remoteCommand)
	return runCommand(ctx, "", append([]string{"ssh"}, args...), nil)
}

func (service *Service) runSSHOutput(ctx context.Context, remoteCommand string) (string, error) {
	args := append([]string{}, service.Config.Caddy.SSHOptions...)
	args = append(args, service.Config.Caddy.ProxmoxSSHHost, remoteCommand)
	return runCommandOutput(ctx, "", append([]string{"ssh"}, args...))
}

func (service *Service) proxmoxStageCommand(remoteArchivePath string, operationID string) string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	archivePath := shellQuote(remoteArchivePath)
	containerArchivePath := caddyContainerArchivePath(operationID)
	stagingPath := caddyStagingPath(operationID)
	validatePath := caddyValidatePath(operationID)
	stageCommand := "set -eu; trap 'rm -f " + containerArchivePath + "' EXIT; rm -rf " + stagingPath + " " + validatePath + "; mkdir -p " + stagingPath + "; tar --warning=no-unknown-keyword -C " + stagingPath + " -xzf " + containerArchivePath + "; find " + stagingPath + " -name '._*' -delete; cp -a " + stagingPath + " " + validatePath + "; find " + validatePath + " -type f \\( -name '*.caddy' -o -name 'Caddyfile' \\) -exec sed -i 's#/etc/caddy/waf/#" + validatePath + "/waf/#g' {} +; chown -R caddy:caddy /var/log/caddy " + stagingPath + " " + validatePath
	return "pct push " + containerID + " " + archivePath + " " + containerArchivePath + " && rm -f " + archivePath + " && pct exec " + containerID + " -- sh -lc " + shellQuote(stageCommand)
}

func (service *Service) proxmoxValidateCommand(configPath string) string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	return "pct exec " + containerID + " -- sh -lc " + shellQuote("runuser -u caddy -- caddy validate --config "+configPath)
}

func (service *Service) proxmoxCreateBackupCommand() string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	command := "set -eu; backup=caddy.backup-$(date +%Y%m%d-%H%M%S)-$$; cp -a /etc/caddy /etc/$backup; printf '%s\\n' \"$backup\""
	return "pct exec " + containerID + " -- sh -lc " + shellQuote(command)
}

func (service *Service) proxmoxApplyCommand(operationID string) string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	stagingPath := caddyStagingPath(operationID)
	validatePath := caddyValidatePath(operationID)
	command := "set -eu; rm -rf /etc/caddy; mv " + stagingPath + " /etc/caddy; rm -rf " + validatePath + "; mkdir -p /srv/caddy; if [ -d /etc/caddy/www ]; then cp -a /etc/caddy/www/. /srv/caddy/; fi; if [ -f /etc/caddy/docs/reverse-proxy.md ]; then cp /etc/caddy/docs/reverse-proxy.md /etc/caddy/README.md; fi; chown -R caddy:caddy /etc/caddy /var/log/caddy /srv/caddy; runuser -u caddy -- caddy validate --config /etc/caddy/Caddyfile; systemctl reload caddy; systemctl is-active caddy"
	return "pct exec " + containerID + " -- sh -lc " + shellQuote(command)
}

func (service *Service) proxmoxCleanupStagingCommand(remoteArchivePath string, operationID string) string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	cleanupCommand := "rm -rf " + caddyStagingPath(operationID) + " " + caddyValidatePath(operationID) + "; rm -f " + caddyContainerArchivePath(operationID)
	return "rm -f " + shellQuote(remoteArchivePath) + "; pct exec " + containerID + " -- sh -lc " + shellQuote(cleanupCommand)
}

func (service *Service) proxmoxListBackupsCommand() string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	return "pct exec " + containerID + " -- sh -lc " + shellQuote("find /etc -maxdepth 1 -type d -name 'caddy.backup-*' -printf '%f\n' | sort")
}

func (service *Service) proxmoxRollbackCommand(backup string, operationID string) string {
	containerID := strconv.Itoa(service.Config.Caddy.ContainerID)
	backupPath := "/etc/" + backup
	rollbackPath := "/etc/caddy.rollback-" + operationID
	validatePath := rollbackPath + ".validate"
	command := "set -eu; trap 'rm -rf " + rollbackPath + " " + validatePath + "' EXIT; test -d " + backupPath + "; rm -rf " + rollbackPath + " " + validatePath + "; cp -a " + backupPath + " " + rollbackPath + "; cp -a " + backupPath + " " + validatePath + "; find " + validatePath + " -type f \\( -name '*.caddy' -o -name 'Caddyfile' \\) -exec sed -i 's#/etc/caddy/waf/#" + validatePath + "/waf/#g' {} +; chown -R caddy:caddy " + rollbackPath + " " + validatePath + "; runuser -u caddy -- caddy validate --config " + validatePath + "/Caddyfile; rm -rf " + validatePath + "; previous=/etc/caddy.backup-$(date +%Y%m%d-%H%M%S)-$$; mv /etc/caddy \"$previous\"; mv " + rollbackPath + " /etc/caddy; chown -R caddy:caddy /etc/caddy /var/log/caddy /srv/caddy; if systemctl reload caddy && systemctl is-active caddy; then exit 0; else rollback_status=$?; rm -rf /etc/caddy; mv \"$previous\" /etc/caddy; chown -R caddy:caddy /etc/caddy /var/log/caddy /srv/caddy; systemctl reload caddy; systemctl is-active caddy; exit \"$rollback_status\"; fi"
	return "pct exec " + containerID + " -- sh -lc " + shellQuote(command)
}

type caddySmokeMode string

const (
	caddySmokeNone     caddySmokeMode = "none"
	caddySmokeSelected caddySmokeMode = "selected"
	caddySmokeAll      caddySmokeMode = "all"
)

type caddySmokeSelection struct {
	Mode    caddySmokeMode
	Domains []string
}

func (service *Service) changedManagedCaddyDomains(ctx context.Context) (caddySmokeSelection, error) {
	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return caddySmokeSelection{}, err
	}

	trackedOutput, err := runCommandOutput(ctx, repoPath, []string{"git", "diff", "--name-only", "HEAD", "--", "caddy"})
	if err != nil {
		return caddySmokeSelection{}, err
	}
	untrackedOutput, err := runCommandOutput(ctx, repoPath, []string{"git", "ls-files", "--others", "--exclude-standard", "--", "caddy"})
	if err != nil {
		return caddySmokeSelection{}, err
	}

	changedPaths := make(map[string]bool)
	for _, output := range []string{trackedOutput, untrackedOutput} {
		for _, path := range strings.Split(output, "\n") {
			path = strings.TrimSpace(strings.TrimPrefix(filepath.ToSlash(path), "caddy/"))
			if path != "" {
				changedPaths[path] = true
			}
		}
	}
	if len(changedPaths) == 0 {
		return caddySmokeSelection{Mode: caddySmokeAll}, nil
	}

	currentManifest, err := service.loadCaddyManifest()
	if err != nil {
		return caddySmokeSelection{}, err
	}
	baselineManifest, err := loadBaselineCaddyManifest(ctx, repoPath)
	if err != nil {
		return caddySmokeSelection{}, err
	}

	currentSites := caddyManifestSitesByDomain(currentManifest)
	baselineSites := caddyManifestSitesByDomain(baselineManifest)
	domains := make(map[string]bool)
	matchedPaths := make(map[string]bool)
	if changedPaths["manifest.yaml"] {
		for domain, currentSite := range currentSites {
			if baselineSite, exists := baselineSites[domain]; !exists || currentSite != baselineSite {
				domains[domain] = true
			}
		}
		for domain, baselineSite := range baselineSites {
			if currentSite, exists := currentSites[domain]; !exists || currentSite != baselineSite {
				domains[domain] = true
			}
		}
	}

	for _, sites := range []map[string]caddyManifestSite{currentSites, baselineSites} {
		for domain, site := range sites {
			sitePath := filepath.ToSlash(site.SitePath)
			rulePath := filepath.ToSlash(site.RulePath)
			if changedPaths[sitePath] || (rulePath != "" && changedPaths[rulePath]) {
				domains[domain] = true
			}
			matchedPaths[sitePath] = true
			if rulePath != "" {
				matchedPaths[rulePath] = true
			}
		}
	}

	for path := range changedPaths {
		if path == "manifest.yaml" || matchedPaths[path] || strings.HasPrefix(path, "sites/") {
			continue
		}
		return caddySmokeSelection{Mode: caddySmokeAll}, nil
	}

	result := make([]string, 0, len(domains))
	for domain := range domains {
		if _, stillManaged := currentSites[domain]; stillManaged {
			result = append(result, domain)
		}
	}
	sort.Strings(result)
	if len(result) == 0 {
		return caddySmokeSelection{Mode: caddySmokeNone}, nil
	}
	return caddySmokeSelection{Mode: caddySmokeSelected, Domains: result}, nil
}

func loadBaselineCaddyManifest(ctx context.Context, repoPath string) (caddyManifest, error) {
	if _, err := runCommandOutput(ctx, repoPath, []string{"git", "cat-file", "-e", "HEAD:caddy/manifest.yaml"}); err != nil {
		return caddyManifest{}, nil
	}

	fileContents, err := runCommandOutput(ctx, repoPath, []string{"git", "show", "HEAD:caddy/manifest.yaml"})
	if err != nil {
		return caddyManifest{}, err
	}

	var manifest caddyManifest
	if err := yaml.Unmarshal([]byte(fileContents), &manifest); err != nil {
		return caddyManifest{}, fmt.Errorf("parse committed Caddy manifest: %w", err)
	}
	return manifest, nil
}

func caddyManifestSitesByDomain(manifest caddyManifest) map[string]caddyManifestSite {
	sites := make(map[string]caddyManifestSite, len(manifest.Sites))
	for _, site := range manifest.Sites {
		sites[site.Domain] = site
	}
	return sites
}

func runCommand(ctx context.Context, dir string, args []string, env []string) error {
	_, err := runCommandWithOutput(ctx, dir, args, env)
	return err
}

func runCommandOutput(ctx context.Context, dir string, args []string) (string, error) {
	return runCommandWithOutput(ctx, dir, args, nil)
}

func runCommandWithOutput(ctx context.Context, dir string, args []string, env []string) (string, error) {
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = dir
	command.Env = os.Environ()
	command.Env = append(command.Env, env...)

	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output

	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}

	return output.String(), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
