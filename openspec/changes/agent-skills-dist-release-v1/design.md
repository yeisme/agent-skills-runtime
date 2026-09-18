# Design

## Context

Scaena 的 `internal/adapters/agentskills/release.go` 是目前唯一的产品侧 dist
下载实现：checksums.txt → `<product>-install-manifest.json` → bundle/manifest/catalog
三资产下载，双重校验（checksums.txt 行 + manifest 内嵌 SHA-256），staging 解压
带 path-traversal/symlink/size 三重防护。Eikona 的 installer 是另一份更大的实现。
Sonora/Anatomia 接入时不应再复制第三、四份。

## Goals / Non-Goals

- Goals: 一份共享下载实现；strict bare semver；产品身份三重绑定
  (manifest.Product/Version/Tag 与请求一致)；默认仅信任公开 dist mirror，
  测试仅允许 loopback HTTP asset base。
- Non-Goals: latest 解析、构建侧打包、多 channel、缓存。

## Decisions

- **API 形态**: `ResolveDistRelease(ctx, DistOptions) (DistRelease, func(), error)`，
  返回 cleanup 闭包移除 staging 目录；失败路径内部自清理。
- **严格版本**: 拒绝 `v` 前缀与 `latest`，调用方必须先归一化（产品侧从
  buildinfo 取 bare 版本），错误码 `AGENT_SKILLS_INVALID_ARGUMENT`。
- **URL 白名单**: 仅当 asset base 为默认公网 mirror 时强制 manifest 内嵌 URL
  与拼出的 URL 一致；测试 base 跳过该检查但仍受 HTTPS/loopback 门槛。
- **错误模型**: 网络/解码失败统一包成 `AGENT_SKILLS_RELEASE_UNAVAILABLE`；
  校验类错误（digest/path/symlink）保留原细分 code，供产品 CLI 直接透传。
- **迁移路径**: Scaena 迁移到共享实现属 scaena 仓库的独立 change；Eikona
  的 installer 目录结构差异大，不强制迁移。

## Risks / Trade-offs

- 共享模块从"从不下载"变为"拥有唯一下载路径"——由本 change 显式记录并
  在 README/docs 标注；下载面比之前大，但换来四个产品共用一份审计过的代码。
- 新 error code 是 additive；消费者不需要立刻处理。

## Migration Plan

- 纯新增导出 surface；现有 consumer（eikona v0.8.1、scaena develop）不动。
- Sonora/Anatomia 的接入 change 以本模块的新 pseudo-version 为准。
