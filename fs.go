package skillsruntime

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func inspectSkillDir(root string) ([]FileDigest, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, newError(CodeUserDrift, "managed Skill path is not a regular directory", root, nil)
	}
	files := make([]FileDigest, 0)
	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return newError(CodeUserDrift, "managed Skill contains a symlink", filePath, nil)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return newError(CodeUserDrift, "managed Skill contains a non-regular file", filePath, nil)
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		digest, size, err := digestFile(filePath)
		if err != nil {
			return err
		}
		files = append(files, FileDigest{Path: filepath.ToSlash(rel), SHA256: digest, Size: size})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func verifySkillDir(root string, expected []FileDigest) (bool, error) {
	actual, err := inspectSkillDir(root)
	if err != nil {
		return false, err
	}
	if len(actual) != len(expected) {
		return false, nil
	}
	expectedCopy := append([]FileDigest(nil), expected...)
	sort.Slice(expectedCopy, func(i, j int) bool { return expectedCopy[i].Path < expectedCopy[j].Path })
	for i := range actual {
		digest, err := normalizeDigest(expectedCopy[i].SHA256)
		if err != nil || actual[i].Path != expectedCopy[i].Path || actual[i].SHA256 != digest {
			return false, err
		}
	}
	return true, nil
}

func copySkillDir(source, target string, expected []FileDigest) error {
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, file := range expected {
		rel, err := validateBundleRelativePath(file.Path)
		if err != nil {
			return err
		}
		sourcePath := filepath.Join(source, filepath.FromSlash(rel))
		targetPath := filepath.Join(target, filepath.FromSlash(rel))
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return newError(CodeBundleSymlink, "source Skill file is not regular", sourcePath, nil)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return err
		}
		if err := copyFile(sourcePath, targetPath, 0o600); err != nil {
			return err
		}
	}
	ok, err := verifySkillDir(target, expected)
	if err != nil {
		return err
	}
	if !ok {
		return newError(CodeDigestMismatch, "copied Skill does not match expected inventory", target, nil)
	}
	return nil
}

func copyFile(source, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func moveDir(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := copyTree(source, target); err != nil {
		return err
	}
	return os.RemoveAll(source)
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, filePath)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return newError(CodeUserDrift, "symlink cannot be copied into transaction evidence", filePath, nil)
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		return copyFile(filePath, destination, 0o600)
	})
}
