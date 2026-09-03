package skillsruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const digestPrefix = "sha256:"

func digestReader(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", n, err
	}
	return digestPrefix + hex.EncodeToString(h.Sum(nil)), n, nil
}

func digestFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	return digestReader(f)
}

func normalizeDigest(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, digestPrefix)
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 digest must contain %d hexadecimal characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid sha256 digest: %w", err)
	}
	return digestPrefix + value, nil
}

func digestHex(value string) (string, error) {
	normalized, err := normalizeDigest(value)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(normalized, digestPrefix), nil
}

// SkillDigest computes the canonical digest of a sorted file inventory.
func SkillDigest(files []FileDigest) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("skill file inventory is empty")
	}
	copyFiles := append([]FileDigest(nil), files...)
	sort.Slice(copyFiles, func(i, j int) bool { return copyFiles[i].Path < copyFiles[j].Path })
	h := sha256.New()
	for _, file := range copyFiles {
		digest, err := normalizeDigest(file.SHA256)
		if err != nil {
			return "", fmt.Errorf("%s: %w", file.Path, err)
		}
		_, _ = io.WriteString(h, file.Path)
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, digest)
		_, _ = h.Write([]byte{0})
	}
	return digestPrefix + hex.EncodeToString(h.Sum(nil)), nil
}
