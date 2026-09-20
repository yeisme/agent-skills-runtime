# Tasks

- [x] 1.1 `dist.go`：`HTTPDoer`、`DefaultDistAssetBase`、size limits、`DistOptions`/`DistRelease`/`ResolveDistRelease`
- [x] 1.2 errors.go 新增 `AGENT_SKILLS_RELEASE_UNAVAILABLE`、`AGENT_SKILLS_SOURCE_UNTRUSTED`
- [x] 1.3 `bundle.go` "never downloads" 注释改为指向 `ResolveDistRelease`
- [x] 2.1 dist_test.go：正常解析+cleanup、篡改 digest、身份不匹配、非信 asset base、非法参数、traversal archive、URL 拼接
- [x] 2.2 `go test ./...`、`go test -race ./...`、`go vet ./...`、`CGO_ENABLED=0 go build ./...` 全绿
- [x] 2.3 `openspec validate agent-skills-dist-release-v1 --strict --no-interactive`
