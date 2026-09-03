package skillsruntime

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Bundle is a validated local release bundle. The shared runtime never downloads it.
type Bundle struct {
	Root     string
	Manifest BundleManifest
	Catalog  Catalog
	Digest   string
}

// LoadBundle reads product-authored manifest and catalog JSON files, then validates all bytes.
func LoadBundle(root, manifestPath, catalogPath string) (Bundle, error) {
	var manifest BundleManifest
	if err := readJSONFile(manifestPath, &manifest); err != nil {
		return Bundle{}, newError(CodeBundleInvalid, "read bundle manifest", manifestPath, err)
	}
	var catalog Catalog
	if err := readJSONFile(catalogPath, &catalog); err != nil {
		return Bundle{}, newError(CodeBundleInvalid, "read bundle catalog", catalogPath, err)
	}
	return NewBundle(root, manifest, catalog)
}

// NewBundle validates an already decoded product bundle.
func NewBundle(root string, manifest BundleManifest, catalog Catalog) (Bundle, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Bundle{}, newError(CodeInvalidArgument, "resolve bundle root", root, err)
	}
	bundle := Bundle{Root: filepath.Clean(absRoot), Manifest: manifest, Catalog: catalog}
	if err := bundle.Validate(); err != nil {
		return Bundle{}, err
	}
	bundle.Manifest, bundle.Catalog = canonicalizeBundle(bundle.Manifest, bundle.Catalog)
	digest, err := bundleIdentityDigest(bundle.Manifest, bundle.Catalog)
	if err != nil {
		return Bundle{}, newError(CodeBundleInvalid, "compute bundle identity", root, err)
	}
	bundle.Digest = digest
	return bundle, nil
}

func (b Bundle) Validate() error {
	if b.Manifest.SchemaVersion != BundleSchema {
		return newError(CodeSchemaUnsupported, "unsupported bundle schema", b.Manifest.SchemaVersion, nil)
	}
	if b.Catalog.SchemaVersion != CatalogSchema {
		return newError(CodeSchemaUnsupported, "unsupported catalog schema", b.Catalog.SchemaVersion, nil)
	}
	if b.Manifest.Product == "" || b.Manifest.ProductVersion == "" || b.Manifest.BundleVersion == "" {
		return newError(CodeBundleInvalid, "product and version fields are required", b.Root, nil)
	}
	if b.Catalog.Product != b.Manifest.Product || b.Catalog.ProductVersion != b.Manifest.ProductVersion || b.Catalog.BundleVersion != b.Manifest.BundleVersion {
		return newError(CodeBundleInvalid, "catalog identity does not match bundle manifest", b.Root, nil)
	}
	if len(b.Manifest.Skills) == 0 {
		return newError(CodeBundleInvalid, "bundle contains no skills", b.Root, nil)
	}
	manifestSkills := make(map[string]BundleSkill, len(b.Manifest.Skills))
	for _, skill := range b.Manifest.Skills {
		if err := validateBundleSkill(b.Root, skill); err != nil {
			return err
		}
		if _, exists := manifestSkills[skill.Name]; exists {
			return newError(CodeBundleInvalid, "duplicate skill in bundle", skill.Name, nil)
		}
		manifestSkills[skill.Name] = skill
	}
	if strings.TrimSpace(b.Manifest.Source.Repository) == "" || !commitPattern.MatchString(strings.ToLower(b.Manifest.Source.Commit)) {
		return newError(CodeBundleInvalid, "bundle source must include a repository and exact 40-character commit", b.Manifest.Product, nil)
	}
	for _, skill := range b.Manifest.Skills {
		for _, ref := range skill.References {
			_, included := manifestSkills[ref.Name]
			if ref.Availability == ReferenceIncluded && !included {
				return newError(CodeBundleInvalid, "included Skill reference is missing from the bundle", ref.Name, nil)
			}
		}
	}
	if err := validateBundleTree(b.Root, manifestSkills); err != nil {
		return err
	}
	return validateCatalog(b.Catalog, manifestSkills)
}

func validateBundleSkill(root string, skill BundleSkill) error {
	if !skillNamePattern.MatchString(skill.Name) {
		return newError(CodeBundleInvalid, "invalid skill name", skill.Name, nil)
	}
	if skill.Role != SkillRoleEntry && skill.Role != SkillRoleDependency {
		return newError(CodeBundleInvalid, "invalid skill role", skill.Name, nil)
	}
	if !validMaturity(skill.Maturity) {
		return newError(CodeBundleInvalid, "invalid skill maturity", skill.Name, nil)
	}
	if len(skill.Files) == 0 {
		return newError(CodeBundleInvalid, "skill file inventory is empty", skill.Name, nil)
	}
	seen := map[string]struct{}{}
	if strings.TrimSpace(skill.Source.Repository) == "" || !commitPattern.MatchString(strings.ToLower(skill.Source.Commit)) || strings.TrimSpace(skill.Source.Path) == "" {
		return newError(CodeBundleInvalid, "Skill source must include repository, exact commit, and path", skill.Name, nil)
	}
	hasSkillMD := false
	actual := make([]FileDigest, 0, len(skill.Files))
	for _, declared := range skill.Files {
		rel, err := validateBundleRelativePath(declared.Path)
		if err != nil {
			return newError(CodeBundlePathForbidden, err.Error(), declared.Path, err)
		}
		if _, exists := seen[rel]; exists {
			return newError(CodeBundleInvalid, "duplicate file in skill inventory", rel, nil)
		}
		seen[rel] = struct{}{}
		hasSkillMD = hasSkillMD || rel == "SKILL.md"
		full := filepath.Join(root, "skills", skill.Name, filepath.FromSlash(rel))
		info, err := os.Lstat(full)
		if err != nil {
			return newError(CodeBundleInvalid, "declared skill file is missing", full, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return newError(CodeBundleSymlink, "symlink is not allowed in a skill bundle", full, nil)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 != 0 {
			return newError(CodeBundlePathForbidden, "only non-executable regular files are allowed", full, nil)
		}
		digest, size, err := digestFile(full)
		if err != nil {
			return newError(CodeIO, "hash skill file", full, err)
		}
		expected, err := normalizeDigest(declared.SHA256)
		if err != nil || digest != expected {
			return newError(CodeDigestMismatch, "skill file digest does not match manifest", full, err)
		}
		if declared.Size > 0 && declared.Size != size {
			return newError(CodeDigestMismatch, "skill file size does not match manifest", full, nil)
		}
		actual = append(actual, FileDigest{Path: rel, SHA256: digest, Size: size})
	}
	if !hasSkillMD {
		return newError(CodeBundleInvalid, "SKILL.md is required", skill.Name, nil)
	}
	digest, err := SkillDigest(actual)
	if err != nil {
		return newError(CodeBundleInvalid, "compute skill digest", skill.Name, err)
	}
	expected, err := normalizeDigest(skill.Digest)
	if err != nil || digest != expected {
		return newError(CodeDigestMismatch, "skill tree digest does not match manifest", skill.Name, err)
	}
	for _, ref := range skill.References {
		if !skillNamePattern.MatchString(ref.Name) {
			return newError(CodeBundleInvalid, "invalid skill reference", ref.Name, nil)
		}
		if ref.Availability != ReferenceIncluded && ref.Availability != ReferenceExternalProduct {
			return newError(CodeBundleInvalid, "skill reference availability must be included or external_product", ref.Name, nil)
		}
		if ref.Availability == ReferenceExternalProduct && strings.TrimSpace(ref.Product) == "" {
			return newError(CodeBundleInvalid, "external_product reference requires product", ref.Name, nil)
		}
	}
	return nil
}

func validateBundleTree(root string, skills map[string]BundleSkill) error {
	skillsRoot := filepath.Join(root, "skills")
	return filepath.WalkDir(skillsRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return newError(CodeBundleInvalid, "walk bundle", filePath, walkErr)
		}
		if filePath == skillsRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return newError(CodeBundleSymlink, "symlink is not allowed in a skill bundle", filePath, nil)
		}
		rel, err := filepath.Rel(skillsRoot, filePath)
		if err != nil {
			return newError(CodeBundleInvalid, "resolve bundle path", filePath, err)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		skill, exists := skills[parts[0]]
		if !exists {
			return newError(CodeBundleInvalid, "undeclared skill directory", filePath, nil)
		}
		if entry.IsDir() {
			return nil
		}
		if len(parts) < 2 {
			return newError(CodeBundlePathForbidden, "file must be below a skill directory", filePath, nil)
		}
		within := strings.Join(parts[1:], "/")
		declared := false
		for _, file := range skill.Files {
			if file.Path == within {
				declared = true
				break
			}
		}
		if !declared {
			return newError(CodeBundleInvalid, "undeclared file in skill directory", filePath, nil)
		}
		return nil
	})
}

func validateBundleRelativePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || path.IsAbs(value) {
		return "", fmt.Errorf("bundle path must be a non-empty slash-relative path")
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("bundle path traversal is not allowed")
	}
	if clean == "SKILL.md" || clean == "agents/openai.yaml" || strings.HasPrefix(clean, "references/") || strings.HasPrefix(clean, "assets/") {
		return clean, nil
	}
	return "", fmt.Errorf("bundle path is outside the allowed Skill payload")
}

func validateCatalog(catalog Catalog, skills map[string]BundleSkill) error {
	if len(catalog.Entries) != len(skills) {
		return newError(CodeBundleInvalid, "catalog must describe every bundled skill exactly once", catalog.Product, nil)
	}
	seen := map[string]struct{}{}
	for _, entry := range catalog.Entries {
		skill, exists := skills[entry.Name]
		if !exists {
			return newError(CodeBundleInvalid, "catalog contains an unknown skill", entry.Name, nil)
		}
		if _, duplicate := seen[entry.Name]; duplicate {
			return newError(CodeBundleInvalid, "catalog contains a duplicate skill", entry.Name, nil)
		}
		seen[entry.Name] = struct{}{}
		if entry.Role != skill.Role || entry.Maturity != skill.Maturity {
			return newError(CodeBundleInvalid, "catalog role or maturity differs from manifest", entry.Name, nil)
		}
		digest, err := normalizeDigest(entry.Digest)
		skillDigest, skillErr := normalizeDigest(skill.Digest)
		if err != nil || skillErr != nil || digest != skillDigest {
			return newError(CodeDigestMismatch, "catalog digest differs from manifest", entry.Name, err)
		}
		if strings.TrimSpace(entry.DisplayName) == "" || strings.TrimSpace(entry.Description) == "" {
			return newError(CodeBundleInvalid, "catalog display_name and description are required", entry.Name, nil)
		}
	}
	return nil
}

func canonicalizeBundle(manifest BundleManifest, catalog Catalog) (BundleManifest, Catalog) {
	manifest.Source.Commit = strings.ToLower(manifest.Source.Commit)
	for index := range manifest.Skills {
		manifest.Skills[index].Digest, _ = normalizeDigest(manifest.Skills[index].Digest)
		manifest.Skills[index].Files = normalizeFiles(manifest.Skills[index].Files)
		manifest.Skills[index].Source.Commit = strings.ToLower(manifest.Skills[index].Source.Commit)
	}
	for index := range catalog.Entries {
		catalog.Entries[index].Digest, _ = normalizeDigest(catalog.Entries[index].Digest)
	}
	return manifest, catalog
}

func validMaturity(value Maturity) bool {
	return value == MaturityStable || value == MaturityBeta || value == MaturityExperimental
}

func bundleIdentityDigest(manifest BundleManifest, catalog Catalog) (string, error) {
	payload := struct {
		Manifest BundleManifest `json:"manifest"`
		Catalog  Catalog        `json:"catalog"`
	}{Manifest: manifest, Catalog: catalog}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest, _, err := digestReader(strings.NewReader(string(encoded)))
	return digest, err
}

func sortedBundleSkills(skills []BundleSkill) []BundleSkill {
	result := append([]BundleSkill(nil), skills...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
