# Configuration

`boringctl` reads a local YAML catalog and uses Proxmox as the live source of
truth. The config describes how to reach the cluster and which templates,
storage, plans, and SSH keys should appear in provisioning workflows.

Start with [the example config](../configs/boringctl.example.yaml).

## Config paths

The first matching source wins:

1. `--config <path>`
2. `BORINGCTL_CONFIG`
3. `--profile <name>` or `BORINGCTL_PROFILE`
4. `~/.config/boringctl/config.yaml`

Profiles are stored as `~/.config/boringctl/<profile>.yaml`:

```bash
boringctl --profile lab doctor
boringctl --profile lab tui
BORINGCTL_PROFILE=lab boringctl list
```

The repository-level `boringctl.yaml` path is ignored so a contributor can keep
a local development profile without publishing it.

## Credentials

Create a dedicated Proxmox user and a privilege-separated API token. The token
still needs ACLs for the operations it performs; privilege separation does not
copy the user's permissions onto the token automatically.

Set the environment variables named by `auth.token_id_env` and
`auth.token_secret_env`, or store them in
`~/.config/boringctl/credentials.env`. Environment values take precedence.

```bash
install -m 600 /dev/null ~/.config/boringctl/credentials.env
printf '%s\n' \
  'PVE_TOKEN_ID=boringctl@pve!cli' \
  'PVE_TOKEN_SECRET=your-token-secret' \
  > ~/.config/boringctl/credentials.env
```

`boringctl doctor` rejects credential files that are readable by other users.
Do not place token values in the YAML config.

There is no universal least-privilege role for every `boringctl` command.
Read-only inventory, guest provisioning, backups, and raw API access need
different Proxmox privileges. Scope the token to the paths and operations you
intend to use. For stricter separation, use different profiles and tokens for
read-only inspection and mutation workflows.

## TLS

TLS verification is enabled by default. For a private certificate authority,
set `cluster.ca_file`:

```yaml
cluster:
  endpoint: "https://pve.example.com:8006"
  ca_file: "~/.config/boringctl/proxmox-ca.pem"
```

`insecure_tls: true` is intended only for isolated development environments and
is reported as a warning by the doctor.

## Guest and SSH requirements

QEMU IP discovery and VM shell access require the QEMU guest agent to be
installed, enabled in Proxmox, and running inside the guest. Guest SSH also
requires network reachability and a public key accepted by the guest account.

Node and LXC shell access use `nodes.<name>.ssh_host`. The SSH identity must be
allowed to connect to the node and run `pct exec` for containers. Keep
`BatchMode=yes` and strict host-key checking in unattended workflows; add the
real host key to `known_hosts` before the first run.

## Discover a catalog

With the endpoint and credentials configured, `init-config` discovers cluster
nodes, active storage, and QEMU templates named `tmpl-*`:

```bash
boringctl init-config
boringctl init-config --output ~/.config/boringctl/config.yaml --force
```

Review the generated catalog before provisioning. LXC image entries and local
plan names remain operator choices.

## Validate the result

```bash
boringctl config show
boringctl config check
boringctl doctor
boringctl --output json doctor
```

The doctor checks credential permissions, TLS posture, Proxmox connectivity,
configured nodes and SSH hosts, storage and template mappings, guest-agent
availability, and the optional Caddy tree. Required failures return a non-zero
exit status; advisory findings are warnings.
