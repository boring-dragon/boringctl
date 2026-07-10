# Automation

## Output formats

`--output auto|text|json` controls command output. `auto` is the default and
switches to JSON when stdout is piped. JSON responses use `schema_version`,
snake_case fields, and structured errors.

```bash
boringctl --output text list
boringctl --output json list
```

Destructive commands require interactive confirmation in text mode. JSON mode
returns `confirmation_required` unless global `--yes` is supplied.

## Command discovery

Use `schema` instead of scraping help text:

```bash
boringctl schema
boringctl schema task
boringctl --output json schema shell
```

The schema includes commands, flags, output behavior, and safety metadata for
automation and agents.

## Proxmox tasks and raw API access

Commands that start asynchronous Proxmox work return a UPID. Inspect it with:

```bash
boringctl task status 'UPID:pve1:...'
boringctl task log 'UPID:pve1:...'
boringctl task wait 'UPID:pve1:...' --timeout 5m
```

The raw escape hatch accepts `get`, `post`, `put`, and `delete`:

```bash
boringctl api get /version
boringctl api get /nodes
boringctl api post /nodes/pve1/lxc/120/status/start --yes
```

Raw calls use the same credentials and output rules as the higher-level
commands. They do not bypass Proxmox authorization.

## Export and apply

Export a guest by numeric VMID and compare its deterministic spec:

```bash
boringctl --output text export guest 120 > guest-120.yaml
boringctl apply --file guest-120.yaml --dry-run
boringctl apply --file guest-120.yaml --yes
```

`apply` updates an existing guest only; it does not create one.

## Releases

A successful CI run for a push to `main` creates the next patch release. If no
release tag exists, automation starts at `v0.1.0`; otherwise it increments the
latest patch version. The release contains checksummed Linux and macOS archives
for amd64 and arm64.

The release workflow checks out the exact SHA that passed CI. Tag allocation is
serialized and reruns reuse an existing tag for the same commit.
