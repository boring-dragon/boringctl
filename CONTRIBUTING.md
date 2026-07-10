# Contributing

Contributions are welcome when they keep `boringctl` focused on Proxmox and its
closely related operational workflows.

Before opening a pull request:

```bash
gofmt -w $(rg --files -g '*.go')
go test ./...
go vet ./...
go build ./...
```

Keep changes narrow, preserve the existing text and JSON behavior unless the
change explicitly evolves that contract, and never commit cluster configs,
credentials, private keys, or real infrastructure details.

## Releases

A successful CI run for a push to `main` automatically creates the next patch
release. The first release is `v0.1.0`; subsequent releases increment the patch
version and publish checksummed Linux and macOS archives for amd64 and arm64.
