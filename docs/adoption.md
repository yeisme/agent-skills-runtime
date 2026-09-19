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
| scaena | ✅ | ✅ | ⏳ 下个 release 首次携带（`vars` 已钉 e322ce5a） | 共享 dist（2026-09-19 迁移，e26b7c5b） |
| sonora | ✅ | ✅ sonora-agent-skills-install-v1（2026-09-18, 8a650f3；源路径修正 audio-workflow/） | ⏳ 下个 release 首次携带（`vars` 已钉 e322ce5a） | 共享 dist（本模块） |
| anatomia | ✅ | ✅ anatomia-agent-skills-install-v1（2026-09-18, ba212e4；兼修复 09-06 虚假归档） | ⏳ 下个 release 首次携带（`vars` 已钉 e322ce5a） | 共享 dist（本模块） |

不在此合同范围的产品（pinax、auctra、inferrum、radar、mediahub、quaestor、
credentialctl、ordo、digital-human、gateway、gitea-mcp）走 `.skills/yeisme`
仓库或 template-registry 渠道，dist 只发二进制。

## 迁移待办

1. ~~eikona 裸 install 默认 latest~~ — 已收敛：`eikona-skills-install-version-default-v1`
   （2026-09-19，0215a629）：裸 install/plan pin 运行 released 版本，dev 构建
   fail-closed（`SETUP_RELEASE_REQUIRED`），`--version latest` 显式保留旧行为。
2. ~~scaena 迁移共享 dist 解析~~ — 已完成：`scaena-agent-skills-shared-dist-v1`
   （2026-09-19，e26b7c5b）：release.go 278→75 行，下载/校验/解压全走
   `ResolveDistRelease`；builder 首次对真实源 17/17 全绿。
3. ~~SKILLS_SOURCE_REF 变量~~ — 已设置（2026-09-19）：sonora/scaena/anatomia 三仓
   `vars.<PRODUCT>_SKILLS_SOURCE_REF` 均钉在 my-skills `e322ce5a`；该 commit 补发
   了 anatomia-video-transform-operator、creative-grilling、manga-drama-grill-me，
   并把 scaena gitlink 升到 scaena-skills `4f7a4a4`（scaena/ai-drama 等 skill
   模块是 my-skills 的 submodule，远程 API 探不到，须 recursive 检出后核验）。
4. **发版实测（owner 门）**：三产品下个 release 后各跑一次
   `<product> skills install --yes`；skills 资产首次随 release 上传。
5. **anatomia 后续归档必须附实现 commit 证据**（2026-09-06 虚假归档治理）。

## 相关资料

- [contracts](./contracts.md)、[release](./release.md)
- `agent-skills-dist-release-v1`（本模块 dist API 的 OpenSpec change）
- 各产品 change：`cli/sonora/openspec/changes/sonora-agent-skills-install-v1/`、
  `agent/anatomia/openspec/changes/anatomia-agent-skills-install-v1/`
