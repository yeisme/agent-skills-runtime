package skillsruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestMultiProductClaimsAndLastClaimDelete(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	eikona := testManager(t, home, "eikona")
	scaena := testManager(t, home, "scaena")

	eikonaBundle := testBundle(t, t.TempDir(), "eikona", "v1.0.0", map[string]string{"shared-skill": "# Shared\n"})
	plan, err := eikona.PlanInstall(eikonaBundle, []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eikona.Apply(context.Background(), plan, ApplyOptions{Confirm: true}); err != nil {
		t.Fatal(err)
	}

	scaenaBundle := testBundle(t, t.TempDir(), "scaena", "v1.0.0", map[string]string{"shared-skill": "# Shared\n"})
	plan, err = scaena.PlanInstall(scaenaBundle, []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionShare {
		t.Fatalf("share plan = %#v", plan.Actions)
	}
	if _, err := scaena.Apply(context.Background(), plan, ApplyOptions{Confirm: true}); err != nil {
		t.Fatal(err)
	}
	registry, err := scaena.ReadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 1 || len(registry.Entries[0].Claims) != 2 {
		t.Fatalf("registry = %#v", registry)
	}

	uninstall, err := eikona.PlanUninstall("eikona")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eikona.Apply(context.Background(), uninstall, ApplyOptions{Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(target.SkillsDir, "shared-skill")) {
		t.Fatal("shared bytes were removed with another claim present")
	}

	uninstall, err = scaena.PlanUninstall("scaena")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scaena.Apply(context.Background(), uninstall, ApplyOptions{Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if pathExists(filepath.Join(target.SkillsDir, "shared-skill")) {
		t.Fatal("last-claim bytes were not removed")
	}
}

func TestDifferentDigestFailsClosed(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	first := testManager(t, home, "eikona")
	second := testManager(t, home, "scaena")
	installBundle(t, first, testBundle(t, t.TempDir(), "eikona", "v1", map[string]string{"shared-skill": "one"}), target)

	plan, err := second.PlanInstall(testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"shared-skill": "two"}), []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Code != CodeDigestConflict {
		t.Fatalf("conflicts = %#v", plan.Conflicts)
	}
	if _, err := second.Apply(context.Background(), plan, ApplyOptions{Confirm: true, ReplaceConflicts: true}); ErrorCode(err) != CodeDigestConflict {
		t.Fatalf("apply error code = %q, err=%v", ErrorCode(err), err)
	}
}

func TestUnmanagedConflictRequiresExplicitReplacement(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	dir := filepath.Join(target.SkillsDir, "shared-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, home, "scaena")
	plan, err := manager.PlanInstall(testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"shared-skill": "same"}), []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Code != CodeUnmanagedConflict {
		t.Fatalf("conflicts = %#v", plan.Conflicts)
	}
	if _, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true}); ErrorCode(err) != CodeUnmanagedConflict {
		t.Fatalf("apply error code = %q, err=%v", ErrorCode(err), err)
	}
	if _, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true, ReplaceConflicts: true}); err != nil {
		t.Fatal(err)
	}
}

func TestDriftIsPreservedOnLastClaimUninstall(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	manager := testManager(t, home, "scaena")
	installBundle(t, manager, testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"shared-skill": "original"}), target)
	skillFile := filepath.Join(target.SkillsDir, "shared-skill", "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("user edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := manager.PlanUninstall("scaena")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Blocking {
		t.Fatalf("uninstall conflicts = %#v", plan.Conflicts)
	}
	result, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "partial" || !pathExists(skillFile) {
		t.Fatalf("result=%#v file_exists=%v", result, pathExists(skillFile))
	}
	registry, err := manager.ReadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Entries) != 1 || len(registry.Entries[0].Claims) != 0 {
		t.Fatalf("orphan registry = %#v", registry)
	}
}

func TestRollbackCreatesNewTransactionAndRestoresBytes(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	manager := testManager(t, home, "scaena")
	first := installBundle(t, manager, testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"shared-skill": "version one"}), target)
	second := installBundle(t, manager, testBundle(t, t.TempDir(), "scaena", "v2", map[string]string{"shared-skill": "version two"}), target)
	if first.TransactionID == second.TransactionID {
		t.Fatal("install transactions are not unique")
	}
	plan, err := manager.PlanRollback("scaena")
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.TransactionID == second.TransactionID {
		t.Fatal("rollback reused a prior transaction")
	}
	content, err := os.ReadFile(filepath.Join(target.SkillsDir, "shared-skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version one" {
		t.Fatalf("content = %q", content)
	}
}

func TestStalePlanIsRejected(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	first := testManager(t, home, "eikona")
	second := testManager(t, home, "scaena")
	stale, err := second.PlanInstall(testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"scaena-skill": "scaena"}), []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	installBundle(t, first, testBundle(t, t.TempDir(), "eikona", "v1", map[string]string{"eikona-skill": "eikona"}), target)
	if _, err := second.Apply(context.Background(), stale, ApplyOptions{Confirm: true}); ErrorCode(err) != CodePlanStale {
		t.Fatalf("stale error code = %q, err=%v", ErrorCode(err), err)
	}
}

func TestSerializedPlanWithoutConflictsCanBeApplied(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	manager := testManager(t, home, "scaena")
	plan, err := manager.PlanInstall(testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"scaena-skill": "scaena"}), []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(t.TempDir(), "install-plan.json")
	if err := WritePlan(planPath, plan); err != nil {
		t.Fatal(err)
	}
	readPlan, err := ReadPlan(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if readPlan.Conflicts == nil {
		t.Fatal("read plan conflicts must normalize to an empty slice")
	}
	if _, err := manager.Apply(context.Background(), readPlan, ApplyOptions{Confirm: true}); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyAdoption(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	dir := filepath.Join(target.SkillsDir, "legacy-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := inspectSkillDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := SkillDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, home, "eikona")
	plan, err := manager.PlanLegacyAdoption(ProductSnapshot{
		Identity: ProductIdentity{Product: "eikona", ProductVersion: "v1", BundleVersion: "v1", BundleDigest: strings.Repeat("a", 64)},
		Entries:  []SnapshotEntry{{Runtime: target.Runtime, SkillsDir: target.SkillsDir, Skill: "legacy-skill", Digest: digest, Files: files}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 || len(plan.Actions) != 1 || plan.Actions[0].Kind != ActionClaim {
		t.Fatalf("adoption plan = %#v %#v", plan.Actions, plan.Conflicts)
	}
	if _, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryLockContentionFailsClosed(t *testing.T) {
	home := t.TempDir()
	first := testManager(t, home, "scaena")
	paths, err := DefaultPaths(home, "eikona")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(ManagerOptions{Paths: paths, LockTimeout: 40 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := first.acquireLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	if _, err := second.acquireLock(context.Background()); ErrorCode(err) != CodeRegistryBusy {
		t.Fatalf("lock error code = %q, err=%v", ErrorCode(err), err)
	}
}

func TestApplyRecoversIncompleteRuntimeReplacement(t *testing.T) {
	home := t.TempDir()
	target := RuntimeTarget{Runtime: RuntimeAgents, SkillsDir: filepath.Join(home, ".agents", "skills")}
	manager := testManager(t, home, "scaena")
	versionOne := testBundle(t, t.TempDir(), "scaena", "v1", map[string]string{"shared-skill": "version one"})
	installBundle(t, manager, versionOne, target)
	plan, err := manager.PlanInstall(versionOne, []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}

	registry, err := manager.ReadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := manager.ReadReceipt()
	if err != nil || receipt == nil {
		t.Fatalf("receipt=%#v err=%v", receipt, err)
	}
	versionTwo := testBundle(t, t.TempDir(), "scaena", "v2", map[string]string{"shared-skill": "version two"})
	entry := versionTwo.Manifest.Skills[0]
	txRoot := filepath.Join(manager.paths.TransactionsRoot(), "crash-fixture")
	backup := filepath.Join(txRoot, "backups", "shared-skill")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(txRoot, "registry-before.json"), registry, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(txRoot, "receipt-before.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeSkill := filepath.Join(target.SkillsDir, "shared-skill")
	if err := moveDir(runtimeSkill, backup); err != nil {
		t.Fatal(err)
	}
	if err := copySkillDir(filepath.Join(versionTwo.Root, "skills", "shared-skill"), runtimeSkill, entry.Files); err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{
		SchemaVersion: TransactionSchema, TransactionID: "crash-fixture", Product: "scaena", Operation: OperationInstall,
		Status: transactionApplying, BaseRegistryRevision: registry.Revision, ReceiptExisted: true,
		Mutations: []fileMutation{{Target: runtimeSkill, Backup: backup, Existed: true, ResultDigest: entry.Digest}},
		StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := writeJSONAtomic(filepath.Join(txRoot, "journal.json"), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "unchanged" {
		t.Fatalf("result = %#v", result)
	}
	content, err := os.ReadFile(filepath.Join(runtimeSkill, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version one" {
		t.Fatalf("recovered content = %q", content)
	}
	var recovered transactionJournal
	if err := readJSONFile(filepath.Join(txRoot, "journal.json"), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Status != transactionRolledBack {
		t.Fatalf("journal status = %s", recovered.Status)
	}
}

func testManager(t *testing.T, home, product string) *Manager {
	t.Helper()
	paths, err := DefaultPaths(home, product)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ManagerOptions{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func installBundle(t *testing.T, manager *Manager, bundle Bundle, target RuntimeTarget) ApplyResult {
	t.Helper()
	plan, err := manager.PlanInstall(bundle, []RuntimeTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Apply(context.Background(), plan, ApplyOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testBundle(t *testing.T, root, product, version string, contents map[string]string) Bundle {
	t.Helper()
	manifest := BundleManifest{
		SchemaVersion: BundleSchema, Product: product, ProductVersion: version, BundleVersion: version,
		Source:         SourceRef{Repository: "https://github.com/yeisme/yeisme-agent-my-skills", Commit: testCommit},
		RuntimeTargets: []RuntimeID{RuntimeAgents, RuntimeCodex, RuntimeClaude},
	}
	catalog := Catalog{SchemaVersion: CatalogSchema, Product: product, ProductVersion: version, BundleVersion: version}
	for name, content := range contents {
		dir := filepath.Join(root, "skills", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		fileDigest, size, err := digestFile(file)
		if err != nil {
			t.Fatal(err)
		}
		files := []FileDigest{{Path: "SKILL.md", SHA256: fileDigest, Size: size}}
		skillDigest, err := SkillDigest(files)
		if err != nil {
			t.Fatal(err)
		}
		manifest.Skills = append(manifest.Skills, BundleSkill{
			Name: name, Role: SkillRoleEntry, Maturity: MaturityBeta, Digest: skillDigest,
			Source: SourceRef{Repository: "https://github.com/yeisme/skills", Commit: testCommit, Path: name}, Files: files,
		})
		catalog.Entries = append(catalog.Entries, CatalogEntry{
			Name: name, DisplayName: name, Description: "Test Skill " + name, Maturity: MaturityBeta, Role: SkillRoleEntry, Digest: skillDigest,
			Keywords: []string{name}, DefaultPrompt: "Use " + name,
		})
	}
	sortBundleFixture(&manifest, &catalog)
	bundle, err := NewBundle(root, manifest, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func sortBundleFixture(manifest *BundleManifest, catalog *Catalog) {
	for i := 0; i < len(manifest.Skills); i++ {
		for j := i + 1; j < len(manifest.Skills); j++ {
			if manifest.Skills[j].Name < manifest.Skills[i].Name {
				manifest.Skills[i], manifest.Skills[j] = manifest.Skills[j], manifest.Skills[i]
			}
		}
	}
	for i := 0; i < len(catalog.Entries); i++ {
		for j := i + 1; j < len(catalog.Entries); j++ {
			if catalog.Entries[j].Name < catalog.Entries[i].Name {
				catalog.Entries[i], catalog.Entries[j] = catalog.Entries[j], catalog.Entries[i]
			}
		}
	}
}
