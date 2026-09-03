package skillsruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Paths struct {
	HomeDir         string
	StateRoot       string
	ProductStateDir string
}

// DefaultPaths resolves the stable user-global registry and per-product receipt roots.
func DefaultPaths(homeDir, product string) (Paths, error) {
	if !skillNamePattern.MatchString(product) {
		return Paths{}, newError(CodeInvalidArgument, "invalid product name", product, nil)
	}
	if strings.TrimSpace(homeDir) == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return Paths{}, newError(CodeInvalidArgument, "resolve user home", "", err)
		}
	}
	home, err := filepath.Abs(homeDir)
	if err != nil {
		return Paths{}, newError(CodeInvalidArgument, "resolve user home", homeDir, err)
	}
	return Paths{
		HomeDir:         filepath.Clean(home),
		StateRoot:       filepath.Join(home, ".yeisme", "agent-skills"),
		ProductStateDir: filepath.Join(home, "."+product),
	}, nil
}

func (p Paths) RegistryPath() string     { return filepath.Join(p.StateRoot, "registry.json") }
func (p Paths) LockPath() string         { return filepath.Join(p.StateRoot, "registry.lock") }
func (p Paths) StoreRoot() string        { return filepath.Join(p.StateRoot, "store", "sha256") }
func (p Paths) TransactionsRoot() string { return filepath.Join(p.StateRoot, "transactions") }
func (p Paths) ReceiptPath() string {
	return filepath.Join(p.ProductStateDir, "agent-skills.lock.json")
}

type ManagerOptions struct {
	Paths               Paths
	AllowedRuntimeRoots []string
	LockTimeout         time.Duration
	StaleLockAfter      time.Duration
	Now                 func() time.Time
	NewID               func() string
}

type Manager struct {
	paths          Paths
	runtimeRoots   []string
	lockTimeout    time.Duration
	staleLockAfter time.Duration
	now            func() time.Time
	newID          func() string
}

func NewManager(options ManagerOptions) (*Manager, error) {
	paths := options.Paths
	if paths.HomeDir == "" || paths.StateRoot == "" || paths.ProductStateDir == "" {
		return nil, newError(CodeInvalidArgument, "home, state, and product paths are required", "", nil)
	}
	resolvedHome, err := resolvePath(paths.HomeDir)
	if err != nil {
		return nil, newError(CodeInvalidArgument, "resolve user home", paths.HomeDir, err)
	}
	paths.HomeDir = resolvedHome
	for _, candidate := range []*string{&paths.StateRoot, &paths.ProductStateDir} {
		resolved, resolveErr := resolvePath(*candidate)
		if resolveErr != nil || !pathWithin(paths.HomeDir, resolved) {
			return nil, newError(CodeInvalidArgument, "state paths must be below the user home", *candidate, resolveErr)
		}
		*candidate = resolved
	}
	runtimeRoots := append([]string{paths.HomeDir}, options.AllowedRuntimeRoots...)
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		if !filepath.IsAbs(codexHome) {
			codexHome = filepath.Join(paths.HomeDir, codexHome)
		}
		runtimeRoots = append(runtimeRoots, codexHome)
	}
	runtimeRoots, err = normalizeAllowedRoots(runtimeRoots)
	if err != nil {
		return nil, err
	}
	if options.LockTimeout <= 0 {
		options.LockTimeout = 5 * time.Second
	}
	if options.StaleLockAfter <= 0 {
		options.StaleLockAfter = 30 * time.Minute
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.NewID == nil {
		options.NewID = randomID
	}
	return &Manager{
		paths:          paths,
		runtimeRoots:   runtimeRoots,
		lockTimeout:    options.LockTimeout,
		staleLockAfter: options.StaleLockAfter,
		now:            options.Now,
		newID:          options.NewID,
	}, nil
}

func (m *Manager) Paths() Paths { return m.paths }

func randomID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}

func loadRegistry(path string) (Registry, error) {
	var registry Registry
	err := readJSONFile(path, &registry)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{SchemaVersion: RegistrySchema, Entries: []RegistryEntry{}}, nil
	}
	if err != nil {
		return Registry{}, newError(CodeIO, "read shared registry", path, err)
	}
	if registry.SchemaVersion != RegistrySchema {
		return Registry{}, newError(CodeSchemaUnsupported, "unsupported registry schema", path, nil)
	}
	if registry.Entries == nil {
		registry.Entries = []RegistryEntry{}
	}
	if err := validateRegistry(registry); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

func validateRegistry(registry Registry) error {
	seen := map[string]struct{}{}
	for _, entry := range registry.Entries {
		if !skillNamePattern.MatchString(entry.Skill) {
			return newError(CodeBundleInvalid, "registry contains invalid skill name", entry.Skill, nil)
		}
		if entry.Runtime != RuntimeAgents && entry.Runtime != RuntimeCodex && entry.Runtime != RuntimeClaude {
			return newError(CodeBundleInvalid, "registry contains invalid runtime", string(entry.Runtime), nil)
		}
		if _, err := normalizeDigest(entry.Digest); err != nil {
			return newError(CodeBundleInvalid, "registry contains invalid digest", entry.Skill, err)
		}
		key := registryKey(entry.Runtime, entry.SkillsDir, entry.Skill)
		if _, duplicate := seen[key]; duplicate {
			return newError(CodeBundleInvalid, "registry contains duplicate runtime skill entry", entry.Skill, nil)
		}
		seen[key] = struct{}{}
		claims := map[string]struct{}{}
		for _, claim := range entry.Claims {
			if !skillNamePattern.MatchString(claim.Product) {
				return newError(CodeBundleInvalid, "registry contains invalid product claim", claim.Product, nil)
			}
			if _, duplicate := claims[claim.Product]; duplicate {
				return newError(CodeBundleInvalid, "registry contains duplicate product claim", claim.Product, nil)
			}
			claims[claim.Product] = struct{}{}
		}
	}
	return nil
}

func loadReceipt(path string) (*ProductReceipt, error) {
	var receipt ProductReceipt
	err := readJSONFile(path, &receipt)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, newError(CodeIO, "read product receipt", path, err)
	}
	if receipt.SchemaVersion != ProductReceiptSchema {
		return nil, newError(CodeSchemaUnsupported, "unsupported product receipt schema", path, nil)
	}
	return &receipt, nil
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	backup := target + ".backup-" + randomID()
	targetExists := pathExists(target)
	if targetExists {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(source, target); err != nil {
		if targetExists {
			_ = os.Rename(backup, target)
		}
		return err
	}
	if targetExists {
		_ = os.Remove(backup)
	}
	return nil
}

func syncDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func registryKey(runtimeID RuntimeID, skillsDir, skill string) string {
	return string(runtimeID) + "\x00" + filepath.Clean(skillsDir) + "\x00" + skill
}

func sortRegistry(registry *Registry) {
	sort.Slice(registry.Entries, func(i, j int) bool {
		left := registryKey(registry.Entries[i].Runtime, registry.Entries[i].SkillsDir, registry.Entries[i].Skill)
		right := registryKey(registry.Entries[j].Runtime, registry.Entries[j].SkillsDir, registry.Entries[j].Skill)
		return left < right
	})
	for i := range registry.Entries {
		sort.Slice(registry.Entries[i].Claims, func(a, b int) bool {
			return registry.Entries[i].Claims[a].Product < registry.Entries[i].Claims[b].Product
		})
	}
}

type lockOwner struct {
	SchemaVersion string    `json:"schema_version"`
	Token         string    `json:"token"`
	PID           int       `json:"pid"`
	CreatedAt     time.Time `json:"created_at"`
}

type registryLock struct {
	path  string
	token string
}

func (m *Manager) acquireLock(ctx context.Context) (*registryLock, error) {
	if err := os.MkdirAll(m.paths.StateRoot, 0o700); err != nil {
		return nil, newError(CodeIO, "create shared state root", m.paths.StateRoot, err)
	}
	deadline := time.Now().Add(m.lockTimeout)
	for {
		token := m.newID()
		err := os.Mkdir(m.paths.LockPath(), 0o700)
		if err == nil {
			owner := lockOwner{SchemaVersion: "yeisme.agent_skills.lock.v1", Token: token, PID: os.Getpid(), CreatedAt: m.now()}
			ownerPath := filepath.Join(m.paths.LockPath(), "owner.json")
			if err := writeJSONAtomic(ownerPath, owner, 0o600); err != nil {
				_ = os.RemoveAll(m.paths.LockPath())
				return nil, newError(CodeIO, "write registry lock owner", ownerPath, err)
			}
			return &registryLock{path: m.paths.LockPath(), token: token}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, newError(CodeIO, "acquire registry lock", m.paths.LockPath(), err)
		}
		m.reapStaleLock()
		if time.Now().After(deadline) {
			return nil, &Error{Code: CodeRegistryBusy, Message: "shared Agent Skills registry is busy", Path: m.paths.LockPath(), Retryable: true}
		}
		select {
		case <-ctx.Done():
			return nil, &Error{Code: CodeRegistryBusy, Message: "registry lock wait canceled", Path: m.paths.LockPath(), Retryable: true, Err: ctx.Err()}
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (m *Manager) reapStaleLock() {
	var owner lockOwner
	if err := readJSONFile(filepath.Join(m.paths.LockPath(), "owner.json"), &owner); err != nil {
		return
	}
	if m.now().Sub(owner.CreatedAt) < m.staleLockAfter {
		return
	}
	stalePath := m.paths.LockPath() + ".stale-" + m.newID()
	_ = os.Rename(m.paths.LockPath(), stalePath)
}

func (l *registryLock) assertOwned() error {
	var owner lockOwner
	ownerPath := filepath.Join(l.path, "owner.json")
	if err := readJSONFile(ownerPath, &owner); err != nil {
		return newError(CodeRegistryBusy, "registry lock ownership was lost", ownerPath, err)
	}
	if owner.Token != l.token {
		return newError(CodeRegistryBusy, "registry lock ownership changed", ownerPath, nil)
	}
	return nil
}

func (l *registryLock) release() {
	if l == nil || l.assertOwned() != nil {
		return
	}
	_ = os.RemoveAll(l.path)
}
