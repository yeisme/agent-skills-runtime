package skillsruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const distFixtureCommit = "1111111111111111111111111111111111111111"

func writeDistFixtureBundle(t *testing.T, root, product, version string) Bundle {
	t.Helper()
	skillRoot := filepath.Join(root, "skills", product+"-agent-router")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "# " + product + " agent router\nroute to the smallest safe command.\n"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	files, digest, err := InventorySkillDirectory(skillRoot)
	if err != nil {
		t.Fatal(err)
	}
	manifest := BundleManifest{
		SchemaVersion: BundleSchema, Product: product, ProductVersion: version, BundleVersion: version,
		Source:         SourceRef{Repository: "https://github.com/yeisme/yeisme-agent-my-skills", Commit: distFixtureCommit},
		RuntimeTargets: []RuntimeID{RuntimeAgents, RuntimeCodex, RuntimeClaude},
		Skills: []BundleSkill{{
			Name: product + "-agent-router", Role: SkillRoleEntry, Maturity: MaturityStable, Digest: digest,
			Source: SourceRef{Repository: "https://github.com/yeisme/yeisme-agent-my-skills", Commit: distFixtureCommit, Path: product + "/" + product + "-agent-router"}, Files: files,
		}},
	}
	catalog := Catalog{
		SchemaVersion: CatalogSchema, Product: product, ProductVersion: version, BundleVersion: version,
		Entries: []CatalogEntry{{
			Name: product + "-agent-router", DisplayName: product + " agent router",
			Description: "route " + product + " workflows", Maturity: MaturityStable, Role: SkillRoleEntry, Digest: digest,
		}},
	}
	if err := WriteBundleManifest(filepath.Join(root, "bundle.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := WriteCatalog(filepath.Join(root, "catalog.json"), catalog); err != nil {
		t.Fatal(err)
	}
	bundle, err := LoadBundle(root, filepath.Join(root, "bundle.json"), filepath.Join(root, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func distBundleArchive(t *testing.T, root string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	err := filepath.Walk(filepath.Join(root, "skills"), func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: filepath.ToSlash(relative), Mode: 0o600, Size: int64(len(content))}); err != nil {
			return err
		}
		_, err = tarWriter.Write(content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func sha256Hex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type distFixtureServer struct {
	server   *httptest.Server
	base     string
	tag      string
	versions map[string][]byte
}

// newDistFixtureServer serves checksums.txt, install manifest, bundle, manifest and catalog
// assets for product at the single version, mirroring the public dist layout.
func newDistFixtureServer(t *testing.T, product, version string, mutate func(manifest *ProductInstallManifest, checksumLines *[]string)) *distFixtureServer {
	t.Helper()
	root := t.TempDir()
	writeDistFixtureBundle(t, root, product, version)
	archive := distBundleArchive(t, root)
	manifestBytes, err := os.ReadFile(filepath.Join(root, "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalogBytes, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	tag := product + "/v" + version
	distBase := "https://github.com/yeisme/yeisme-dist/releases/download"
	asset := func(name string, payload []byte) ReleaseAsset {
		return ReleaseAsset{Name: name, Kind: "application/octet-stream", URL: distBase + "/" + tag + "/" + name, SHA256: sha256Hex(payload), Size: int64(len(payload))}
	}
	installManifest := ProductInstallManifest{
		SchemaVersion: InstallManifestSchema, Product: product, ProductVersion: version, Tag: tag, Commit: distFixtureCommit,
		Skills: &SkillsRelease{
			BundleVersion: version, Source: SourceRef{Repository: "https://github.com/yeisme/yeisme-agent-my-skills", Commit: distFixtureCommit},
			RuntimeTargets: []RuntimeID{RuntimeAgents, RuntimeCodex, RuntimeClaude},
			Bundle:         asset(product+"-skills_"+version+".tar.gz", archive),
			Manifest:       asset(product+"-skills_"+version+".bundle.json", manifestBytes),
			Catalog:        asset(product+"-skills_"+version+".catalog.json", catalogBytes),
		},
	}
	files := map[string][]byte{
		installManifest.Skills.Bundle.Name:   archive,
		installManifest.Skills.Manifest.Name: manifestBytes,
		installManifest.Skills.Catalog.Name:  catalogBytes,
	}
	var checksumLines []string
	for name, payload := range files {
		checksumLines = append(checksumLines, sha256Hex(payload)+"  "+name)
	}
	if mutate != nil {
		mutate(&installManifest, &checksumLines)
	}
	encodedManifest, err := json.Marshal(installManifest)
	if err != nil {
		t.Fatal(err)
	}
	files[product+"-install-manifest.json"] = encodedManifest
	checksumLines = append(checksumLines, sha256Hex(encodedManifest)+"  "+product+"-install-manifest.json")
	files["checksums.txt"] = []byte(strings.Join(checksumLines, "\n"))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(request.URL.Path, "/"+tag+"/")
		payload, ok := files[name]
		if !ok {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = response.Write(payload)
	}))
	t.Cleanup(server.Close)
	return &distFixtureServer{server: server, base: server.URL, tag: tag, versions: files}
}

func TestResolveDistReleaseVerifiesAndLoadsBundle(t *testing.T) {
	fixture := newDistFixtureServer(t, "sonora", "0.3.0", nil)
	release, cleanup, err := ResolveDistRelease(context.Background(), DistOptions{Product: "sonora", Version: "0.3.0", AssetBase: fixture.base})
	if err != nil {
		t.Fatalf("ResolveDistRelease: %v", err)
	}
	defer cleanup()
	if release.Tag != fixture.tag {
		t.Fatalf("release tag = %q, want %q", release.Tag, fixture.tag)
	}
	if release.Bundle.Manifest.Product != "sonora" || release.Bundle.Manifest.ProductVersion != "0.3.0" {
		t.Fatalf("bundle identity = %s %s", release.Bundle.Manifest.Product, release.Bundle.Manifest.ProductVersion)
	}
	skillPath := filepath.Join(release.Bundle.Root, "skills", "sonora-agent-router", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("staged skill missing: %v", err)
	}
	cleanup()
	if _, err := os.Stat(release.Bundle.Root); !os.IsNotExist(err) {
		t.Fatalf("staging directory survived cleanup: %v", err)
	}
}

func TestResolveDistReleaseRejectsTamperedBundle(t *testing.T) {
	fixture := newDistFixtureServer(t, "sonora", "0.3.0", func(manifest *ProductInstallManifest, checksumLines *[]string) {
		manifest.Skills.Bundle.SHA256 = strings.Repeat("0", 64)
	})
	_, cleanup, err := ResolveDistRelease(context.Background(), DistOptions{Product: "sonora", Version: "0.3.0", AssetBase: fixture.base})
	if cleanup != nil {
		defer cleanup()
	}
	if ErrorCode(err) != CodeDigestMismatch {
		t.Fatalf("error = %v, want code %s", err, CodeDigestMismatch)
	}
}

func TestResolveDistReleaseRejectsIdentityMismatch(t *testing.T) {
	fixture := newDistFixtureServer(t, "sonora", "0.3.0", nil)
	_, cleanup, err := ResolveDistRelease(context.Background(), DistOptions{Product: "sonora", Version: "0.9.9", AssetBase: fixture.base})
	if cleanup != nil {
		defer cleanup()
	}
	if ErrorCode(err) != CodeReleaseUnavailable {
		t.Fatalf("error = %v, want code %s", err, CodeReleaseUnavailable)
	}
}

func TestResolveDistReleaseRejectsUntrustedAssetBase(t *testing.T) {
	_, cleanup, err := ResolveDistRelease(context.Background(), DistOptions{Product: "sonora", Version: "0.3.0", AssetBase: "http://10.0.0.5:8080/releases"})
	if cleanup != nil {
		defer cleanup()
	}
	if ErrorCode(err) != CodeSourceUntrusted {
		t.Fatalf("error = %v, want code %s", err, CodeSourceUntrusted)
	}
}

func TestResolveDistReleaseRejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		options DistOptions
		code    string
	}{
		{DistOptions{Product: "Sonora", Version: "0.3.0"}, CodeInvalidArgument},
		{DistOptions{Product: "sonora", Version: "latest"}, CodeInvalidArgument},
		{DistOptions{Product: "sonora", Version: "v0.3.0"}, CodeInvalidArgument},
	}
	for _, testCase := range cases {
		_, cleanup, err := ResolveDistRelease(context.Background(), testCase.options)
		if cleanup != nil {
			defer cleanup()
		}
		if ErrorCode(err) != testCase.code {
			t.Fatalf("options %+v error = %v, want code %s", testCase.options, err, testCase.code)
		}
	}
}

func TestResolveDistReleaseRejectsTraversalArchive(t *testing.T) {
	root := t.TempDir()
	writeDistFixtureBundle(t, root, "sonora", "0.3.0")
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	evil := []byte("pwned")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o600, Size: int64(len(evil))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(evil); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buffer.Bytes()
	fixture := newDistFixtureServer(t, "sonora", "0.3.0", func(manifest *ProductInstallManifest, checksumLines *[]string) {
		manifest.Skills.Bundle.SHA256 = sha256Hex(archive)
		manifest.Skills.Bundle.Size = int64(len(archive))
		replaced := false
		for index, line := range *checksumLines {
			if strings.HasSuffix(line, manifest.Skills.Bundle.Name) {
				(*checksumLines)[index] = sha256Hex(archive) + "  " + manifest.Skills.Bundle.Name
				replaced = true
			}
		}
		if !replaced {
			(*checksumLines) = append(*checksumLines, sha256Hex(archive)+"  "+manifest.Skills.Bundle.Name)
		}
	})
	// checksums.txt and the install manifest are encoded from the mutated values,
	// but the served bundle bytes come from this shared map; swap them for the
	// traversal archive so every digest still agrees.
	for name := range fixture.versions {
		if strings.HasSuffix(name, ".tar.gz") {
			fixture.versions[name] = archive
		}
	}
	_, cleanup, err := ResolveDistRelease(context.Background(), DistOptions{Product: "sonora", Version: "0.3.0", AssetBase: fixture.base})
	if cleanup != nil {
		defer cleanup()
	}
	if ErrorCode(err) != CodeBundlePathForbidden {
		t.Fatalf("error = %v, want code %s", err, CodeBundlePathForbidden)
	}
}

func TestDistAssetURLJoinsExactly(t *testing.T) {
	got := distAssetURL("https://example.com/base/", "sonora/v0.3.0", "checksums.txt")
	want := "https://example.com/base/sonora/v0.3.0/checksums.txt"
	if got != want {
		t.Fatalf("distAssetURL = %q, want %q", got, want)
	}
}
