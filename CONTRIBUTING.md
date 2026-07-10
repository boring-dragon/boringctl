# Contributing

Contributions are welcome when they keep `boringctl` focused on Proxmox and its
closely related operational workflows.

Before opening a pull request:

Use Go 1.25 or newer, then run:

```bash
gofmt -w $(rg --files -g '*.go')
go test ./...
go vet ./...
go build ./...
```

Keep changes narrow, preserve the existing text and JSON behavior unless the
change explicitly evolves that contract, and never commit cluster configs,
credentials, private keys, or real infrastructure details.

CI also runs `govulncheck` and scans the complete Git history with `gitleaks`.
Normal changes should arrive through pull requests: every successful push to
`main` creates a patch release.

## Releases

A successful CI run for a push to `main` automatically creates the next patch
release. If no release tag exists, automation starts at `v0.1.0`; otherwise it
increments the latest patch version. Releases contain checksummed Linux and
macOS archives for amd64 and arm64.
