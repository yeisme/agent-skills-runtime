# agent-skills-runtime Specification

## Purpose
TBD - created by archiving change agent-skills-runtime-v1. Update Purpose after archive.
## Requirements
### Requirement: The runtime SHALL validate bounded Skill bundles

Bundle entries SHALL be regular files below one named Skill directory and SHALL be limited
to `SKILL.md`, `agents/openai.yaml`, `references/**`, and `assets/**`. Declared SHA-256 and
computed inventory SHALL match before planning any mutation.

#### Scenario: A bundle contains an executable script

- **WHEN** a bundle contains `scripts/install.sh`
- **THEN** validation SHALL fail with `AGENT_SKILLS_BUNDLE_PATH_FORBIDDEN`
- **AND** no user state SHALL be created

### Requirement: The registry SHALL coordinate product claims by digest

Registry entries SHALL identify runtime, Skill name, digest, managed files, and product
claims. Same-digest claims SHALL coexist; a different digest under the same runtime/name
SHALL fail closed.

#### Scenario: The final product claim is removed

- **WHEN** uninstall releases the last claim and current bytes match the managed digest
- **THEN** runtime files SHALL be removed and registry revision incremented
- **AND** the committed transaction SHALL preserve recovery evidence

### Requirement: Mutations SHALL be plan-based and crash-recoverable

Apply SHALL require confirmation, acquire the user-global lock, validate plan base revision,
write staging and a transaction journal, then update runtime, registry and product receipt.

#### Scenario: Registry changed after planning

- **WHEN** apply observes a different registry revision than the reviewed plan
- **THEN** it SHALL return `AGENT_SKILLS_PLAN_STALE`
- **AND** SHALL NOT apply the stale operation set

### Requirement: Drift SHALL block implicit replacement and deletion

Any managed file whose current digest differs from registry inventory SHALL be reported as
drift. Apply, rollback and uninstall SHALL preserve it unless explicit reviewed replacement
is enabled for the exact conflict.

#### Scenario: Last-claim uninstall sees drift

- **WHEN** a managed Skill directory was edited after install
- **THEN** uninstall SHALL release no destructive operation against that directory
- **AND** SHALL report partial status with `AGENT_SKILLS_USER_DRIFT`

### Requirement: Offline suggestions SHALL be deterministic and non-executing

Catalog search and suggestion SHALL use only local bundle/catalog/receipt data. Suggestion
SHALL return at most three compatible entries with Skill ref, maturity, reason and default
prompt, and SHALL never execute a Skill or network request.

#### Scenario: Two entries have equal score

- **WHEN** two compatible catalog entries receive the same match score
- **THEN** results SHALL be ordered by Skill name ascending
- **AND** repeated calls with the same inputs SHALL be byte-equivalent after JSON encoding

