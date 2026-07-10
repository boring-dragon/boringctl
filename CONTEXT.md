# Domain context

## Terms

- **Cluster** — the Proxmox installation addressed by one boringctl profile.
- **Node** — a Proxmox host in the cluster.
- **Guest** — a QEMU virtual machine or LXC container managed through Proxmox.
- **Profile** — one YAML configuration selecting a cluster and its local catalog.
- **Doctor** — the read-only diagnostic module that checks local configuration and live integration health.
- **Operation journal** — the bounded local record for boringctl operations that Proxmox task history does not represent.
- **Managed Caddy tree** — the Git-owned Caddy configuration that boringctl stages, validates, deploys, and can restore.
