# Getting started

This guide takes a new Proxmox installation from an API token to a working
`boringctl doctor`, inventory view, and TUI. Start with read-only access. Add
mutation permissions only when you are ready to provision or change guests.

## Prerequisites

- A Proxmox VE API endpoint reachable over HTTPS, normally on port `8006`
- A terminal at least 80 columns by 24 rows for the TUI
- Go 1.25 or newer when building from source
- SSH access only if you want node, LXC, VM, or Caddy shell operations
- A QEMU template when you want to provision VMs

## 1. Install boringctl

Download the archive for your platform and `checksums.txt` from the
[latest release](https://github.com/boring-labs/boringctl/releases/latest),
verify its SHA-256 digest, and place `boringctl` on your `PATH`.

You can instead install it with Go:

```bash
go install github.com/boring-labs/boringctl/cmd/boringctl@latest
boringctl version
```

## 2. Create a read-only Proxmox token

Proxmox privilege-separated API tokens have their own ACLs. The token's
effective permissions are limited by both the backing user and token ACLs.
The following commands create a read-only identity suitable for `doctor`,
inventory commands, and the TUI:

```bash
pveum user add boringctl@pve
pveum acl modify / -user boringctl@pve -role PVEAuditor
pveum user token add boringctl@pve cli -privsep 1
pveum acl modify / -token 'boringctl@pve!cli' -role PVEAuditor
pveum user permissions boringctl@pve
pveum user token permissions boringctl@pve cli
```

Run these commands as a Proxmox administrator. Save the token secret when it
is displayed; Proxmox shows it only once. The full token ID is
`boringctl@pve!cli`.

This token cannot create, start, stop, back up, or delete guests. For mutation
workflows, use a separate profile and token with ACLs scoped to the paths and
operations you intend to allow. There is no safe universal provisioning role.
See the
[Proxmox VE Administration Guide](https://pve.proxmox.com/pve-docs/pve-admin-guide.pdf)
for the current ACL and API-token model.

## 3. Create the local configuration

From an extracted release archive or source checkout, copy the version-matched
example:

```bash
mkdir -p ~/.config/boringctl
cp configs/boringctl.example.yaml ~/.config/boringctl/config.yaml
$EDITOR ~/.config/boringctl/config.yaml
```

When installed with `go install`, download the neutral example from the
repository:

```bash
mkdir -p ~/.config/boringctl
curl -fsSL \
  https://raw.githubusercontent.com/boring-labs/boringctl/main/configs/boringctl.example.yaml \
  -o ~/.config/boringctl/config.yaml
$EDITOR ~/.config/boringctl/config.yaml
```

At minimum, replace the endpoint and catalog entries with values from your
cluster. A valid config currently needs at least one node, one QEMU image
mapping, and one plan, even when you only intend to use the dashboard.

```yaml
cluster:
  endpoint: "https://pve.example.com:8006"

auth:
  token_id_env: "PVE_TOKEN_ID"
  token_secret_env: "PVE_TOKEN_SECRET"

defaults:
  bridge: "vmbr0"
  cpu_type: "host"
  full_clone: true
  ssh_key: "default"
  ssh_options: ["-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"]
  network: "dhcp"

nodes:
  pve1:
    label: "Primary node"
    ssh_host: ""
    storages: ["local-lvm"]

storages:
  local-lvm:
    label: "Local LVM"

images:
  ubuntu-24.04:
    label: "Ubuntu 24.04 LTS"
    family: "Ubuntu"
    default_user: "ubuntu"
    recommended: true
    templates:
      pve1: 9000

plans:
  small:
    label: "Small"
    cores: 2
    memory_mb: 4096
    disk_gb: 40

ssh_keys:
  default:
    path: "~/.ssh/id_ed25519.pub"
```

Remove the entire `caddy` block from the example when you do not use the
optional Caddy integration.

Create the credentials file without putting the token secret in a shell
command or shell history:

```bash
install -m 600 /dev/null ~/.config/boringctl/credentials.env
$EDITOR ~/.config/boringctl/credentials.env
```

Enter these two lines in the editor:

```dotenv
PVE_TOKEN_ID=boringctl@pve!cli
PVE_TOKEN_SECRET=replace-with-the-token-secret
```

`doctor` rejects a credentials file that is readable by other users. Never put
the secret in the YAML config.

## 4. Prepare a VM template

The `images.<name>.templates` value is the VMID of an existing QEMU template on
each node. VM shell access and reliable IP discovery require the QEMU guest
agent to be installed in the image, enabled in Proxmox, and running in the
guest. SSH also needs a reachable network and the configured public key.

The repository includes
[`scripts/build-node-templates.sh`](../scripts/build-node-templates.sh) for
building checksum-verified cloud-image templates. Review every setting before
running it as root; the [operations guide](operations.md#build-qemu-templates)
describes its inputs.

LXC support is optional. Add `lxc_images` entries only when you have matching
Proxmox volume IDs such as
`local:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst`.

## 5. Validate before changing anything

Run the checks in this order:

```bash
boringctl config show
boringctl config check
boringctl doctor
boringctl list
boringctl tui
```

`config show` redacts credential values. `doctor` exits non-zero for required
failures and reports advisory findings as warnings. Warnings about an omitted
Caddy integration, unconfigured node SSH, or unavailable guest agents are
expected when you do not use those features.

## 6. Make the first change safely

Switch to a deliberately scoped mutation profile and preview the request:

```bash
boringctl --profile provisioning create \
  --node pve1 \
  --image ubuntu-24.04 \
  --plan small \
  --name api-01 \
  --storage local-lvm \
  --ssh-key default \
  --network dhcp \
  --dry-run
```

Remove `--dry-run` only after reviewing the plan. Guest mutation commands use
numeric VMIDs:

```bash
boringctl show api-01
boringctl start 120
boringctl snapshot 120 before-upgrade
```

## Optional: shell access

Node and LXC shell access needs `nodes.<name>.ssh_host`, a trusted host key, and
an SSH identity allowed to connect to the node. LXC access runs `pct exec` on
that node. QEMU shell access uses the guest-agent IP and then connects directly
to the guest over SSH.

The default SSH options keep connections non-interactive and enforce host-key
verification. Add each real host key to `known_hosts` before first use. Inspect
the exact command without executing it:

```bash
boringctl shell node:pve1 --print
boringctl shell vm:api-01 --user ubuntu --print
```

## Optional: assisted catalog discovery

`init-config` is not a zero-config bootstrap. It first loads an already valid
config and credentials so it can reach Proxmox. It then discovers nodes, active
storage, and QEMU templates whose names begin with `tmpl-`:

```bash
boringctl init-config --output ./discovered.yaml
$EDITOR ./discovered.yaml
```

Review and merge the result instead of overwriting your only working config.
Discovered node SSH hosts are blank, storage may need pruning, plans remain
local choices, and no image entries appear when the cluster has no matching
`tmpl-*` templates. LXC images are not discovered.

## Troubleshooting

- **Certificate error:** set `cluster.ca_file` to your private CA certificate.
  Avoid `insecure_tls` outside isolated development.
- **401 response:** check the complete token ID, secret, realm, and environment
  variable names.
- **403 response:** inspect ACLs on both the backing user and the
  privilege-separated token.
- **Credentials permission failure:** run
  `chmod 600 ~/.config/boringctl/credentials.env`.
- **Template or storage drift:** update the catalog VMID, volume ID, storage
  name, or node mapping to match live Proxmox inventory.
- **Guest-agent warning:** install and start `qemu-guest-agent` in the guest and
  enable the agent for that VM in Proxmox.
- **SSH host-key failure:** verify the destination out of band, then add its
  actual key to `known_hosts`; do not disable strict checking as a shortcut.
- **TUI resize message:** enlarge the terminal to at least 80 × 24.

Continue with [configuration](configuration.md),
[cluster operations](operations.md), [Caddy integration](caddy.md), or
[automation](automation.md).
