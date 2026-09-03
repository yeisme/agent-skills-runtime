# 发布与兼容

本仓库是公开 Go module。正常 build/test 必须保持纯 Go 与 `CGO_ENABLED=0`。

## Local gates

```bash
go mod tidy -diff
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
golangci-lint run
goreleaser check
goreleaser release --snapshot --clean --skip=publish
openspec validate --all --strict
```

## Version policy

- 首个 public consumer 可 pin 精确 commit/pseudo-version。
- `v0.1.0` tag 是 schema、error code、state path 和 exported API 的稳定起点。
- 添加 optional field 或 exported helper 为 additive minor change。
- 删除、重命名、narrowing 或语义复用必须先建立 OpenSpec migration，保持旧 surface
  至少一个 release，并提供 Eikona/Scaena consumer rollback。
- 已推送 tag 不复用。发布失败后修复并使用下一个 patch/pre-release tag。

## Release workflow

Pull request 与 `main` CI 执行 tidy diff、unit/race、pure-Go build、golangci-lint 和
GoReleaser config check。`workflow_dispatch` 只能验证 snapshot；只有匹配 semver 的
tag job 可在 protected `release` environment 使用 `contents: write` 创建 GitHub Release。

产品升级共享 module 后必须运行各自的 installer/CLI contract tests。Eikona legacy
migration deprecation window 未结束前，不得发布移除 legacy adapter 的 module/consumer
组合。

