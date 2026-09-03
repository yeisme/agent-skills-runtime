# Agent Skills Runtime instructions

## Scope and architecture

`agent-skills-runtime` is the public, pure-Go module
`github.com/yeisme/agent-skills-runtime`. It owns only the mechanical contracts
and filesystem operations needed by Yeisme products to install version-bound
Agent Skills, share identical managed files safely, maintain product claims,
and query an offline catalog.

Product bundle policy, release discovery, command wording, semantic routing,
and domain recommendations remain in the consuming product. This module is not
a marketplace, remote registry service, Agent scheduler, or Skill authoring
tool.

Keep the normal build compatible with `CGO_ENABLED=0`. Prefer the standard
library. The user-scoped registry is JSON written atomically under an explicit
lock; do not introduce relational persistence or a daemon.

## Stable contracts

- Public Go APIs, schema names, JSON fields, error codes, runtime identifiers,
  digest rules, and state paths are compatibility surfaces.
- Additive evolution is preferred. Removing, renaming, narrowing, or
  repurposing a released surface requires an OpenSpec migration,
  deprecation window, consumer list, and rollback.
- Managed files are content addressed. A runtime/skill path may have multiple
  product claims only when all claims use the same digest.
- Never adopt an unmanaged directory implicitly. Legacy adoption must be
  explicit, validated, and owned by the migrating product adapter.
- Preserve user edits. Drift is reported and blocks replacement or deletion
  unless the caller explicitly requests reviewed conflict replacement.

## Security and filesystem safety

- Accept only relative, slash-normalized bundle paths rooted below one Skill
  directory. Reject traversal, absolute paths, symlinks, devices, and bundle
  entries outside the allowlist.
- Bundle payloads may include only `SKILL.md`, `agents/openai.yaml`,
  `references/**`, and `assets/**`. Executable scripts are excluded.
- Do not persist credentials, raw prompts, hidden system prompts, provider
  payloads, private tool arguments, or chain-of-thought.
- All mutations require a lock, a validated plan revision, staging, an
  append-only transaction journal, and atomic replacement where the platform
  permits it.

## Validation

```bash
go mod tidy -diff
go test ./...
go vet ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
golangci-lint run
goreleaser check
openspec validate --all --strict
```

Integration, component, system, or e2e entry points must write redacted
evidence under `temp/integration-test-runs/<run-id>/`. Unit tests do not need
that wrapper.

Commit, tag, release, and later pushes remain root-maintainer actions and need
explicit authorization for the exact external operation.

## Skill routing

- Use `yeisme-coding-execution-driver` for implementation and verification.
- Use `backend-system-workflow` for locking, atomic writes, state transitions,
  and concurrency.
- Use `yeisme-evolutionary-change-policy` for schemas, state paths, error codes,
  and public Go APIs.
- Use `golang-github-release-guardrails` for CI, GoReleaser, tags, and releases.
- Use `project-integration-test-evidence` for non-unit verification evidence.

