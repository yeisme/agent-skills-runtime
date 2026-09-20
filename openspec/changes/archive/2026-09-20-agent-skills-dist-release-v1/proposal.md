# agent-skills-dist-release-v1

## Why

Sonora and Anatomia need the same exact-version Agent Skills install lifecycle that
Eikona and Scaena already ship, but each product currently has to port Scaena's
release download, checksum, and staging code (~280 lines of security-sensitive
logic). Diverging copies of that code across products is the main obstacle to a
unified cross-product skills distribution contract.

## What Changes

- Add an additive `ResolveDistRelease(ctx, DistOptions)` API that downloads the
  exact `<product>/v<version>` release assets from the public distribution
  mirror (`https://github.com/yeisme/yeisme-dist/releases/download`), verifies
  every byte against `checksums.txt` and the product install manifest, extracts
  the bundle into a private staging directory with path-symlink-and-size guards,
  and returns the validated `Bundle` plus a cleanup function.
- Export `HTTPDoer`, `DefaultDistAssetBase`, bounded size limits, and the new
  error codes `AGENT_SKILLS_RELEASE_UNAVAILABLE` and
  `AGENT_SKILLS_SOURCE_UNTRUSTED`.
- Update the bundle doc comment: the runtime previously stated it never
  downloads bundles; `ResolveDistRelease` is now the single shared download
  path, restricted to HTTPS (loopback HTTP for tests).

## 不做什么

- 不改变现有 `LoadBundle`/`Manager`/registry/receipt 任何已有 surface。
- 不实现 latest-channel 解析或 catalog 发现；调用方必须传 bare `X.Y.Z`。
- 不做 bundle 构建侧（产品 release 脚本继续参考 scaena-skills-bundle.sh）。

## Impact

- 实现：`dist.go`、`dist_test.go`、`errors.go` 两个新 code、`bundle.go` 注释。
- 消费方：sonora-agent-skills-install-v1、anatomia-agent-skills-install-v1
  （Eikona/Scaena 可后续迁移，属各自仓库的 follow-up change）。
- 兼容：纯 additive，无 breaking；已发布 pseudo-version 不受影响。
