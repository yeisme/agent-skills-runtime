package skillsruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type transactionStatus string

const (
	transactionPrepared   transactionStatus = "prepared"
	transactionApplying   transactionStatus = "applying"
	transactionCommitted  transactionStatus = "committed"
	transactionRolledBack transactionStatus = "rolled_back"
)

type fileMutation struct {
	Target       string `json:"target"`
	Backup       string `json:"backup"`
	Existed      bool   `json:"existed"`
	ResultDigest string `json:"result_digest,omitempty"`
}

type transactionJournal struct {
	SchemaVersion        string            `json:"schema_version"`
	TransactionID        string            `json:"transaction_id"`
	Product              string            `json:"product"`
	Operation            PlanOperation     `json:"operation"`
	Status               transactionStatus `json:"status"`
	BaseRegistryRevision uint64            `json:"base_registry_revision"`
	TargetRevision       uint64            `json:"target_revision,omitempty"`
	ReceiptExisted       bool              `json:"receipt_existed"`
	Mutations            []fileMutation    `json:"mutations,omitempty"`
	StartedAt            time.Time         `json:"started_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
	Error                string            `json:"error,omitempty"`
}

func (m *Manager) commitPlan(lock *registryLock, registry Registry, plan InstallPlan, options ApplyOptions) (result ApplyResult, resultErr error) {
	currentReceipt, err := loadReceipt(m.paths.ReceiptPath())
	if err != nil {
		return ApplyResult{}, err
	}
	if currentReceipt != nil && currentReceipt.Product != plan.Product {
		return ApplyResult{}, newError(CodePlanInvalid, "product receipt belongs to another product", m.paths.ReceiptPath(), nil)
	}
	if len(plan.Actions) == 0 && currentReceipt != nil && snapshotEqual(currentReceipt.Current, plan.Desired) {
		return ApplyResult{
			SchemaVersion:    ProductReceiptSchema,
			Status:           "unchanged",
			Operation:        plan.Operation,
			Product:          plan.Product,
			RegistryRevision: registry.Revision,
			TransactionID:    currentReceipt.TransactionID,
			ReceiptPath:      m.paths.ReceiptPath(),
		}, nil
	}

	transactionID := m.newID()
	transactionRoot := filepath.Join(m.paths.TransactionsRoot(), transactionID)
	if err := os.MkdirAll(filepath.Join(transactionRoot, "backups"), 0o700); err != nil {
		return ApplyResult{}, newError(CodeIO, "create transaction directory", transactionRoot, err)
	}
	journal := transactionJournal{
		SchemaVersion:        TransactionSchema,
		TransactionID:        transactionID,
		Product:              plan.Product,
		Operation:            plan.Operation,
		Status:               transactionPrepared,
		BaseRegistryRevision: registry.Revision,
		ReceiptExisted:       currentReceipt != nil,
		StartedAt:            m.now(),
		UpdatedAt:            m.now(),
	}
	journalPath := filepath.Join(transactionRoot, "journal.json")
	if err := writeJSONAtomic(filepath.Join(transactionRoot, "registry-before.json"), registry, 0o600); err != nil {
		return ApplyResult{}, newError(CodeIO, "snapshot registry", transactionRoot, err)
	}
	if currentReceipt != nil {
		if err := writeJSONAtomic(filepath.Join(transactionRoot, "receipt-before.json"), currentReceipt, 0o600); err != nil {
			return ApplyResult{}, newError(CodeIO, "snapshot product receipt", transactionRoot, err)
		}
	}
	if err := writeJSONAtomic(journalPath, journal, 0o600); err != nil {
		return ApplyResult{}, newError(CodeIO, "write transaction journal", journalPath, err)
	}
	defer func() {
		if resultErr == nil {
			return
		}
		journal.Error = resultErr.Error()
		journal.UpdatedAt = m.now()
		_ = writeJSONAtomic(journalPath, journal, 0o600)
		if rollbackErr := m.rollbackTransaction(lock, transactionRoot, &journal); rollbackErr != nil {
			resultErr = &Error{Code: CodeTransactionIncomplete, Message: "transaction failed and automatic recovery was incomplete", Path: transactionRoot, Err: errors.Join(resultErr, rollbackErr)}
		}
	}()

	if err := lock.assertOwned(); err != nil {
		return ApplyResult{}, err
	}
	journal.Status = transactionApplying
	journal.UpdatedAt = m.now()
	if err := writeJSONAtomic(journalPath, journal, 0o600); err != nil {
		return ApplyResult{}, newError(CodeIO, "advance transaction journal", journalPath, err)
	}

	if err := m.prepareDesiredStore(transactionRoot, plan); err != nil {
		return ApplyResult{}, err
	}
	newRegistry, err := m.projectRegistry(registry, plan.Desired, transactionID)
	if err != nil {
		return ApplyResult{}, err
	}

	desiredMap := snapshotEntryMap(plan.Desired)
	for _, action := range plan.Actions {
		if action.Kind != ActionInstall && action.Kind != ActionReplace && action.Kind != ActionRemove {
			continue
		}
		entry, desired := desiredMap[registryKey(action.Runtime, action.SkillsDir, action.Skill)]
		resultDigest := ""
		if desired {
			resultDigest = entry.Digest
		}
		if err := m.mutateRuntimeTarget(lock, transactionRoot, journalPath, &journal, action, entry, resultDigest); err != nil {
			return ApplyResult{}, err
		}
	}

	newRegistry.Revision = registry.Revision + 1
	newRegistry.UpdatedAt = m.now()
	sortRegistry(&newRegistry)
	if err := lock.assertOwned(); err != nil {
		return ApplyResult{}, err
	}
	if err := writeJSONAtomic(m.paths.RegistryPath(), newRegistry, 0o600); err != nil {
		return ApplyResult{}, newError(CodeIO, "write shared registry", m.paths.RegistryPath(), err)
	}

	newReceipt := ProductReceipt{
		SchemaVersion:    ProductReceiptSchema,
		Product:          plan.Product,
		RegistryRevision: newRegistry.Revision,
		Current:          plan.Desired,
		TransactionID:    transactionID,
		UpdatedAt:        m.now(),
	}
	if currentReceipt != nil {
		previous := currentReceipt.Current
		newReceipt.Previous = &previous
	}
	if err := writeJSONAtomic(m.paths.ReceiptPath(), newReceipt, 0o600); err != nil {
		return ApplyResult{}, newError(CodeIO, "write product receipt", m.paths.ReceiptPath(), err)
	}

	journal.Status = transactionCommitted
	journal.TargetRevision = newRegistry.Revision
	journal.UpdatedAt = m.now()
	if err := writeJSONAtomic(journalPath, journal, 0o600); err != nil {
		return ApplyResult{}, newError(CodeTransactionIncomplete, "commit succeeded but journal finalization failed", journalPath, err)
	}

	result = ApplyResult{
		SchemaVersion:    ProductReceiptSchema,
		Status:           "success",
		Operation:        plan.Operation,
		Product:          plan.Product,
		RegistryRevision: newRegistry.Revision,
		TransactionID:    transactionID,
		ReceiptPath:      m.paths.ReceiptPath(),
		Evidence:         []string{journalPath, m.paths.RegistryPath(), m.paths.ReceiptPath()},
	}
	for _, action := range plan.Actions {
		key := string(action.Runtime) + ":" + action.Skill
		switch action.Kind {
		case ActionInstall, ActionReplace, ActionClaim:
			result.Installed = append(result.Installed, key)
		case ActionShare:
			result.Shared = append(result.Shared, key)
		case ActionRemove:
			result.Removed = append(result.Removed, key)
		case ActionPreserveDrift:
			result.Preserved = append(result.Preserved, key)
		}
	}
	sort.Strings(result.Installed)
	sort.Strings(result.Shared)
	sort.Strings(result.Removed)
	sort.Strings(result.Preserved)
	if len(result.Preserved) > 0 {
		result.Status = "partial"
	}
	return result, nil
}

func (m *Manager) prepareDesiredStore(transactionRoot string, plan InstallPlan) error {
	for _, entry := range plan.Desired.Entries {
		storePath, err := m.storePath(entry.Digest)
		if err != nil {
			return err
		}
		if pathExists(storePath) {
			ok, verifyErr := verifySkillDir(storePath, entry.Files)
			if verifyErr != nil || !ok {
				return newError(CodeDigestMismatch, "content-addressed store entry is invalid", storePath, verifyErr)
			}
			continue
		}
		var source string
		switch plan.SourceKind {
		case sourceBundle:
			source = filepath.Join(plan.SourceRoot, "skills", entry.Skill)
		case sourceRuntime:
			source = filepath.Join(entry.SkillsDir, entry.Skill)
		case sourceStore:
			return newError(CodeDigestMismatch, "rollback store entry is missing", storePath, nil)
		default:
			return newError(CodePlanInvalid, "plan has no source for desired Skill bytes", entry.Skill, nil)
		}
		ok, verifyErr := verifySkillDir(source, entry.Files)
		if verifyErr != nil || !ok {
			return newError(CodeDigestMismatch, "plan source does not match desired Skill inventory", source, verifyErr)
		}
		hexDigest, _ := digestHex(entry.Digest)
		temp := filepath.Join(transactionRoot, "staging", "store", hexDigest)
		_ = os.RemoveAll(temp)
		if err := copySkillDir(source, temp, entry.Files); err != nil {
			_ = os.RemoveAll(temp)
			return newError(CodeIO, "stage content-addressed Skill", temp, err)
		}
		if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
			_ = os.RemoveAll(temp)
			return newError(CodeIO, "create content-addressed store", filepath.Dir(storePath), err)
		}
		if err := os.Rename(temp, storePath); err != nil {
			if pathExists(storePath) {
				_ = os.RemoveAll(temp)
				ok, verifyErr = verifySkillDir(storePath, entry.Files)
				if verifyErr == nil && ok {
					continue
				}
			}
			_ = os.RemoveAll(temp)
			return newError(CodeIO, "commit content-addressed Skill", storePath, err)
		}
	}
	return nil
}

func (m *Manager) projectRegistry(registry Registry, desired ProductSnapshot, transactionID string) (Registry, error) {
	result := Registry{SchemaVersion: RegistrySchema, Revision: registry.Revision, UpdatedAt: registry.UpdatedAt, Entries: append([]RegistryEntry(nil), registry.Entries...)}
	for i := range result.Entries {
		claims := make([]Claim, 0, len(result.Entries[i].Claims))
		for _, claim := range result.Entries[i].Claims {
			if claim.Product != desired.Identity.Product {
				claims = append(claims, claim)
			}
		}
		result.Entries[i].Claims = claims
	}
	index := registryEntryIndex(result)
	for _, desiredEntry := range desired.Entries {
		key := registryKey(desiredEntry.Runtime, desiredEntry.SkillsDir, desiredEntry.Skill)
		claim := Claim{
			Product:        desired.Identity.Product,
			ProductVersion: desired.Identity.ProductVersion,
			BundleVersion:  desired.Identity.BundleVersion,
			BundleDigest:   desired.Identity.BundleDigest,
			TransactionID:  transactionID,
			ClaimedAt:      m.now(),
		}
		if position, exists := index[key]; exists {
			entry := &result.Entries[position]
			if entry.Digest != desiredEntry.Digest && len(entry.Claims) > 0 {
				return Registry{}, newError(CodeDigestConflict, "cannot replace a Skill claimed by another product", filepath.Join(entry.SkillsDir, entry.Skill), nil)
			}
			entry.Digest = desiredEntry.Digest
			entry.Files = normalizeFiles(desiredEntry.Files)
			entry.Claims = append(entry.Claims, claim)
			continue
		}
		result.Entries = append(result.Entries, RegistryEntry{
			Runtime: desiredEntry.Runtime, SkillsDir: desiredEntry.SkillsDir, Skill: desiredEntry.Skill,
			Digest: desiredEntry.Digest, Files: normalizeFiles(desiredEntry.Files), Claims: []Claim{claim},
		})
		index[key] = len(result.Entries) - 1
	}
	filtered := result.Entries[:0]
	for _, entry := range result.Entries {
		if len(entry.Claims) > 0 {
			filtered = append(filtered, entry)
			continue
		}
		target := filepath.Join(entry.SkillsDir, entry.Skill)
		ok, err := verifySkillDir(target, entry.Files)
		if errors.Is(err, os.ErrNotExist) || (err == nil && ok) {
			continue
		}
		// A zero-claim drifted entry remains as an orphan so future products cannot
		// silently adopt or overwrite user-modified managed bytes.
		filtered = append(filtered, entry)
	}
	result.Entries = filtered
	return result, nil
}

func registryEntryIndex(registry Registry) map[string]int {
	result := make(map[string]int, len(registry.Entries))
	for index, entry := range registry.Entries {
		result[registryKey(entry.Runtime, entry.SkillsDir, entry.Skill)] = index
	}
	return result
}

func (m *Manager) mutateRuntimeTarget(lock *registryLock, transactionRoot, journalPath string, journal *transactionJournal, action PlanAction, desired SnapshotEntry, resultDigest string) error {
	if err := lock.assertOwned(); err != nil {
		return err
	}
	target := filepath.Join(action.SkillsDir, action.Skill)
	resolvedTarget, resolveErr := resolvePath(target)
	if resolveErr != nil || !pathWithinAny(m.runtimeRoots, resolvedTarget) || resolvedTarget != filepath.Clean(target) {
		return newError(CodePlanInvalid, "runtime target is outside the allowed roots or changed through a symlink", target, resolveErr)
	}
	mutation := fileMutation{
		Target:       target,
		Backup:       filepath.Join(transactionRoot, "backups", fmt.Sprintf("%04d-%s-%s", len(journal.Mutations)+1, action.Runtime, action.Skill)),
		Existed:      pathExists(target),
		ResultDigest: resultDigest,
	}
	journal.Mutations = append(journal.Mutations, mutation)
	journal.UpdatedAt = m.now()
	if err := writeJSONAtomic(journalPath, journal, 0o600); err != nil {
		return newError(CodeIO, "record runtime mutation", journalPath, err)
	}
	if mutation.Existed {
		if err := moveDir(target, mutation.Backup); err != nil {
			return newError(CodeIO, "backup runtime Skill", target, err)
		}
	}
	if action.Kind == ActionRemove {
		return nil
	}
	storePath, err := m.storePath(desired.Digest)
	if err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(target), "."+action.Skill+".tmp-"+m.newID())
	_ = os.RemoveAll(temp)
	if err := copySkillDir(storePath, temp, desired.Files); err != nil {
		_ = os.RemoveAll(temp)
		return newError(CodeIO, "stage runtime Skill", temp, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		_ = os.RemoveAll(temp)
		return newError(CodeIO, "create runtime skills directory", filepath.Dir(target), err)
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.RemoveAll(temp)
		return newError(CodeIO, "commit runtime Skill", target, err)
	}
	return nil
}

func (m *Manager) storePath(digest string) (string, error) {
	hexDigest, err := digestHex(digest)
	if err != nil {
		return "", newError(CodeInvalidArgument, "invalid Skill digest", digest, err)
	}
	return filepath.Join(m.paths.StoreRoot(), hexDigest), nil
}

func (m *Manager) recoverIncompleteTransactions(lock *registryLock) error {
	paths := incompleteTransactionPaths(m.paths.TransactionsRoot())
	for _, transactionRoot := range paths {
		var journal transactionJournal
		journalPath := filepath.Join(transactionRoot, "journal.json")
		if err := readJSONFile(journalPath, &journal); err != nil {
			return newError(CodeTransactionIncomplete, "read incomplete transaction journal", journalPath, err)
		}
		if err := m.rollbackTransaction(lock, transactionRoot, &journal); err != nil {
			return err
		}
	}
	return nil
}

func incompleteTransactionPaths(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	result := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		transactionRoot := filepath.Join(root, entry.Name())
		var journal transactionJournal
		if readJSONFile(filepath.Join(transactionRoot, "journal.json"), &journal) == nil && journal.Status != transactionCommitted && journal.Status != transactionRolledBack {
			result = append(result, transactionRoot)
		}
	}
	sort.Strings(result)
	return result
}

func (m *Manager) rollbackTransaction(lock *registryLock, transactionRoot string, journal *transactionJournal) error {
	if err := lock.assertOwned(); err != nil {
		return err
	}
	for index := len(journal.Mutations) - 1; index >= 0; index-- {
		mutation := journal.Mutations[index]
		if pathExists(mutation.Target) {
			if mutation.ResultDigest != "" {
				actualFiles, err := inspectSkillDir(mutation.Target)
				if err != nil {
					return newError(CodeTransactionIncomplete, "cannot inspect failed transaction target", mutation.Target, err)
				}
				actualDigest, err := SkillDigest(actualFiles)
				if err != nil || actualDigest != mutation.ResultDigest {
					return newError(CodeTransactionIncomplete, "failed transaction target changed after the crash", mutation.Target, err)
				}
			} else {
				return newError(CodeTransactionIncomplete, "removed target was recreated after the crash", mutation.Target, nil)
			}
			if err := os.RemoveAll(mutation.Target); err != nil {
				return newError(CodeTransactionIncomplete, "remove failed transaction target", mutation.Target, err)
			}
		}
		if mutation.Existed && pathExists(mutation.Backup) {
			if err := moveDir(mutation.Backup, mutation.Target); err != nil {
				return newError(CodeTransactionIncomplete, "restore transaction backup", mutation.Target, err)
			}
		}
	}
	var before Registry
	registryBeforePath := filepath.Join(transactionRoot, "registry-before.json")
	if err := readJSONFile(registryBeforePath, &before); err != nil {
		return newError(CodeTransactionIncomplete, "read registry recovery snapshot", registryBeforePath, err)
	}
	if err := writeJSONAtomic(m.paths.RegistryPath(), before, 0o600); err != nil {
		return newError(CodeTransactionIncomplete, "restore shared registry", m.paths.RegistryPath(), err)
	}
	receiptBeforePath := filepath.Join(transactionRoot, "receipt-before.json")
	if journal.ReceiptExisted {
		var receipt ProductReceipt
		if err := readJSONFile(receiptBeforePath, &receipt); err != nil {
			return newError(CodeTransactionIncomplete, "read receipt recovery snapshot", receiptBeforePath, err)
		}
		if err := writeJSONAtomic(m.paths.ReceiptPath(), receipt, 0o600); err != nil {
			return newError(CodeTransactionIncomplete, "restore product receipt", m.paths.ReceiptPath(), err)
		}
	} else if err := os.Remove(m.paths.ReceiptPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return newError(CodeTransactionIncomplete, "remove partial product receipt", m.paths.ReceiptPath(), err)
	}
	journal.Status = transactionRolledBack
	journal.UpdatedAt = m.now()
	if err := writeJSONAtomic(filepath.Join(transactionRoot, "journal.json"), journal, 0o600); err != nil {
		return newError(CodeTransactionIncomplete, "finalize transaction rollback", transactionRoot, err)
	}
	return nil
}
