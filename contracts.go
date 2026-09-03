package skillsruntime

import "time"

const (
	BundleSchema          = "yeisme.agent_skills.bundle.v1"
	InstallManifestSchema = "yeisme.product_install_manifest.v1"
	RegistrySchema        = "yeisme.agent_skills.registry.v1"
	InstallPlanSchema     = "yeisme.agent_skills.install_plan.v1"
	CatalogSchema         = "yeisme.agent_skills.catalog.v1"
	SuggestionSchema      = "yeisme.agent_skills.suggestion.v1"
	ProductReceiptSchema  = "yeisme.agent_skills.product_receipt.v1"
	TransactionSchema     = "yeisme.agent_skills.transaction.v1"
)

type RuntimeID string

const (
	RuntimeAgents RuntimeID = "agents"
	RuntimeCodex  RuntimeID = "codex"
	RuntimeClaude RuntimeID = "claude"
)

type SkillRole string

const (
	SkillRoleEntry      SkillRole = "entry"
	SkillRoleDependency SkillRole = "dependency"
)

type Maturity string

const (
	MaturityStable       Maturity = "stable"
	MaturityBeta         Maturity = "beta"
	MaturityExperimental Maturity = "experimental"
)

type ReferenceAvailability string

const (
	ReferenceIncluded        ReferenceAvailability = "included"
	ReferenceExternalProduct ReferenceAvailability = "external_product"
)

type SourceRef struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Path       string `json:"path,omitempty"`
}

type SkillReference struct {
	Name         string                `json:"name"`
	Availability ReferenceAvailability `json:"availability"`
	Product      string                `json:"product,omitempty"`
}

type FileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type BundleSkill struct {
	Name       string           `json:"name"`
	Role       SkillRole        `json:"role"`
	Maturity   Maturity         `json:"maturity"`
	Digest     string           `json:"digest"`
	Source     SourceRef        `json:"source"`
	Files      []FileDigest     `json:"files"`
	References []SkillReference `json:"references,omitempty"`
}

type BundleManifest struct {
	SchemaVersion  string        `json:"schema_version"`
	Product        string        `json:"product"`
	ProductVersion string        `json:"product_version"`
	BundleVersion  string        `json:"bundle_version"`
	Source         SourceRef     `json:"source"`
	RuntimeTargets []RuntimeID   `json:"runtime_targets"`
	Skills         []BundleSkill `json:"skills"`
}

type ReleaseAsset struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type SkillsRelease struct {
	BundleVersion  string       `json:"bundle_version"`
	Source         SourceRef    `json:"source"`
	RuntimeTargets []RuntimeID  `json:"runtime_targets"`
	Bundle         ReleaseAsset `json:"bundle"`
	Manifest       ReleaseAsset `json:"manifest"`
	Catalog        ReleaseAsset `json:"catalog"`
}

type ProductInstallManifest struct {
	SchemaVersion  string                  `json:"schema_version"`
	Product        string                  `json:"product"`
	ProductVersion string                  `json:"product_version"`
	Tag            string                  `json:"tag"`
	Commit         string                  `json:"commit"`
	Assets         map[string]ReleaseAsset `json:"assets,omitempty"`
	Skills         *SkillsRelease          `json:"skills,omitempty"`
}

type CatalogEntry struct {
	Name          string           `json:"name"`
	DisplayName   string           `json:"display_name"`
	Description   string           `json:"description"`
	Maturity      Maturity         `json:"maturity"`
	Role          SkillRole        `json:"role"`
	Digest        string           `json:"digest"`
	Keywords      []string         `json:"keywords,omitempty"`
	DefaultPrompt string           `json:"default_prompt,omitempty"`
	References    []SkillReference `json:"references,omitempty"`
}

type Catalog struct {
	SchemaVersion  string         `json:"schema_version"`
	Product        string         `json:"product"`
	ProductVersion string         `json:"product_version"`
	BundleVersion  string         `json:"bundle_version"`
	Entries        []CatalogEntry `json:"entries"`
}

type ProductIdentity struct {
	Product        string `json:"product"`
	ProductVersion string `json:"product_version"`
	BundleVersion  string `json:"bundle_version"`
	BundleDigest   string `json:"bundle_digest"`
}

type RuntimeTarget struct {
	Runtime   RuntimeID `json:"runtime"`
	SkillsDir string    `json:"skills_dir"`
}

type Claim struct {
	Product        string    `json:"product"`
	ProductVersion string    `json:"product_version"`
	BundleVersion  string    `json:"bundle_version"`
	BundleDigest   string    `json:"bundle_digest"`
	TransactionID  string    `json:"transaction_id"`
	ClaimedAt      time.Time `json:"claimed_at"`
}

type RegistryEntry struct {
	Runtime   RuntimeID    `json:"runtime"`
	SkillsDir string       `json:"skills_dir"`
	Skill     string       `json:"skill"`
	Digest    string       `json:"digest"`
	Files     []FileDigest `json:"files"`
	Claims    []Claim      `json:"claims"`
}

type Registry struct {
	SchemaVersion string          `json:"schema_version"`
	Revision      uint64          `json:"revision"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Entries       []RegistryEntry `json:"entries"`
}

type SnapshotEntry struct {
	Runtime   RuntimeID    `json:"runtime"`
	SkillsDir string       `json:"skills_dir"`
	Skill     string       `json:"skill"`
	Digest    string       `json:"digest"`
	Files     []FileDigest `json:"files"`
}

type ProductSnapshot struct {
	Identity ProductIdentity `json:"identity"`
	Entries  []SnapshotEntry `json:"entries"`
}

type ProductReceipt struct {
	SchemaVersion    string           `json:"schema_version"`
	Product          string           `json:"product"`
	RegistryRevision uint64           `json:"registry_revision"`
	Current          ProductSnapshot  `json:"current"`
	Previous         *ProductSnapshot `json:"previous,omitempty"`
	TransactionID    string           `json:"transaction_id"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

type PlanOperation string

const (
	OperationInstall   PlanOperation = "install"
	OperationUninstall PlanOperation = "uninstall"
	OperationRollback  PlanOperation = "rollback"
	OperationAdopt     PlanOperation = "adopt_legacy"
)

type PlanActionKind string

const (
	ActionInstall       PlanActionKind = "install"
	ActionReplace       PlanActionKind = "replace"
	ActionShare         PlanActionKind = "share"
	ActionClaim         PlanActionKind = "claim"
	ActionRelease       PlanActionKind = "release"
	ActionRemove        PlanActionKind = "remove"
	ActionPreserveDrift PlanActionKind = "preserve_drift"
)

type PlanAction struct {
	Kind      PlanActionKind `json:"kind"`
	Runtime   RuntimeID      `json:"runtime"`
	SkillsDir string         `json:"skills_dir"`
	Skill     string         `json:"skill"`
	Digest    string         `json:"digest,omitempty"`
	Reason    string         `json:"reason,omitempty"`
}

type Conflict struct {
	Code      string    `json:"code"`
	Blocking  bool      `json:"blocking"`
	Runtime   RuntimeID `json:"runtime"`
	SkillsDir string    `json:"skills_dir"`
	Skill     string    `json:"skill"`
	Path      string    `json:"path"`
	Message   string    `json:"message"`
}

type InstallPlan struct {
	SchemaVersion        string          `json:"schema_version"`
	PlanID               string          `json:"plan_id"`
	Operation            PlanOperation   `json:"operation"`
	Product              string          `json:"product"`
	BaseRegistryRevision uint64          `json:"base_registry_revision"`
	Desired              ProductSnapshot `json:"desired"`
	SourceRoot           string          `json:"source_root,omitempty"`
	SourceKind           string          `json:"source_kind"`
	Actions              []PlanAction    `json:"actions"`
	Conflicts            []Conflict      `json:"conflicts,omitempty"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	CreatedAt            time.Time       `json:"created_at"`
}

type ApplyOptions struct {
	Confirm          bool
	ReplaceConflicts bool
}

type ApplyResult struct {
	SchemaVersion    string        `json:"schema_version"`
	Status           string        `json:"status"`
	Operation        PlanOperation `json:"operation"`
	Product          string        `json:"product"`
	RegistryRevision uint64        `json:"registry_revision"`
	TransactionID    string        `json:"transaction_id"`
	Installed        []string      `json:"installed,omitempty"`
	Shared           []string      `json:"shared,omitempty"`
	Removed          []string      `json:"removed,omitempty"`
	Preserved        []string      `json:"preserved,omitempty"`
	ReceiptPath      string        `json:"receipt_path"`
	Evidence         []string      `json:"evidence,omitempty"`
}

type DoctorIssue struct {
	Code    string    `json:"code"`
	Runtime RuntimeID `json:"runtime,omitempty"`
	Skill   string    `json:"skill,omitempty"`
	Path    string    `json:"path,omitempty"`
	Message string    `json:"message"`
}

type DoctorReport struct {
	Status           string        `json:"status"`
	Product          string        `json:"product"`
	RegistryRevision uint64        `json:"registry_revision"`
	ReceiptPath      string        `json:"receipt_path"`
	Issues           []DoctorIssue `json:"issues,omitempty"`
}

type Suggestion struct {
	SchemaVersion string   `json:"schema_version"`
	SkillRef      string   `json:"skill_ref"`
	Name          string   `json:"name"`
	Maturity      Maturity `json:"maturity"`
	Reason        string   `json:"reason"`
	DefaultPrompt string   `json:"default_prompt,omitempty"`
	Score         int      `json:"score"`
}
