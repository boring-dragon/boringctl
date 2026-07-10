# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's **Security** tab by
opening a private vulnerability report. Do not open a public issue containing
credentials, network topology, or exploit details.

Include the affected version, operating system, reproduction steps, and the
impact you observed. You should receive an acknowledgement within seven days.

## Operational security

`boringctl` controls Proxmox and can execute commands over SSH. Use a dedicated,
least-privilege Proxmox user and privilege-separated API token. Keep
`~/.config/boringctl/credentials.env` readable only by its owner, review every
command before passing `--yes`, and keep TLS verification enabled.
