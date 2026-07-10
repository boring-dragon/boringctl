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
