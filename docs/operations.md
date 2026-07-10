# Cluster operations

## Provision guests

Preview changes before creating a VM:

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
```

Remove `--dry-run` to create the guest. Create an LXC container with the same
catalog concepts:

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

## Inspect and mutate guests

`show` accepts a guest name or VMID. Mutation commands use a numeric VMID.

```bash
boringctl list
boringctl list --node pve1 --status running --kind lxc
boringctl show api-01
boringctl start 120
boringctl stop 120
boringctl reboot 120
boringctl rename 120 api-02
boringctl tags 120 --add production
boringctl snapshot 120 before-upgrade
boringctl snapshot 120 --rollback before-upgrade --yes
boringctl backup create 120 --storage backup
boringctl delete 120
```

Destructive actions prompt in an interactive terminal. JSON mode returns a
`confirmation_required` error unless global `--yes` is supplied.

## Shell access

`shell` resolves nodes, guest names, and VMIDs, then chooses the access path:

- Nodes use SSH through `nodes.<name>.ssh_host`.
- LXC containers use SSH to the owning node and `pct exec`.
- QEMU VMs use the guest-agent IP and SSH to the guest.

```bash
boringctl shell pve1
boringctl shell node:pve1 -- pveversion
boringctl shell lxc:tools-01 -- uname -a
boringctl shell vm:api-01 --user ubuntu -- uptime
boringctl shell api-01 --print --output json
```

Persist a VM alias in `~/.ssh/config` with its numeric VMID:

```bash
boringctl ssh-config 120 --alias api
boringctl ssh-config 120 --print
```

## Terminal UI

```bash
boringctl
boringctl tui
```

The dashboard shows weighted cluster CPU and memory, usage across configured
storage, guest counts, node health, and one hour of Proxmox RRD history. Guest
lists include live resource usage, status, and uptime. Detail screens use
braille charts for CPU, memory, disk, and network activity.

Press `/` to search guest lists and `r` to refresh. The minimum terminal size
is 80 × 24.

## Tasks, storage, and local history

```bash
boringctl task list --limit 20
boringctl task status 'UPID:pve1:...'
boringctl task log 'UPID:pve1:...'
boringctl task wait 'UPID:pve1:...' --timeout 5m
boringctl storage list --node pve1 --storage local --content vztmpl
```

Proxmox tasks remain the source of truth for Proxmox mutations. A bounded local
journal records operations, such as Caddy deploy and rollback, that are not
fully represented there:

```bash
boringctl journal
boringctl --output json journal
```

The journal is stored with owner-only permissions under
`~/.config/boringctl`. It does not record credentials or raw guest config.

## Build QEMU templates

[`scripts/build-node-templates.sh`](../scripts/build-node-templates.sh) creates
prepared QEMU templates on a Proxmox node. It requires a trusted two-column
SHA-256 manifest (`hash` then `filename`) so mutable image URLs are never
installed unchecked.

```bash
STORAGE=local-lvm \
BRIDGE=vmbr0 \
VMID_BASE=9000 \
SSH_KEY=/root/operator.pub \
CHECKSUMS_FILE=/root/cloud-image-sha256s.txt \
./scripts/build-node-templates.sh
```

Obtain each digest from the image publisher through a trusted channel. Review
the image URLs, storage, bridge, VMID range, and template list before running
the script as root.
