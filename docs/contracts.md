# 公共合同

## Schema

| Schema | Owner | Purpose |
| --- | --- | --- |
| `yeisme.agent_skills.bundle.v1` | product release | exact Skill files, provenance, role and maturity |
| `yeisme.product_install_manifest.v1` | product release | exact CLI/tag and optional Skills assets |
| `yeisme.agent_skills.registry.v1` | shared runtime | runtime entries and product claims |
| `yeisme.agent_skills.install_plan.v1` | shared runtime | reviewed mutation set and base revision |
| `yeisme.agent_skills.catalog.v1` | product release | offline discovery metadata |
| `yeisme.agent_skills.suggestion.v1` | shared primitive/product projection | deterministic non-executing suggestion |
| `yeisme.agent_skills.product_receipt.v1` | shared runtime | current and previous product claim sets |

JSON reader 接受未知 optional fields，但拒绝未知 major schema。v0.1.0 发布后，删除、
重命名、重定义 required field、runtime id、error code、state path 或 public Go symbol 均
需要 migration、至少一个 release deprecation window 和 rollback。

## Bundle layout

```text
<bundle-root>/
└── skills/
    └── <skill-name>/
        ├── SKILL.md
        ├── agents/openai.yaml
        ├── references/
        └── assets/
```

Manifest 和 catalog 可作为 tarball 外的 release assets，也可由产品复制到解压目录；
`LoadBundle` 通过 caller 提供的路径读取二者。`InventorySkillDirectory` 供产品 bundle
generator 生成精确 file inventory 和 canonical Skill tree digest。

Skill tree digest 为排序后的 `path + NUL + normalized sha256 + NUL` 的 SHA-256。
Public JSON 使用 `sha256:<64 lowercase hex>`；store 目录使用不带前缀的 hex。

## Go entry points

```go
bundle, err := skillsruntime.LoadBundle(root, manifestPath, catalogPath)
targets, err := skillsruntime.ResolveRuntimes("auto", skillsruntime.DetectOptions{
    HomeDir: home,
    CodexHome: codexHome,
})
paths, err := skillsruntime.DefaultPaths(home, "scaena")
manager, err := skillsruntime.NewManager(skillsruntime.ManagerOptions{
    Paths: paths,
    AllowedRuntimeRoots: []string{codexHome},
})
plan, err := manager.PlanInstall(bundle, targets)
result, err := manager.Apply(ctx, plan, skillsruntime.ApplyOptions{Confirm: true})
```

Mutation APIs：

- `PlanInstall`
- `PlanUninstall`
- `PlanRollback`
- `PlanLegacyAdoption`
- `Apply`
- `Doctor`

Read-only/catalog APIs：

- `ReadRegistry`
- `ReadReceipt`
- `Catalog.List`
- `Catalog.Search`
- `Catalog.Describe`
- `Catalog.Suggest`

Structured release assets 应由产品 CLI/service 调用 `WriteBundleManifest`、`WriteCatalog`
与 `WriteInstallManifest` 生成，不由 Agent 手写 JSON。

## Stable error codes

| Code | Meaning |
| --- | --- |
| `AGENT_SKILLS_SCHEMA_UNSUPPORTED` | unknown schema major |
| `AGENT_SKILLS_BUNDLE_INVALID` | incomplete or inconsistent bundle/catalog |
| `AGENT_SKILLS_BUNDLE_PATH_FORBIDDEN` | path or executable payload outside allowlist |
| `AGENT_SKILLS_BUNDLE_SYMLINK_FORBIDDEN` | symlink in bundle |
| `AGENT_SKILLS_DIGEST_MISMATCH` | declared and actual bytes differ |
| `AGENT_SKILLS_REGISTRY_BUSY` | lock unavailable or ownership lost |
| `AGENT_SKILLS_PLAN_STALE` | registry or filesystem changed after preview |
| `AGENT_SKILLS_UNMANAGED_CONFLICT` | target exists without shared ownership |
| `AGENT_SKILLS_DIGEST_CONFLICT` | another product claims different bytes |
| `AGENT_SKILLS_USER_DRIFT` | managed bytes were modified or removed |
| `AGENT_SKILLS_NOT_INSTALLED` | product has no current receipt |
| `AGENT_SKILLS_VERSION_MISMATCH` | receipt does not match running release |
| `AGENT_SKILLS_TRANSACTION_INCOMPLETE` | crash recovery needs operator attention |

产品可定义更窄的错误，例如 Scaena 在调用共享 mutation 前检测到 Eikona legacy lock
时使用 `AGENT_SKILLS_LEGACY_OWNER_DETECTED`。

