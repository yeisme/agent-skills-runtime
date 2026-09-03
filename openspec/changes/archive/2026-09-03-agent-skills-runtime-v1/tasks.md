## 1. 公共合同与 bundle validation

- [x] 1.1 实现 schema structs、runtime ids、state paths、typed errors 与 JSON validators。
  - Scope: root Go package; no product policy or networking.
  - Acceptance: `go test ./... -run 'TestSchema|TestBundle'` passes; traversal and symlink fixtures fail closed.
- [x] 1.2 实现 content digest、allowlisted tree inventory 与 catalog loading。
  - Depends on: 1.1.
  - Acceptance: deterministic digest tests pass on Linux and `CGO_ENABLED=0 go test ./...`.

## 2. Plan、claims 与 transaction

- [x] 2.1 实现 runtime auto detection、registry revision CAS 和 unmanaged/digest conflict plan。
  - Depends on: 1.1, 1.2.
  - Acceptance: `go test ./... -run 'TestPlan|TestDetectRuntimes'` passes.
- [x] 2.2 实现 lock、staging、journal、store、runtime replacement、registry 和 receipt atomic writes。
  - Depends on: 2.1.
  - Acceptance: apply happy path, stale plan, concurrent lock and injected failure recovery tests pass.
- [x] 2.3 实现 doctor、uninstall、rollback 与 explicit legacy adoption。
  - Depends on: 2.2.
  - Acceptance: multi-product same digest, last-claim delete, drift preserve and rollback tests pass.

## 3. Offline catalog

- [x] 3.1 实现 list/search/describe/suggest deterministic projections。
  - Depends on: 1.2.
  - Acceptance: ranking, `--all` equivalent filtering, incompatibility exclusion and max-three tests pass.

## 4. 文档与发布护栏

- [x] 4.1 完成 architecture/contracts/release 文档、CI、golangci-lint 与 GoReleaser config。
- [x] 4.2 运行 `go test ./...`、`go test -race ./...`、`go vet ./...`、`CGO_ENABLED=0 go build ./...`、`golangci-lint run`、`goreleaser check`。
- [x] 4.3 运行 `openspec validate agent-skills-runtime-v1 --strict --no-interactive` 并记录结果。

## Verification record

- 2026-09-03: `go test ./...` passed.
- 2026-09-03: `go test -race ./...` passed.
- 2026-09-03: `go vet ./...` passed.
- 2026-09-03: `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` passed.
- 2026-09-03: `golangci-lint run` reported `0 issues`.
- 2026-09-03: `goreleaser check` passed.
- 2026-09-03: strict OpenSpec validation passed.
- 2026-09-03 review follow-up: serialized plans now normalize omitted empty
  action/conflict arrays before stale-plan comparison;
  `TestSerializedPlanWithoutConflictsCanBeApplied` passed.
- 2026-09-03 review follow-up: the complete local gate set above passed again
  after the serialization fix.

任务串行，因为 schema、registry mutation 和 exported API 是共享写入边界；消费者可在
公共 module tag 后独立并行。
