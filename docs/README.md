# 文档索引

- [架构与状态模型](architecture.md)：模块边界、共享 registry、claim、事务与回滚。
- [公共合同](contracts.md)：bundle、catalog、plan、registry、receipt schema 与错误码。
- [发布](release.md)：公开 Go module 的 CI、tag、兼容与消费者迁移要求。
- [产品接入与统一分发方案](adoption.md)：跨产品同版本安装合同、状态快照与迁移待办。
- [OpenSpec change](../openspec/changes/archive/2026-09-03-agent-skills-runtime-v1/)：首版实现与验证记录。

## Agent Skills

本项目会话的 active skills 由仓库根 `.skills/profiles/targets/shared/agent-skills-runtime.txt` 分配，用 `scripts/skills.sh sync-target shared/agent-skills-runtime` 生成 `.agents/skills/` 与 `.claude/skills/`。不要手改运行副本。

完整对照、缺口与下一波优化见 [子项目 Skill Profile](../../../docs/skills/subproject-skill-profiles.md)。

