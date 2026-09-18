package skillsruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// HTTPDoer abstracts the HTTP client so consumers can inject transport stubs.
type HTTPDoer interface {
	Do(request *http.Request) (*http.Response, error)
}

const (
	// DefaultDistAssetBase is the canonical public distribution release asset base.
	DefaultDistAssetBase = "https://github.com/yeisme/yeisme-dist/releases/download"
	// DistSizeLimits bound release metadata, bundles, and archive extraction.
	DistMaxBundleBytes    = 128 << 20
	DistMaxMetadataBytes  = 4 << 20
	DistMaxExtractBytes   = 256 << 20
	DistMaxExtractEntries = 8192
)

var distVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
var distProductPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// DistOptions selects a product release on the public distribution mirror.
type DistOptions struct {
	// Product is the lowercase distribution product name, e.g. "sonora".
	Product string
	// Version is the bare semantic version without a leading "v".
	Version string
	// AssetBase overrides DefaultDistAssetBase for tests; it must be HTTPS or loopback HTTP.
	AssetBase string
	// HTTPClient overrides the default 45s HTTP client.
	HTTPClient HTTPDoer
}

// DistRelease is a downloaded, checksum-verified, locally extracted release bundle.
type DistRelease struct {
	Tag    string
	Bundle Bundle
}

// ResolveDistRelease downloads the exact `<product>/v<version>` release assets
// from the distribution mirror, verifies every byte against checksums.txt and
// the install manifest, extracts the bundle into a private staging directory,
// and returns the validated Bundle. The returned cleanup function removes the
// staging directory and must be called by the consumer.
func ResolveDistRelease(ctx context.Context, options DistOptions) (DistRelease, func(), error) {
	product := strings.TrimSpace(options.Product)
	if !distProductPattern.MatchString(product) {
		return DistRelease{}, func() {}, newError(CodeInvalidArgument, "dist product must be a lowercase distribution product name", product, nil)
	}
	version := strings.TrimSpace(options.Version)
	if !distVersionPattern.MatchString(version) {
		return DistRelease{}, func() {}, newError(CodeInvalidArgument, "dist version must be a bare X.Y.Z semantic version", options.Version, nil)
	}
	tag := product + "/v" + version
	base := strings.TrimSuffix(strings.TrimSpace(options.AssetBase), "/")
	if base == "" {
		base = DefaultDistAssetBase
	}
	if err := validateDistAssetBase(base); err != nil {
		return DistRelease{}, func() {}, err
	}
	doer := options.HTTPClient
	if doer == nil {
		doer = &http.Client{Timeout: 45 * time.Second}
	}
	checksums, err := distDownload(ctx, doer, distAssetURL(base, tag, "checksums.txt"), DistMaxMetadataBytes)
	if err != nil {
		return DistRelease{}, func() {}, newDistReleaseError(err)
	}
	installManifestBytes, err := distDownloadChecked(ctx, doer, base, tag, product+"-install-manifest.json", checksums, DistMaxMetadataBytes)
	if err != nil {
		return DistRelease{}, func() {}, newDistReleaseError(err)
	}
	var installManifest ProductInstallManifest
	if err := json.Unmarshal(installManifestBytes, &installManifest); err != nil {
		return DistRelease{}, func() {}, newDistReleaseError(fmt.Errorf("decode install manifest: %w", err))
	}
	if err := ValidateInstallManifest(installManifest); err != nil {
		return DistRelease{}, func() {}, err
	}
	if installManifest.Product != product || installManifest.ProductVersion != version || installManifest.Tag != tag || installManifest.Skills == nil {
		return DistRelease{}, func() {}, newDistReleaseError(fmt.Errorf("install manifest identity does not match %s %s", product, version))
	}
	assets := installManifest.Skills
	bundleBytes, err := distDownloadDeclaredAsset(ctx, doer, base, tag, assets.Bundle, checksums, DistMaxBundleBytes)
	if err != nil {
		return DistRelease{}, func() {}, newDistReleaseError(err)
	}
	bundleManifestBytes, err := distDownloadDeclaredAsset(ctx, doer, base, tag, assets.Manifest, checksums, DistMaxMetadataBytes)
	if err != nil {
		return DistRelease{}, func() {}, newDistReleaseError(err)
	}
	catalogBytes, err := distDownloadDeclaredAsset(ctx, doer, base, tag, assets.Catalog, checksums, DistMaxMetadataBytes)
	if err != nil {
		return DistRelease{}, func() {}, newDistReleaseError(err)
	}
	stage, err := os.MkdirTemp("", product+"-agent-skills-")
	if err != nil {
		return DistRelease{}, func() {}, newError(CodeIO, "create dist staging directory", "", err)
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	if err := distExtractBundle(bundleBytes, stage); err != nil {
		cleanup()
		return DistRelease{}, func() {}, err
	}
	manifestPath := filepath.Join(stage, "bundle.json")
	catalogPath := filepath.Join(stage, "catalog.json")
	if err := os.WriteFile(manifestPath, bundleManifestBytes, 0o600); err != nil {
		cleanup()
		return DistRelease{}, func() {}, newError(CodeIO, "write staged bundle manifest", manifestPath, err)
	}
	if err := os.WriteFile(catalogPath, catalogBytes, 0o600); err != nil {
		cleanup()
		return DistRelease{}, func() {}, newError(CodeIO, "write staged catalog", catalogPath, err)
	}
	bundle, err := LoadBundle(stage, manifestPath, catalogPath)
	if err != nil {
		cleanup()
		return DistRelease{}, func() {}, err
	}
	if bundle.Manifest.Product != product || bundle.Manifest.ProductVersion != version {
		cleanup()
		return DistRelease{}, func() {}, newDistReleaseError(fmt.Errorf("bundle identity does not match %s %s", product, version))
	}
	return DistRelease{Tag: tag, Bundle: bundle}, cleanup, nil
}

func distDownloadDeclaredAsset(ctx context.Context, doer HTTPDoer, base, tag string, asset ReleaseAsset, checksums []byte, limit int64) ([]byte, error) {
	if asset.Name == "" || filepath.Base(asset.Name) != asset.Name {
		return nil, newError(CodeBundleInvalid, "release asset name is not a basename", asset.Name, nil)
	}
	expectedURL := distAssetURL(base, tag, asset.Name)
	if base == DefaultDistAssetBase && asset.URL != expectedURL {
		return nil, newError(CodeBundleInvalid, "release asset URL is not the public mirror URL", asset.Name, nil)
	}
	payload, err := distDownloadChecked(ctx, doer, base, tag, asset.Name, checksums, limit)
	if err != nil {
		return nil, err
	}
	if err := distVerifyDigest(payload, asset.SHA256); err != nil {
		return nil, err
	}
	return payload, nil
}

func distDownloadChecked(ctx context.Context, doer HTTPDoer, base, tag, name string, checksums []byte, limit int64) ([]byte, error) {
	payload, err := distDownload(ctx, doer, distAssetURL(base, tag, name), limit)
	if err != nil {
		return nil, err
	}
	want, err := distChecksumFor(checksums, name)
	if err != nil {
		return nil, err
	}
	if err := distVerifyDigest(payload, want); err != nil {
		return nil, err
	}
	return payload, nil
}

func distDownload(ctx context.Context, doer HTTPDoer, rawURL string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := doer.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asset download returned status %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("asset exceeds the %d byte limit", limit)
	}
	return payload, nil
}

func distChecksumFor(checksums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

func distVerifyDigest(payload []byte, expected string) error {
	expected = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(expected)), "sha256:")
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != expected {
		return newError(CodeDigestMismatch, "release asset failed SHA-256 verification", "", nil)
	}
	return nil
}

func distExtractBundle(payload []byte, destination string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return newError(CodeBundleInvalid, "skills bundle is not valid gzip", "", err)
	}
	defer func() { _ = gzipReader.Close() }()
	reader := tar.NewReader(gzipReader)
	var extractedBytes int64
	extractedEntries := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return newError(CodeBundleInvalid, "read skills archive", "", err)
		}
		extractedEntries++
		if extractedEntries > DistMaxExtractEntries {
			return fmt.Errorf("skills archive contains more than %d entries", DistMaxExtractEntries)
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if header.Name == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return newError(CodeBundlePathForbidden, "skills archive contains path traversal", header.Name, nil)
		}
		target := filepath.Join(destination, clean)
		if !pathInside(destination, target) || (clean != "skills" && !strings.HasPrefix(filepath.ToSlash(clean), "skills/")) {
			return newError(CodeBundlePathForbidden, "skills archive entry is outside the skills root", header.Name, nil)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return newError(CodeIO, "create staged archive directory", target, err)
			}
		case tar.TypeReg:
			if header.Size < 0 || header.Size > DistMaxMetadataBytes {
				return fmt.Errorf("skills archive member exceeds size limit: %s", header.Name)
			}
			if extractedBytes > DistMaxExtractBytes-header.Size {
				return fmt.Errorf("skills archive exceeds the %d byte extraction limit", DistMaxExtractBytes)
			}
			extractedBytes += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return newError(CodeIO, "create staged archive parent", target, err)
			}
			data, err := io.ReadAll(io.LimitReader(reader, DistMaxMetadataBytes+1))
			if err != nil || int64(len(data)) != header.Size {
				return newError(CodeBundleInvalid, "read skills archive member", header.Name, err)
			}
			if err := os.WriteFile(target, data, 0o600); err != nil {
				return newError(CodeIO, "write staged archive member", target, err)
			}
		default:
			return newError(CodeBundleSymlink, "skills archive contains a link or unsupported entry", header.Name, nil)
		}
	}
	return nil
}

func validateDistAssetBase(base string) error {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return newError(CodeSourceUntrusted, "agent skills asset base is invalid", base, nil)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost") {
		return nil
	}
	return newError(CodeSourceUntrusted, "agent skills asset base must use HTTPS or loopback HTTP", base, nil)
}

func distAssetURL(base, tag, name string) string {
	return strings.TrimSuffix(base, "/") + "/" + tag + "/" + name
}

func newDistReleaseError(err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return err
	}
	return newError(CodeReleaseUnavailable, err.Error(), "", nil)
}

// pathInside reports whether candidate resolves under root without traversal.
func pathInside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
