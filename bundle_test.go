package skillsruntime

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBundleValidationAndForbiddenPaths(t *testing.T) {
	bundle := testBundle(t, t.TempDir(), "scaena", "v1.0.0", map[string]string{
		"scaena-storyboard-breakdown": "# Storyboard\n拆成可审阅分镜。\n",
	})
	if bundle.Digest == "" {
		t.Fatal("bundle digest is empty")
	}

	invalid := bundle.Manifest
	invalid.Skills[0].Files[0].Path = "../SKILL.md"
	if _, err := NewBundle(bundle.Root, invalid, bundle.Catalog); ErrorCode(err) != CodeBundlePathForbidden {
		t.Fatalf("traversal error code = %q, err=%v", ErrorCode(err), err)
	}
}

func TestBundleRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills", "linked-skill")
	if err := os.MkdirAll(filepath.Join(skillRoot, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("# Linked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(skillRoot, "assets", "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	skillFileDigest, size, err := digestFile(filepath.Join(skillRoot, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	linkDigest, linkSize, err := digestFile(link)
	if err != nil {
		t.Fatal(err)
	}
	files := []FileDigest{{Path: "SKILL.md", SHA256: skillFileDigest, Size: size}, {Path: "assets/link.txt", SHA256: linkDigest, Size: linkSize}}
	treeDigest, err := SkillDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	manifest := BundleManifest{
		SchemaVersion: BundleSchema, Product: "scaena", ProductVersion: "v1.0.0", BundleVersion: "v1.0.0",
		Source:         SourceRef{Repository: "https://github.com/yeisme/yeisme-agent-my-skills", Commit: testCommit},
		RuntimeTargets: []RuntimeID{RuntimeAgents},
		Skills: []BundleSkill{{
			Name: "linked-skill", Role: SkillRoleEntry, Maturity: MaturityBeta, Digest: treeDigest, Files: files,
			Source: SourceRef{Repository: "https://github.com/yeisme/skills", Commit: testCommit, Path: "linked-skill"},
		}},
	}
	catalog := Catalog{SchemaVersion: CatalogSchema, Product: "scaena", ProductVersion: "v1.0.0", BundleVersion: "v1.0.0", Entries: []CatalogEntry{{
		Name: "linked-skill", DisplayName: "Linked", Description: "Linked fixture", Maturity: MaturityBeta, Role: SkillRoleEntry, Digest: treeDigest,
	}}}
	if _, err := NewBundle(root, manifest, catalog); ErrorCode(err) != CodeBundleSymlink {
		t.Fatalf("symlink error code = %q, err=%v", ErrorCode(err), err)
	}
}

func TestCatalogSearchAndSuggestDeterministic(t *testing.T) {
	catalog := Catalog{Entries: []CatalogEntry{
		{Name: "scaena-production-operator", DisplayName: "Production Operator", Description: "端到端短剧生产", Role: SkillRoleEntry, Maturity: MaturityBeta, Keywords: []string{"生产"}, DefaultPrompt: "继续短剧生产"},
		{Name: "scaena-storyboard-breakdown", DisplayName: "Storyboard Breakdown", Description: "把剧本拆成可审阅分镜", Role: SkillRoleEntry, Maturity: MaturityBeta, Keywords: []string{"分镜", "storyboard"}, DefaultPrompt: "把剧本拆成可审阅分镜"},
		{Name: "ai-drama-director", DisplayName: "Director", Description: "导演依赖", Role: SkillRoleDependency, Maturity: MaturityStable, Keywords: []string{"导演"}},
	}}
	matches := catalog.Search("分镜", false)
	if len(matches) != 1 || matches[0].Entry.Name != "scaena-storyboard-breakdown" {
		t.Fatalf("matches = %#v", matches)
	}
	want := catalog.Suggest("把剧本拆成可审阅分镜", map[string]bool{"scaena-storyboard-breakdown": true}, 3)
	got := catalog.Suggest("把剧本拆成可审阅分镜", map[string]bool{"scaena-storyboard-breakdown": true}, 3)
	if !reflect.DeepEqual(got, want) || len(got) != 1 || got[0].SkillRef != "$scaena-storyboard-breakdown" {
		t.Fatalf("suggestions = %#v", got)
	}
	if entries := catalog.List(false); len(entries) != 2 {
		t.Fatalf("entry list length = %d", len(entries))
	}
	if entries := catalog.List(true); len(entries) != 3 {
		t.Fatalf("full list length = %d", len(entries))
	}
}

func TestDetectRuntimes(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{".codex", ".claude"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", "")
	targets, err := DetectRuntimes(DetectOptions{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 {
		t.Fatalf("targets = %#v", targets)
	}
	if targets[0].Runtime != RuntimeAgents || targets[1].Runtime != RuntimeClaude || targets[2].Runtime != RuntimeCodex {
		t.Fatalf("runtime order = %#v", targets)
	}
}

func TestDetectRuntimesAllowsExplicitExternalCodexHome(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	targets, err := DetectRuntimes(DetectOptions{HomeDir: home, CodexHome: codexHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[1].Runtime != RuntimeCodex || targets[1].SkillsDir != filepath.Join(codexHome, "skills") {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestDetectRuntimesRejectsRuntimeSymlinkOutsideAllowedRoots(t *testing.T) {
	home := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(home, ".agents")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := DetectRuntimes(DetectOptions{HomeDir: home}); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("error code = %q, err=%v", ErrorCode(err), err)
	}
}
