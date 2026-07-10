# Domain context

## Terms

- **Cluster** — the Proxmox installation addressed by one boringctl profile.
- **Node** — a Proxmox host in the cluster.
- **Guest** — a QEMU virtual machine or LXC container managed through Proxmox.
- **Profile** — one YAML configuration selecting a cluster and its local catalog.
- **Doctor** — the read-only diagnostic module that checks local configuration
  and live integration health.
- **Operation journal** — the bounded local record for operations that Proxmox
  task history does not represent.
- **Managed Caddy tree** — the Git-owned Caddy configuration that `boringctl`
  stages, validates, deploys, and can restore.

## Boundaries

- Proxmox is the source of truth for nodes, guests, storage, and task state.
- Profiles are local connection settings and provisioning catalogs, not desired-state databases.
- The operation journal is bounded history, not reconciliation state.
- Caddy integration is optional and isolated behind the `caddy` config block.
- Commands that advertise name resolution may accept guest names. Mutation
  commands generally require numeric VMIDs.
