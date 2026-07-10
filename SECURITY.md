# Security policy

## Supported versions

Only the latest release receives security fixes.

## Report a vulnerability

Use private vulnerability reporting in the repository's **Security** tab when
it is available. Do not open a public issue containing credentials, network
topology, exploit details, or an unpatched reproduction.

If private reporting is unavailable, open a minimal issue asking the
maintainers to provide a private contact channel. Do not include vulnerability
details in that issue.

Include the affected version, operating system, reproduction steps, and
observed impact. The project aims to acknowledge reports within seven days.

## Operational security

`boringctl` can provision Proxmox guests and execute commands over SSH. Use a
dedicated, least-privilege Proxmox user with a privilege-separated API token.
Keep `~/.config/boringctl/credentials.env` readable only by its owner, review
commands before passing `--yes`, and keep TLS and SSH host-key verification
enabled.

If a credential is committed or included in a report, revoke and replace it.
Removing it from Git history is not a substitute for rotation.
