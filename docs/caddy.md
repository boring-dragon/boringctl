# Caddy integration

Caddy support is optional. It manages route files in a local Git repository,
validates the staged tree, and deploys it to a configured LXC container.

Remove the `caddy` config block when the integration is not used.

## Configuration

```yaml
caddy:
  repo_path: "~/infrastructure"
  proxmox_ssh_host: "root@pve.example.com"
  container_id: 200
  ssh_options:
    - "-o"
    - "BatchMode=yes"
    - "-o"
    - "IdentityAgent=none"
    - "-o"
    - "StrictHostKeyChecking=yes"
  remote_archive_path: "/root/boringctl-caddy.tgz"
  default_domain: "example.com"
  common_proxy_snippet: ""
  internal_acl_snippet: "lan_only"
  public_waf_by_default: false
```

The repository must contain `caddy/Caddyfile` and any snippets referenced by
the site templates.

`internal_acl_snippet` is required for `visibility: internal`. This prevents a
route from being labeled internal without an actual network access rule.
`common_proxy_snippet` is optional. WAF generation is opt-in unless
`public_waf_by_default` is enabled for a Caddy build that provides the `waf`
directive.

The SSH identity must be able to connect to the Proxmox node and execute `pct`
against the configured container. Use non-interactive key authentication and a
pre-populated `known_hosts` file; do not disable host-key verification in CI.

## Manage routes

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

Run `boringctl caddy templates` for the current template list rather than
assuming a locally installed binary matches this documentation.

## Validate and deploy

```bash
boringctl caddy check
boringctl caddy deploy
boringctl caddy rollback --list
```

A deploy stages and validates the new tree before activation. It captures the
exact previous config and restores it automatically if live validation,
reload, or a scoped post-deploy smoke check fails. Manual rollback remains
available.
