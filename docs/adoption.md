# 产品接入指南与统一分发方案

本文是 Yeisme 产品接入「按当前 CLI 版本安装 Agent Skills」的统一合同。状态快照与
迁移待办见文末；规范源头是 `openspec/specs/agent-skills-runtime/spec.md` 与各产品
自己的 OpenSpec change。

## 统一合同（所有产品一致）

1. **同版本不变量**：Skills 必须与运行中的 released CLI 精确同版本。产品侧从
   `buildinfo.Version` 取 bare `X.Y.Z`；非 released 构建（`dev` 等）fail-closed，
   不允许猜测版本。
2. **一份下载实现**：dist release 解析（checksums.txt → install manifest →
   bundle/manifest/catalog，双重 SHA-256，staging 防护）统一走本模块的
   `ResolveDistRelease`。产品不得再各自复制下载校验代码。
3. **发现与安装分离**：`skills list/search/describe/suggest` 离线、不执行；
   `install/plan/apply/doctor/rollback/uninstall` 走共享 `Manager`
   （registry claim、receipt、snapshot、rollback）。
4. **共享 claim 注册表**：相同 Skill bytes 跨产品共享 `~/.yeisme/agent-skills/`
   的 store 与 registry；产品各自 receipt 记在
   `~/.<product>/agent-skills.lock.json`（兼容 `install.lock.json`）。
5. **构建侧**：产品 release workflow 从受保护的公开 skills 源仓
   （`yeisme/yeisme-agent-my-skills`）检出，构建
   `<product>-skills_<ver>.tar.gz`、`<ver>.bundle.json`、`<ver>.catalog.json`
   与 `<product>-install-manifest.json` 并上传公开 dist
   （`yeisme/yeisme-dist`）。参考实现：scaena `scripts/scaena-skills-bundle.sh`
   与 `.github/workflows/release.yml`。
6. **测试**：产品测试用本地 bundle（`--bundle-root/--bundle-manifest/
   --skills-catalog`）与 httptest dist fixture，不触网、不调用真实 release。

## 产品状态（2026-09-18）

| 产品 | 发现 | 同版本安装 | dist 资产 | 接入模块 |
| --- | --- | --- | --- | --- |
| eikona | ✅ | ✅（setup 强制；裸 `skills install` 目前解析 latest，见待办） | ✅ v0.8.1 | 自有 installer |
| scaena | ✅ | ✅ | ⏳ 下个 release 首次携带 | 自有 adapter（待迁移共享 dist） |
| sonora | ✅ | 🚧 sonora-agent-skills-install-v1 | ⏳ 随 change 增补 | 共享 dist（本模块） |
| anatomia | 🚧 | 🚧 anatomia-agent-skills-install-v1 | ⏳ 随 change 增补 | 共享 dist（本模块） |

不在此合同范围的产品（pinax、auctra、inferrum、radar、mediahub、quaestor、
credentialctl、ordo、digital-human、gateway、gitea-mcp）走 `.skills/yeisme`
仓库或 template-registry 渠道，dist 只发二进制。

## 迁移待办（各产品仓库独立 change）

1. **eikona**：裸 `eikona skills install`（无 `--version`）默认解析 dist latest，
   与同版本不变量冲突；需要 change 把默认值收敛为运行版本（保留 `--version`
   显式覆盖），并给一个兼容窗口。
2. **scaena**：`internal/adapters/agentskills/release.go` 迁移到
   `ResolveDistRelease`，删除本地复制；同时下个 release 发布后实测
   `scaena skills install --yes`。
3. **anatomia**：2026-09-06 归档的 anatomia-agent-skills-distribution-v1 声称
   的实现从未进入 git（tasks 引用的测试在任何分支都不存在）；新 change 必须
   先补齐发现层再落安装层，并把这作为归档治理记录。

## 相关资料

- [contracts](./contracts.md)、[release](./release.md)
- `agent-skills-dist-release-v1`（本模块 dist API 的 OpenSpec change）
- 各产品 change：`cli/sonora/openspec/changes/sonora-agent-skills-install-v1/`、
  `agent/anatomia/openspec/changes/anatomia-agent-skills-install-v1/`
