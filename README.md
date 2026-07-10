# boringctl

`boringctl` is a Proxmox CLI and terminal UI for provisioning, inspecting, and
operating QEMU virtual machines and LXC containers. Proxmox remains the source
of truth; `boringctl` does not require a control-plane database or resident
agent.

It includes:

- VM and LXC creation from a configurable template catalog
- Cluster inventory, lifecycle actions, tags, snapshots, backups, and restore
- Type-aware shell access to Proxmox nodes, containers, and VMs
- Task, storage, raw Proxmox API, export, and apply workflows
- A searchable TUI with live health and Proxmox RRD history
- Optional Git-managed Caddy routes with validation, smoke checks, recovery,
  and rollback
- Stable JSON output and command schema discovery for automation and agents
- Read-only diagnostics through `boringctl doctor`

## Install

Download an archive and `checksums.txt` from the GitHub release page, verify its
SHA-256 checksum, then place `boringctl` somewhere on your `PATH`.

With Go installed:

```bash
go install github.com/boring-labs/boringctl/cmd/boringctl@latest
```

Build from source:

```bash
go build -o boringctl ./cmd/boringctl
./boringctl version
```

Release binaries are built with a patched Go toolchain for Linux and macOS on
amd64 and arm64.

## Configure

Start from the neutral example:

```bash
mkdir -p ~/.config/boringctl
cp configs/boringctl.example.yaml ~/.config/boringctl/config.yaml
$EDITOR ~/.config/boringctl/config.yaml
```

The repository-level `boringctl.yaml` path is ignored so contributors can keep
a local development profile without publishing their infrastructure.

Use a dedicated, least-privilege Proxmox user and privilege-separated API
token. Supply credentials through the environment:

```bash
export PVE_TOKEN_ID='boringctl@pve!cli'
export PVE_TOKEN_SECRET='your-token-secret'
```

Or store them in `~/.config/boringctl/credentials.env`:

```bash
install -m 600 /dev/null ~/.config/boringctl/credentials.env
printf '%s\n' \
  'PVE_TOKEN_ID=boringctl@pve!cli' \
  'PVE_TOKEN_SECRET=your-token-secret' \
  > ~/.config/boringctl/credentials.env
```

Environment variables take priority. `boringctl doctor` reports unsafe
credential-file permissions.

TLS verification is enabled by default. For a private Proxmox CA, set:

```yaml
cluster:
  endpoint: "https://pve.example.com:8006"
  ca_file: "~/.config/boringctl/proxmox-ca.pem"
```

`insecure_tls: true` is available for isolated development environments but is
reported as a warning by the doctor.

### Discover a catalog

If credentials and the endpoint are configured, `init-config` can discover
nodes, active storage, and `tmpl-*` QEMU templates:

```bash
boringctl init-config
boringctl init-config --output ~/.config/boringctl/config.yaml --force
```

### Profiles

Store additional clusters as `~/.config/boringctl/<profile>.yaml`:

```bash
boringctl --profile lab list
boringctl --profile lab tui
BORINGCTL_PROFILE=lab boringctl doctor
```

`BORINGCTL_CONFIG` can point at an arbitrary config path.

## Diagnose

Run the read-only doctor before provisioning or after changing a cluster:

```bash
boringctl doctor
boringctl --output json doctor
```

It checks configuration, credential permissions, TLS posture, Proxmox
connectivity, configured nodes and SSH hosts, storage and template mappings,
guest-agent availability, and the optional local Caddy tree. Required failures
produce a non-zero exit status; advisory findings remain visible as warnings.

The narrower checks remain available:

```bash
boringctl config show
boringctl config check
```

## Provision guests

Preview and create a QEMU VM:

```bash
boringctl create \
  --node pve1 \
  --image ubuntu-24.04 \
  --plan small \
  --name api-01 \
  --storage local-lvm \
  --ssh-key default \
  --network dhcp \
  --dry-run

boringctl create \
  --node pve1 \
  --image ubuntu-24.04 \
  --plan small \
  --name api-01 \
  --storage local-lvm \
  --ssh-key default \
  --network dhcp
```

Create an LXC container:

```bash
boringctl create-lxc \
  --node pve1 \
  --image debian-13 \
  --plan tiny \
  --name tools-01 \
  --storage local-lvm \
  --ssh-key default \
  --network dhcp
```

`--docker` enables the token-safe `nesting=1` feature. Use `--keyctl` only when
the configured Proxmox identity is allowed to change it.

## Operate the cluster

```bash
boringctl list
boringctl list --node pve1 --status running --kind lxc
boringctl show api-01
boringctl start api-01
boringctl stop api-01
boringctl reboot api-01
boringctl rename api-01 api-02
boringctl tags api-02 --add production
boringctl snapshot api-02 before-upgrade
boringctl snapshot api-02 --rollback before-upgrade --yes
boringctl backup create api-02 --storage backup
boringctl delete api-02
```

Human-mode destructive actions prompt for confirmation. JSON mode returns a
`confirmation_required` error unless global `--yes` is explicitly supplied.

### Shell access

`shell` resolves the target and chooses the correct access path:

- Proxmox node: SSH to `nodes.<name>.ssh_host`
- LXC: SSH to its owning node and run `pct exec`
- QEMU VM: discover its guest-agent IP and SSH to the guest

```bash
boringctl shell pve1
boringctl shell node:pve1 -- pveversion
boringctl shell lxc:tools-01 -- uname -a
boringctl shell vm:api-01 --user ubuntu -- uptime
boringctl shell api-01 --print --output json
```

`ssh-config` can persist a VM alias in `~/.ssh/config`:

```bash
boringctl ssh-config api-01 --alias api
boringctl ssh-config api-01 --print
```

## Terminal UI

```bash
boringctl
boringctl tui
```

The TUI opens on a resource dashboard with weighted cluster CPU and memory,
deduplicated configured-storage usage, guest counts, per-node health, and recent Proxmox
RRD history. Guest lists show live CPU, memory, disk, status, and uptime; node
and guest details use btop-style braille charts for CPU, memory, disk, and
network activity. Press `/` to search guest lists and `r` to refresh the
dashboard.

If the terminal is smaller than the supported minimum, the TUI shows the
required and actual dimensions instead of rendering a broken layout.

## Caddy integration

Caddy integration is optional. Configure the `caddy` block only when you have a
Git repository containing `caddy/Caddyfile` and the snippets referenced by your
site templates.

```yaml
caddy:
  repo_path: "~/homelab"
  proxmox_ssh_host: "root@pve.example.com"
  container_id: 112
  ssh_options: ["-o", "BatchMode=yes", "-o", "IdentityAgent=none", "-o", "StrictHostKeyChecking=yes"]
  remote_archive_path: "/root/boringctl-caddy.tgz"
  default_domain: "example.com"
  common_proxy_snippet: ""
  internal_acl_snippet: "lan_only"
  public_waf_by_default: false
```

`internal_acl_snippet` is required for `visibility: internal`, preventing a
route from being labeled internal without an actual network access rule.
`common_proxy_snippet` is optional. WAF generation is opt-in unless
`public_waf_by_default` is enabled for a Caddy build that provides the `waf`
directive.

Manage routes:

```bash
boringctl caddy templates
boringctl caddy list
boringctl caddy add-site \
  --domain app.example.com \
  --target 192.0.2.50:3000 \
  --visibility internal \
  --type generic \
  --dry-run
boringctl caddy edit-site app.example.com --target 192.0.2.51:3000
boringctl caddy remove-site app.example.com
```

Validate and deploy the Git tree:

```bash
boringctl caddy check
boringctl caddy deploy
boringctl caddy rollback --list
```

Deployments stage and validate the new tree before activation, capture the exact
previous config, and restore it automatically when live validation, reload, or
scoped post-deploy smoke checks fail. Manual rollback remains available.

## Operation journal

Proxmox tasks remain the source of truth for Proxmox mutations. A bounded local
journal records operations such as Caddy deploy and rollback that are not fully
represented there:

```bash
boringctl journal
boringctl --output json journal
```

The journal is stored with owner-only permissions under
`~/.config/boringctl` and never records credentials or raw guest configuration.

## Automation and agents

`--output auto|text|json` controls output. `auto` emits JSON when stdout is
piped. JSON responses use `schema_version`, snake_case fields, and structured
errors.

Discover supported commands and their safety metadata:

```bash
boringctl schema
boringctl schema task
boringctl --output json schema shell
```

Inspect Proxmox tasks and storage:

```bash
boringctl task list --limit 20
boringctl task status 'UPID:pve1:...'
boringctl task log 'UPID:pve1:...'
boringctl task wait 'UPID:pve1:...' --timeout 5m
boringctl storage list --node pve1 --storage local --content vztmpl
```

The raw escape hatch accepts `get`, `post`, `put`, and `delete`:

```bash
boringctl api get /version
boringctl api get /nodes
boringctl api post /nodes/pve1/lxc/120/status/start --yes
```

Export and compare existing guest configuration:

```bash
boringctl --output text export guest api-01 > api-01.yaml
boringctl apply --file api-01.yaml --dry-run
boringctl apply --file api-01.yaml --yes
```

`apply` intentionally updates an existing guest only; it does not create one.

## Template builder

`scripts/build-node-templates.sh` creates a catalog of prepared QEMU templates
on a Proxmox node. It requires a trusted two-column SHA-256 manifest (`hash`
then `filename`) so mutable upstream image URLs are never installed unchecked.
Its storage, bridge, VMID range, image directory, and SSH key are configurable:

```bash
STORAGE=local-lvm \
BRIDGE=vmbr0 \
VMID_BASE=9000 \
SSH_KEY=/root/operator.pub \
CHECKSUMS_FILE=/root/cloud-image-sha256s.txt \
./scripts/build-node-templates.sh
```

Obtain each digest from the image publisher through a trusted channel, then
review the image URLs and template list before running the script as root.

## Development

```bash
gofmt -w $(rg --files -g '*.go')
go test ./...
go vet ./...
go build ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

`boringctl` is available under the [MIT License](LICENSE).
