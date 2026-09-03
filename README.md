# agent-skills-runtime

`agent-skills-runtime` is the shared pure-Go library used by Yeisme products to
install release-bound Agent Skills into detected Codex, Claude, and generic
Agent runtime homes.

It provides content-addressed bundle validation, preview/apply plans, a shared
user-level claim registry, drift-safe doctor/rollback/uninstall operations, and
deterministic offline catalog search and suggestions. Product CLIs keep control
of release discovery, bundle curation, command UX, and domain semantics.

The module deliberately does not provide an online marketplace, Skill
authoring, provider calls, or arbitrary repository installation.

## Development

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
goreleaser check
openspec validate --all --strict
```

See [docs/README.md](docs/README.md) for architecture and contract documents.
