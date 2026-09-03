package skillsruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// InventorySkillDirectory returns an allowlisted, content-addressed Skill inventory.
func InventorySkillDirectory(root string) ([]FileDigest, string, error) {
	files, err := inspectSkillDir(root)
	if err != nil {
		return nil, "", err
	}
	if len(files) == 0 {
		return nil, "", newError(CodeBundleInvalid, "Skill directory is empty", root, nil)
	}
	hasSkillMD := false
	for _, file := range files {
		if _, err := validateBundleRelativePath(file.Path); err != nil {
			return nil, "", newError(CodeBundlePathForbidden, err.Error(), filepath.Join(root, file.Path), err)
		}
		if file.Path == "SKILL.md" {
			hasSkillMD = true
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, "", newError(CodeIO, "inspect Skill file mode", filepath.Join(root, file.Path), err)
		}
		if info.Mode().Perm()&0o111 != 0 {
			return nil, "", newError(CodeBundlePathForbidden, "executable Skill payloads are not allowed", filepath.Join(root, file.Path), nil)
		}
	}
	if !hasSkillMD {
		return nil, "", newError(CodeBundleInvalid, "SKILL.md is required", root, nil)
	}
	digest, err := SkillDigest(files)
	if err != nil {
		return nil, "", newError(CodeBundleInvalid, "compute Skill directory digest", root, err)
	}
	return files, digest, nil
}

func ValidateInstallManifest(manifest ProductInstallManifest) error {
	if manifest.SchemaVersion != InstallManifestSchema {
		return newError(CodeSchemaUnsupported, "unsupported product install manifest schema", manifest.SchemaVersion, nil)
	}
	if !skillNamePattern.MatchString(manifest.Product) || manifest.ProductVersion == "" || manifest.Tag == "" || !commitPattern.MatchString(strings.ToLower(manifest.Commit)) {
		return newError(CodeBundleInvalid, "product install manifest identity is incomplete", manifest.Product, nil)
	}
	for name, asset := range manifest.Assets {
		if err := validateReleaseAsset(name, asset); err != nil {
			return err
		}
	}
	if manifest.Skills == nil {
		return nil
	}
	if manifest.Skills.BundleVersion == "" || manifest.Skills.Source.Repository == "" || !commitPattern.MatchString(strings.ToLower(manifest.Skills.Source.Commit)) {
		return newError(CodeBundleInvalid, "Skills release identity is incomplete", manifest.Product, nil)
	}
	for name, asset := range map[string]ReleaseAsset{
		"skills.bundle":   manifest.Skills.Bundle,
		"skills.manifest": manifest.Skills.Manifest,
		"skills.catalog":  manifest.Skills.Catalog,
	} {
		if err := validateReleaseAsset(name, asset); err != nil {
			return err
		}
	}
	return nil
}

func validateReleaseAsset(name string, asset ReleaseAsset) error {
	if strings.TrimSpace(asset.Name) == "" || strings.TrimSpace(asset.Kind) == "" || strings.TrimSpace(asset.URL) == "" {
		return newError(CodeBundleInvalid, "release asset requires name, kind, and URL", name, nil)
	}
	if _, err := normalizeDigest(asset.SHA256); err != nil {
		return newError(CodeDigestMismatch, "release asset has invalid SHA-256", asset.Name, err)
	}
	return nil
}

func ReadCatalog(path string) (Catalog, error) {
	var catalog Catalog
	if err := readJSONFile(path, &catalog); err != nil {
		return Catalog{}, newError(CodeIO, "read Agent Skills catalog", path, err)
	}
	if catalog.SchemaVersion != CatalogSchema {
		return Catalog{}, newError(CodeSchemaUnsupported, "unsupported catalog schema", path, nil)
	}
	return catalog, nil
}

func ReadInstallManifest(path string) (ProductInstallManifest, error) {
	var manifest ProductInstallManifest
	if err := readJSONFile(path, &manifest); err != nil {
		return ProductInstallManifest{}, newError(CodeIO, "read product install manifest", path, err)
	}
	if err := ValidateInstallManifest(manifest); err != nil {
		return ProductInstallManifest{}, err
	}
	return manifest, nil
}

func WriteBundleManifest(path string, manifest BundleManifest) error {
	if manifest.SchemaVersion != BundleSchema {
		return newError(CodeSchemaUnsupported, "unsupported bundle schema", manifest.SchemaVersion, nil)
	}
	return writePublicAsset(path, manifest)
}

func WriteCatalog(path string, catalog Catalog) error {
	if catalog.SchemaVersion != CatalogSchema {
		return newError(CodeSchemaUnsupported, "unsupported catalog schema", catalog.SchemaVersion, nil)
	}
	return writePublicAsset(path, catalog)
}

func WriteInstallManifest(path string, manifest ProductInstallManifest) error {
	if err := ValidateInstallManifest(manifest); err != nil {
		return err
	}
	return writePublicAsset(path, manifest)
}

func writePublicAsset(path string, value any) error {
	if err := writeJSONAtomic(path, value, 0o644); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return newError(CodeIO, "write public release asset", path, err)
		}
		return newError(CodeIO, "write public release asset", path, err)
	}
	return nil
}
