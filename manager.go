package skillsruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

const (
	sourceBundle  = "bundle"
	sourceStore   = "store"
	sourceRuntime = "runtime"
	sourceNone    = "none"
)

// PlanInstall prepares an exact bundle snapshot for the selected user-global runtimes.
func (m *Manager) PlanInstall(bundle Bundle, targets []RuntimeTarget) (InstallPlan, error) {
	if err := bundle.Validate(); err != nil {
		return InstallPlan{}, err
	}
	normalizedTargets, err := normalizeRuntimeTargets(targets, m.runtimeRoots)
	if err != nil {
		return InstallPlan{}, err
	}
	bundleDigest := bundle.Digest
	if bundleDigest == "" {
		bundleDigest, err = bundleIdentityDigest(bundle.Manifest, bundle.Catalog)
		if err != nil {
			return InstallPlan{}, newError(CodeBundleInvalid, "compute bundle identity", bundle.Root, err)
		}
	}
	desired := ProductSnapshot{
		Identity: ProductIdentity{
			Product:        bundle.Manifest.Product,
			ProductVersion: bundle.Manifest.ProductVersion,
			BundleVersion:  bundle.Manifest.BundleVersion,
			BundleDigest:   bundleDigest,
		},
	}
	for _, target := range normalizedTargets {
		for _, skill := range sortedBundleSkills(bundle.Manifest.Skills) {
			desired.Entries = append(desired.Entries, SnapshotEntry{
				Runtime:   target.Runtime,
				SkillsDir: target.SkillsDir,
				Skill:     skill.Name,
				Digest:    skill.Digest,
				Files:     normalizeFiles(skill.Files),
			})
		}
	}
	return m.planDesired(OperationInstall, desired, bundle.Root, sourceBundle)
}

// PlanUninstall releases one product's claims while preserving other products and drift.
func (m *Manager) PlanUninstall(product string) (InstallPlan, error) {
	receipt, err := loadReceipt(m.paths.ReceiptPath())
	if err != nil {
		return InstallPlan{}, err
	}
	if receipt == nil || receipt.Product != product || len(receipt.Current.Entries) == 0 {
		return InstallPlan{}, newError(CodeNotInstalled, "product Agent Skills are not installed", product, nil)
	}
	desired := ProductSnapshot{Identity: receipt.Current.Identity, Entries: []SnapshotEntry{}}
	return m.planDesired(OperationUninstall, desired, "", sourceNone)
}

// PlanRollback restores the previous product claim set through a new transaction.
func (m *Manager) PlanRollback(product string) (InstallPlan, error) {
	receipt, err := loadReceipt(m.paths.ReceiptPath())
	if err != nil {
		return InstallPlan{}, err
	}
	if receipt == nil || receipt.Product != product || receipt.Previous == nil {
		return InstallPlan{}, newError(CodeNotInstalled, "no previous Agent Skills claim set is available", product, nil)
	}
	return m.planDesired(OperationRollback, *receipt.Previous, m.paths.StoreRoot(), sourceStore)
}

// PlanLegacyAdoption explicitly adopts a caller-validated legacy product snapshot.
func (m *Manager) PlanLegacyAdoption(snapshot ProductSnapshot) (InstallPlan, error) {
	if len(snapshot.Entries) == 0 {
		return InstallPlan{}, newError(CodeInvalidArgument, "legacy snapshot contains no entries", snapshot.Identity.Product, nil)
	}
	return m.planDesired(OperationAdopt, snapshot, "", sourceRuntime)
}

func (m *Manager) planDesired(operation PlanOperation, desired ProductSnapshot, sourceRoot, sourceKind string) (InstallPlan, error) {
	desired = normalizeSnapshot(desired)
	if err := validateSnapshot(m.runtimeRoots, desired); err != nil {
		return InstallPlan{}, err
	}
	registry, err := loadRegistry(m.paths.RegistryPath())
	if err != nil {
		return InstallPlan{}, err
	}
	actions, conflicts, err := m.evaluatePlan(registry, operation, desired)
	if err != nil {
		return InstallPlan{}, err
	}
	return InstallPlan{
		SchemaVersion:        InstallPlanSchema,
		PlanID:               m.newID(),
		Operation:            operation,
		Product:              desired.Identity.Product,
		BaseRegistryRevision: registry.Revision,
		Desired:              desired,
		SourceRoot:           sourceRoot,
		SourceKind:           sourceKind,
		Actions:              actions,
		Conflicts:            conflicts,
		RequiresConfirmation: true,
		CreatedAt:            m.now(),
	}, nil
}

func (m *Manager) evaluatePlan(registry Registry, operation PlanOperation, desired ProductSnapshot) ([]PlanAction, []Conflict, error) {
	entries := registryEntryMap(registry)
	desiredEntries := snapshotEntryMap(desired)
	actions := make([]PlanAction, 0)
	conflicts := make([]Conflict, 0)

	for _, desiredEntry := range sortedSnapshotEntries(desired.Entries) {
		key := registryKey(desiredEntry.Runtime, desiredEntry.SkillsDir, desiredEntry.Skill)
		target := filepath.Join(desiredEntry.SkillsDir, desiredEntry.Skill)
		existing, managed := entries[key]
		if operation == OperationAdopt && !managed {
			ok, verifyErr := verifySkillDir(target, desiredEntry.Files)
			if verifyErr != nil || !ok {
				conflicts = append(conflicts, planConflict(CodeUserDrift, true, desiredEntry, target, "legacy Skill bytes do not match the adoption snapshot"))
				continue
			}
			actions = append(actions, planAction(ActionClaim, desiredEntry, "adopt validated legacy ownership"))
			continue
		}
		if !managed {
			if pathExists(target) {
				conflicts = append(conflicts, planConflict(CodeUnmanagedConflict, true, desiredEntry, target, "runtime Skill directory is not managed by the shared registry"))
				actions = append(actions, planAction(ActionReplace, desiredEntry, "replace explicitly reviewed unmanaged directory"))
			} else {
				actions = append(actions, planAction(ActionInstall, desiredEntry, "install missing Skill"))
			}
			continue
		}
		if existing.Digest != desiredEntry.Digest {
			if hasOtherClaims(existing.Claims, desired.Identity.Product) {
				conflicts = append(conflicts, planConflict(CodeDigestConflict, true, desiredEntry, target, "another product claims a different digest for this Skill"))
				continue
			}
			ok, verifyErr := verifySkillDir(target, existing.Files)
			if errors.Is(verifyErr, os.ErrNotExist) {
				actions = append(actions, planAction(ActionReplace, desiredEntry, "restore missing managed Skill with new version"))
				continue
			}
			if verifyErr != nil || !ok {
				conflicts = append(conflicts, planConflict(CodeUserDrift, true, desiredEntry, target, "managed Skill was modified before version replacement"))
				actions = append(actions, planAction(ActionReplace, desiredEntry, "replace explicitly reviewed user drift"))
				continue
			}
			actions = append(actions, planAction(ActionReplace, desiredEntry, "replace an unshared managed version"))
			continue
		}
		ok, verifyErr := verifySkillDir(target, existing.Files)
		if errors.Is(verifyErr, os.ErrNotExist) {
			conflicts = append(conflicts, planConflict(CodeUserDrift, true, desiredEntry, target, "managed Skill directory is missing"))
			actions = append(actions, planAction(ActionReplace, desiredEntry, "restore missing managed Skill"))
			continue
		}
		if verifyErr != nil || !ok {
			conflicts = append(conflicts, planConflict(CodeUserDrift, true, desiredEntry, target, "managed Skill bytes differ from the registry"))
			actions = append(actions, planAction(ActionReplace, desiredEntry, "replace explicitly reviewed user drift"))
			continue
		}
		claim, claimed := findClaim(existing.Claims, desired.Identity.Product)
		if claimed && claimMatchesIdentity(claim, desired.Identity) {
			continue
		}
		if hasOtherClaims(existing.Claims, desired.Identity.Product) {
			actions = append(actions, planAction(ActionShare, desiredEntry, "share identical managed bytes"))
		} else {
			actions = append(actions, planAction(ActionClaim, desiredEntry, "record product claim"))
		}
	}

	for _, existing := range registry.Entries {
		if _, claimed := findClaim(existing.Claims, desired.Identity.Product); !claimed {
			continue
		}
		key := registryKey(existing.Runtime, existing.SkillsDir, existing.Skill)
		if _, keep := desiredEntries[key]; keep {
			continue
		}
		current := SnapshotEntry{Runtime: existing.Runtime, SkillsDir: existing.SkillsDir, Skill: existing.Skill, Digest: existing.Digest, Files: existing.Files}
		target := filepath.Join(existing.SkillsDir, existing.Skill)
		if hasOtherClaims(existing.Claims, desired.Identity.Product) {
			actions = append(actions, planAction(ActionRelease, current, "release product claim and preserve shared bytes"))
			continue
		}
		ok, verifyErr := verifySkillDir(target, existing.Files)
		if errors.Is(verifyErr, os.ErrNotExist) || (verifyErr == nil && ok) {
			actions = append(actions, planAction(ActionRemove, current, "remove unchanged last-claim Skill"))
			continue
		}
		conflicts = append(conflicts, planConflict(CodeUserDrift, false, current, target, "last-claim Skill was modified and will be preserved"))
		actions = append(actions, planAction(ActionPreserveDrift, current, "release claim but preserve user-modified bytes"))
	}
	sortPlan(actions, conflicts)
	return actions, conflicts, nil
}

// Apply rechecks the reviewed plan under the shared lock and commits one transaction.
func (m *Manager) Apply(ctx context.Context, plan InstallPlan, options ApplyOptions) (ApplyResult, error) {
	plan = normalizeInstallPlan(plan)
	if !options.Confirm {
		return ApplyResult{}, newError(CodeConfirmationRequired, "plan application requires explicit confirmation", plan.PlanID, nil)
	}
	if err := validatePlan(m.runtimeRoots, plan); err != nil {
		return ApplyResult{}, err
	}
	lock, err := m.acquireLock(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	defer lock.release()
	if err := m.recoverIncompleteTransactions(lock); err != nil {
		return ApplyResult{}, err
	}
	registry, err := loadRegistry(m.paths.RegistryPath())
	if err != nil {
		return ApplyResult{}, err
	}
	if registry.Revision != plan.BaseRegistryRevision {
		return ApplyResult{}, newError(CodePlanStale, "registry revision changed after planning", plan.PlanID, nil)
	}
	freshActions, freshConflicts, err := m.evaluatePlan(registry, plan.Operation, plan.Desired)
	if err != nil {
		return ApplyResult{}, err
	}
	if !reflect.DeepEqual(freshActions, plan.Actions) || !reflect.DeepEqual(freshConflicts, plan.Conflicts) {
		return ApplyResult{}, newError(CodePlanStale, "filesystem state changed after planning", plan.PlanID, nil)
	}
	for _, conflict := range freshConflicts {
		if conflict.Code == CodeDigestConflict || (conflict.Blocking && !options.ReplaceConflicts) {
			return ApplyResult{}, &Error{Code: conflict.Code, Message: conflict.Message, Path: conflict.Path}
		}
	}
	return m.commitPlan(lock, registry, plan, options)
}

// Doctor verifies the product receipt, shared claims, exact versions, and runtime bytes.
func (m *Manager) Doctor(product string, expected *ProductIdentity) (DoctorReport, error) {
	registry, err := loadRegistry(m.paths.RegistryPath())
	if err != nil {
		return DoctorReport{}, err
	}
	report := DoctorReport{Status: "healthy", Product: product, RegistryRevision: registry.Revision, ReceiptPath: m.paths.ReceiptPath()}
	receipt, err := loadReceipt(m.paths.ReceiptPath())
	if err != nil {
		return DoctorReport{}, err
	}
	if receipt == nil || receipt.Product != product || len(receipt.Current.Entries) == 0 {
		report.Status = "not_installed"
		report.Issues = append(report.Issues, DoctorIssue{Code: CodeNotInstalled, Message: "product Agent Skills are not installed"})
		return report, nil
	}
	if receipt.RegistryRevision > registry.Revision {
		report.Issues = append(report.Issues, DoctorIssue{Code: CodeTransactionIncomplete, Message: "product receipt references a future registry revision"})
	}
	if expected != nil && (receipt.Current.Identity.ProductVersion != expected.ProductVersion || receipt.Current.Identity.BundleVersion != expected.BundleVersion || receipt.Current.Identity.BundleDigest != expected.BundleDigest) {
		report.Issues = append(report.Issues, DoctorIssue{Code: CodeVersionMismatch, Message: "installed Agent Skills do not match the running product release"})
	}
	entries := registryEntryMap(registry)
	for _, snapshot := range receipt.Current.Entries {
		key := registryKey(snapshot.Runtime, snapshot.SkillsDir, snapshot.Skill)
		entry, exists := entries[key]
		if !exists || entry.Digest != snapshot.Digest {
			report.Issues = append(report.Issues, DoctorIssue{Code: CodeTransactionIncomplete, Runtime: snapshot.Runtime, Skill: snapshot.Skill, Path: filepath.Join(snapshot.SkillsDir, snapshot.Skill), Message: "product receipt and shared registry disagree"})
			continue
		}
		if _, claimed := findClaim(entry.Claims, product); !claimed {
			report.Issues = append(report.Issues, DoctorIssue{Code: CodeTransactionIncomplete, Runtime: snapshot.Runtime, Skill: snapshot.Skill, Path: filepath.Join(snapshot.SkillsDir, snapshot.Skill), Message: "shared registry is missing the product claim"})
		}
		target := filepath.Join(snapshot.SkillsDir, snapshot.Skill)
		ok, verifyErr := verifySkillDir(target, snapshot.Files)
		if verifyErr != nil || !ok {
			report.Issues = append(report.Issues, DoctorIssue{Code: CodeUserDrift, Runtime: snapshot.Runtime, Skill: snapshot.Skill, Path: target, Message: "managed Skill bytes differ from the receipt"})
		}
	}
	if incomplete := incompleteTransactionPaths(m.paths.TransactionsRoot()); len(incomplete) > 0 {
		for _, transactionPath := range incomplete {
			report.Issues = append(report.Issues, DoctorIssue{Code: CodeTransactionIncomplete, Path: transactionPath, Message: "an incomplete transaction requires recovery"})
		}
	}
	if len(report.Issues) > 0 {
		report.Status = "degraded"
		for _, issue := range report.Issues {
			if issue.Code == CodeVersionMismatch && len(report.Issues) == 1 {
				report.Status = "version_mismatch"
			}
		}
	}
	return report, nil
}

// CompatibleSkills returns exact current receipt entries that doctor found healthy.
func CompatibleSkills(receipt ProductReceipt, report DoctorReport) map[string]bool {
	blocked := map[string]bool{}
	for _, issue := range report.Issues {
		if issue.Code == CodeVersionMismatch || issue.Code == CodeNotInstalled {
			return map[string]bool{}
		}
		if issue.Skill != "" {
			blocked[issue.Skill] = true
		}
	}
	compatible := map[string]bool{}
	if report.Status == "version_mismatch" || report.Status == "not_installed" {
		return compatible
	}
	for _, entry := range receipt.Current.Entries {
		if !blocked[entry.Skill] {
			compatible[entry.Skill] = true
		}
	}
	return compatible
}

// ReadReceipt returns the current product receipt, if any.
func (m *Manager) ReadReceipt() (*ProductReceipt, error) { return loadReceipt(m.paths.ReceiptPath()) }

// ReadRegistry returns a validated copy of the shared registry.
func (m *Manager) ReadRegistry() (Registry, error) { return loadRegistry(m.paths.RegistryPath()) }

// WritePlan atomically writes a product-authored review plan.
func WritePlan(path string, plan InstallPlan) error {
	if plan.SchemaVersion != InstallPlanSchema || plan.PlanID == "" || plan.Product == "" || plan.Product != plan.Desired.Identity.Product {
		return newError(CodePlanInvalid, "install plan identity is incomplete", plan.PlanID, nil)
	}
	if err := writeJSONAtomic(path, plan, 0o600); err != nil {
		return newError(CodeIO, "write install plan", path, err)
	}
	return nil
}

// ReadPlan reads and validates a serialized install plan schema.
func ReadPlan(path string) (InstallPlan, error) {
	var plan InstallPlan
	if err := readJSONFile(path, &plan); err != nil {
		return InstallPlan{}, newError(CodeIO, "read install plan", path, err)
	}
	if plan.SchemaVersion != InstallPlanSchema {
		return InstallPlan{}, newError(CodeSchemaUnsupported, "unsupported install plan schema", path, nil)
	}
	return normalizeInstallPlan(plan), nil
}

func normalizeInstallPlan(plan InstallPlan) InstallPlan {
	if plan.Actions == nil {
		plan.Actions = []PlanAction{}
	}
	if plan.Conflicts == nil {
		plan.Conflicts = []Conflict{}
	}
	return plan
}

func validateSnapshot(runtimeRoots []string, snapshot ProductSnapshot) error {
	if !skillNamePattern.MatchString(snapshot.Identity.Product) || snapshot.Identity.ProductVersion == "" || snapshot.Identity.BundleVersion == "" {
		return newError(CodeInvalidArgument, "product identity is incomplete", snapshot.Identity.Product, nil)
	}
	if _, err := normalizeDigest(snapshot.Identity.BundleDigest); err != nil {
		return newError(CodeInvalidArgument, "bundle digest is invalid", snapshot.Identity.Product, err)
	}
	seen := map[string]struct{}{}
	for _, entry := range snapshot.Entries {
		if !skillNamePattern.MatchString(entry.Skill) {
			return newError(CodeInvalidArgument, "invalid Skill name", entry.Skill, nil)
		}
		resolved, resolveErr := resolvePath(entry.SkillsDir)
		if resolveErr != nil || !pathWithinAny(runtimeRoots, resolved) || resolved != filepath.Clean(entry.SkillsDir) {
			return newError(CodeInvalidArgument, "runtime path is outside the allowed roots or changed through a symlink", entry.SkillsDir, resolveErr)
		}
		if entry.Runtime != RuntimeAgents && entry.Runtime != RuntimeCodex && entry.Runtime != RuntimeClaude {
			return newError(CodeInvalidArgument, "invalid runtime", string(entry.Runtime), nil)
		}
		filePaths := map[string]struct{}{}
		hasSkillMD := false
		for _, file := range entry.Files {
			if _, duplicate := filePaths[file.Path]; duplicate {
				return newError(CodeInvalidArgument, "duplicate file in Skill snapshot", file.Path, nil)
			}
			filePaths[file.Path] = struct{}{}
			hasSkillMD = hasSkillMD || file.Path == "SKILL.md"
			if _, err := validateBundleRelativePath(file.Path); err != nil {
				return newError(CodeBundlePathForbidden, err.Error(), file.Path, err)
			}
		}
		if !hasSkillMD {
			return newError(CodeInvalidArgument, "Skill snapshot requires SKILL.md", entry.Skill, nil)
		}
		computed, err := SkillDigest(entry.Files)
		if err != nil {
			return newError(CodeInvalidArgument, "invalid Skill file inventory", entry.Skill, err)
		}
		digest, err := normalizeDigest(entry.Digest)
		if err != nil || computed != digest {
			return newError(CodeDigestMismatch, "snapshot Skill digest does not match its files", entry.Skill, err)
		}
		key := registryKey(entry.Runtime, entry.SkillsDir, entry.Skill)
		if _, duplicate := seen[key]; duplicate {
			return newError(CodeInvalidArgument, "duplicate Skill snapshot entry", entry.Skill, nil)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validatePlan(runtimeRoots []string, plan InstallPlan) error {
	if plan.SchemaVersion != InstallPlanSchema || plan.PlanID == "" || plan.Product == "" {
		return newError(CodePlanInvalid, "install plan identity is incomplete", plan.PlanID, nil)
	}
	if plan.Product != plan.Desired.Identity.Product {
		return newError(CodePlanInvalid, "plan product differs from desired snapshot", plan.PlanID, nil)
	}
	if plan.SourceKind != sourceBundle && plan.SourceKind != sourceStore && plan.SourceKind != sourceRuntime && plan.SourceKind != sourceNone {
		return newError(CodePlanInvalid, "unknown plan source kind", plan.SourceKind, nil)
	}
	return validateSnapshot(runtimeRoots, plan.Desired)
}

func normalizeFiles(files []FileDigest) []FileDigest {
	result := append([]FileDigest(nil), files...)
	for i := range result {
		result[i].SHA256, _ = normalizeDigest(result[i].SHA256)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func normalizeSnapshot(snapshot ProductSnapshot) ProductSnapshot {
	snapshot.Identity.BundleDigest, _ = normalizeDigest(snapshot.Identity.BundleDigest)
	for index := range snapshot.Entries {
		snapshot.Entries[index].SkillsDir = filepath.Clean(snapshot.Entries[index].SkillsDir)
		snapshot.Entries[index].Digest, _ = normalizeDigest(snapshot.Entries[index].Digest)
		snapshot.Entries[index].Files = normalizeFiles(snapshot.Entries[index].Files)
	}
	snapshot.Entries = sortedSnapshotEntries(snapshot.Entries)
	return snapshot
}

func registryEntryMap(registry Registry) map[string]RegistryEntry {
	result := make(map[string]RegistryEntry, len(registry.Entries))
	for _, entry := range registry.Entries {
		result[registryKey(entry.Runtime, entry.SkillsDir, entry.Skill)] = entry
	}
	return result
}

func snapshotEntryMap(snapshot ProductSnapshot) map[string]SnapshotEntry {
	result := make(map[string]SnapshotEntry, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		result[registryKey(entry.Runtime, entry.SkillsDir, entry.Skill)] = entry
	}
	return result
}

func sortedSnapshotEntries(entries []SnapshotEntry) []SnapshotEntry {
	result := append([]SnapshotEntry(nil), entries...)
	sort.Slice(result, func(i, j int) bool {
		return registryKey(result[i].Runtime, result[i].SkillsDir, result[i].Skill) < registryKey(result[j].Runtime, result[j].SkillsDir, result[j].Skill)
	})
	return result
}

func planAction(kind PlanActionKind, entry SnapshotEntry, reason string) PlanAction {
	return PlanAction{Kind: kind, Runtime: entry.Runtime, SkillsDir: entry.SkillsDir, Skill: entry.Skill, Digest: entry.Digest, Reason: reason}
}

func planConflict(code string, blocking bool, entry SnapshotEntry, path, message string) Conflict {
	return Conflict{Code: code, Blocking: blocking, Runtime: entry.Runtime, SkillsDir: entry.SkillsDir, Skill: entry.Skill, Path: path, Message: message}
}

func sortPlan(actions []PlanAction, conflicts []Conflict) {
	sort.Slice(actions, func(i, j int) bool {
		left := registryKey(actions[i].Runtime, actions[i].SkillsDir, actions[i].Skill) + "\x00" + string(actions[i].Kind)
		right := registryKey(actions[j].Runtime, actions[j].SkillsDir, actions[j].Skill) + "\x00" + string(actions[j].Kind)
		return left < right
	})
	sort.Slice(conflicts, func(i, j int) bool {
		left := registryKey(conflicts[i].Runtime, conflicts[i].SkillsDir, conflicts[i].Skill) + "\x00" + conflicts[i].Code
		right := registryKey(conflicts[j].Runtime, conflicts[j].SkillsDir, conflicts[j].Skill) + "\x00" + conflicts[j].Code
		return left < right
	})
}

func findClaim(claims []Claim, product string) (Claim, bool) {
	for _, claim := range claims {
		if claim.Product == product {
			return claim, true
		}
	}
	return Claim{}, false
}

func hasOtherClaims(claims []Claim, product string) bool {
	for _, claim := range claims {
		if claim.Product != product {
			return true
		}
	}
	return false
}

func claimMatchesIdentity(claim Claim, identity ProductIdentity) bool {
	return claim.Product == identity.Product && claim.ProductVersion == identity.ProductVersion && claim.BundleVersion == identity.BundleVersion && claim.BundleDigest == identity.BundleDigest
}

func snapshotEqual(left, right ProductSnapshot) bool {
	return reflect.DeepEqual(left, right)
}
