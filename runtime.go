package skillsruntime

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DetectOptions struct {
	HomeDir   string
	CodexHome string
}

// DetectRuntimes always returns the generic Agents home and adds detected Codex/Claude homes.
func DetectRuntimes(options DetectOptions) ([]RuntimeTarget, error) {
	home := strings.TrimSpace(options.HomeDir)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, newError(CodeInvalidArgument, "resolve user home", "", err)
		}
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return nil, newError(CodeInvalidArgument, "resolve user home", home, err)
	}
	targets := []RuntimeTarget{{Runtime: RuntimeAgents, SkillsDir: filepath.Join(absHome, ".agents", "skills")}}
	codexHome := strings.TrimSpace(options.CodexHome)
	if codexHome == "" {
		codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if codexHome != "" {
		if !filepath.IsAbs(codexHome) {
			codexHome = filepath.Join(absHome, codexHome)
		}
		targets = append(targets, RuntimeTarget{Runtime: RuntimeCodex, SkillsDir: filepath.Join(filepath.Clean(codexHome), "skills")})
	} else if pathExists(filepath.Join(absHome, ".codex")) {
		targets = append(targets, RuntimeTarget{Runtime: RuntimeCodex, SkillsDir: filepath.Join(absHome, ".codex", "skills")})
	}
	if pathExists(filepath.Join(absHome, ".claude")) {
		targets = append(targets, RuntimeTarget{Runtime: RuntimeClaude, SkillsDir: filepath.Join(absHome, ".claude", "skills")})
	}
	allowedRoots := []string{absHome}
	if codexHome != "" {
		allowedRoots = append(allowedRoots, codexHome)
	}
	return normalizeRuntimeTargets(targets, allowedRoots)
}

// ResolveRuntimes resolves "auto" or a comma-separated runtime list.
func ResolveRuntimes(value string, options DetectOptions) ([]RuntimeTarget, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "auto" {
		return DetectRuntimes(options)
	}
	home := options.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, newError(CodeInvalidArgument, "resolve user home", "", err)
		}
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return nil, newError(CodeInvalidArgument, "resolve user home", home, err)
	}
	seen := map[RuntimeID]struct{}{}
	targets := make([]RuntimeTarget, 0, 3)
	for _, raw := range strings.Split(value, ",") {
		runtimeID := RuntimeID(strings.TrimSpace(raw))
		if _, duplicate := seen[runtimeID]; duplicate {
			continue
		}
		seen[runtimeID] = struct{}{}
		switch runtimeID {
		case RuntimeAgents:
			targets = append(targets, RuntimeTarget{Runtime: runtimeID, SkillsDir: filepath.Join(absHome, ".agents", "skills")})
		case RuntimeCodex:
			codexHome := strings.TrimSpace(options.CodexHome)
			if codexHome == "" {
				codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
			}
			if codexHome == "" {
				codexHome = filepath.Join(absHome, ".codex")
			} else if !filepath.IsAbs(codexHome) {
				codexHome = filepath.Join(absHome, codexHome)
			}
			targets = append(targets, RuntimeTarget{Runtime: runtimeID, SkillsDir: filepath.Join(filepath.Clean(codexHome), "skills")})
		case RuntimeClaude:
			targets = append(targets, RuntimeTarget{Runtime: runtimeID, SkillsDir: filepath.Join(absHome, ".claude", "skills")})
		default:
			return nil, newError(CodeInvalidArgument, "unknown runtime", string(runtimeID), nil)
		}
	}
	if len(targets) == 0 {
		return nil, newError(CodeInvalidArgument, "at least one runtime is required", value, nil)
	}
	allowedRoots := []string{absHome}
	if codexHome := strings.TrimSpace(options.CodexHome); codexHome != "" {
		if !filepath.IsAbs(codexHome) {
			codexHome = filepath.Join(absHome, codexHome)
		}
		allowedRoots = append(allowedRoots, codexHome)
	} else if codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		if !filepath.IsAbs(codexHome) {
			codexHome = filepath.Join(absHome, codexHome)
		}
		allowedRoots = append(allowedRoots, codexHome)
	}
	return normalizeRuntimeTargets(targets, allowedRoots)
}

func normalizeRuntimeTargets(targets []RuntimeTarget, allowedRoots []string) ([]RuntimeTarget, error) {
	roots, err := normalizeAllowedRoots(allowedRoots)
	if err != nil {
		return nil, err
	}
	result := make([]RuntimeTarget, 0, len(targets))
	seen := map[string]struct{}{}
	for _, target := range targets {
		if target.Runtime != RuntimeAgents && target.Runtime != RuntimeCodex && target.Runtime != RuntimeClaude {
			return nil, newError(CodeInvalidArgument, "unknown runtime", string(target.Runtime), nil)
		}
		abs, err := filepath.Abs(target.SkillsDir)
		if err != nil {
			return nil, newError(CodeInvalidArgument, "resolve runtime skills directory", target.SkillsDir, err)
		}
		resolved, err := resolvePath(abs)
		if err != nil {
			return nil, newError(CodeInvalidArgument, "resolve runtime skills directory", abs, err)
		}
		if !pathWithinAny(roots, resolved) {
			return nil, newError(CodeInvalidArgument, "runtime skills directory is outside the allowed user roots", resolved, nil)
		}
		key := string(target.Runtime) + "\x00" + resolved
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, RuntimeTarget{Runtime: target.Runtime, SkillsDir: resolved})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Runtime != result[j].Runtime {
			return result[i].Runtime < result[j].Runtime
		}
		return result[i].SkillsDir < result[j].SkillsDir
	})
	return result, nil
}

func normalizeAllowedRoots(roots []string) ([]string, error) {
	result := make([]string, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		resolved, err := resolvePath(root)
		if err != nil {
			return nil, newError(CodeInvalidArgument, "resolve allowed runtime root", root, err)
		}
		if _, exists := seen[resolved]; exists {
			continue
		}
		seen[resolved] = struct{}{}
		result = append(result, resolved)
	}
	if len(result) == 0 {
		return nil, newError(CodeInvalidArgument, "at least one allowed runtime root is required", "", nil)
	}
	sort.Strings(result)
	return result, nil
}

func resolvePath(value string) (string, error) {
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	prefix := abs
	suffix := make([]string, 0)
	for {
		if _, err := os.Lstat(prefix); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
		suffix = append([]string{filepath.Base(prefix)}, suffix...)
		prefix = parent
	}
	resolvedPrefix, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		return "", err
	}
	parts := append([]string{resolvedPrefix}, suffix...)
	return filepath.Clean(filepath.Join(parts...)), nil
}

func pathWithinAny(roots []string, candidate string) bool {
	for _, root := range roots {
		if pathWithin(root, candidate) {
			return true
		}
	}
	return false
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
