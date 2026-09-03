## Why

多个 Yeisme 产品会把重叠 Agent Skills 安装到相同用户 runtime。产品私有 lock
无法表达共同所有权，也无法在 uninstall、rollback 与用户 drift 时安全协作。

## What Changes

- 提供公共纯 Go module、七个 v1 schema 和 typed error codes。
- 校验受限 bundle 内容、文件 digest、catalog 和 product release identity。
- 实现 user-global runtime discovery、preview plan、revision CAS、共享 claims、
  staging/journal、apply、doctor、rollback 与 uninstall。
- 提供 deterministic offline list/search/describe/suggest primitives。
- 提供显式 legacy adoption API，但不内置任何产品 legacy 格式。

## Capabilities

### New Capabilities

- `agent-skills-runtime`: 共享 Agent Skills 文件所有权、事务和离线 catalog 公共库。

## Impact

- 首个消费者为 Eikona 和 Scaena。
- v0.1.0 发布前 API 标记 pre-release；v0.1.0 起 schema、路径、error code 和导出
  symbol 按 evolutionary policy 稳定。
- 无数据库、daemon、network client、CLI 或 Skill semantic router。

