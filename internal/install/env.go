package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvStore reads and writes PATH-like variables plus PSModulePath.
// The OS implementation uses the registry on Windows; tests inject
// the memory implementation.
type EnvStore interface {
	Get(name string, global bool) string
	Set(name, value string, global bool) error
}

// memoryStore backs tests with process env semantics.
type memoryStore struct {
	user   map[string]string
	system map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{user: map[string]string{}, system: map[string]string{}}
}

func (m *memoryStore) Get(name string, global bool) string {
	if global {
		return m.system[name]
	}
	return m.user[name]
}

func (m *memoryStore) Set(name, value string, global bool) error {
	if global {
		if value == "" {
			delete(m.system, name)
		} else {
			m.system[name] = value
		}
		return nil
	}
	if value == "" {
		delete(m.user, name)
	} else {
		m.user[name] = value
	}
	return nil
}

// DefaultStore selects the OS registry store on Windows and the
// process environment elsewhere.
func DefaultStore() EnvStore {
	return defaultStoreOS()
}

// GetPath reads PATH for scope through the OS store.
func GetPath(global bool) string {
	return DefaultStore().Get("PATH", global)
}

// SetPath writes PATH for scope through the OS store.
func SetPath(value string, global bool) {
	_ = DefaultStore().Set("PATH", value, global)
}

// SplitPathLike mirrors Split-PathLikeEnvVar (lib/system.ps1:76-94):
// entries matching any pattern split into inPath, the rest remain.
func SplitPathLike(patterns []string, path string) (inPath, rest string) {
	if path == "" {
		return "", ""
	}
	parts := strings.Split(path, ";")
	var in, out []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var matched, kept []string
		for _, entry := range parts {
			if entry == "" {
				continue
			}
			if like(entry, p) {
				matched = append(matched, entry)
			} else {
				kept = append(kept, entry)
			}
		}
		in = append(in, matched...)
		parts = kept
		_ = out
	}
	return strings.Join(in, ";"), strings.Join(parts, ";")
}

func like(s, pattern string) bool {
	// Case-insensitive exact or trailing-wildcard match, covering the
	// manifest PATH patterns in practice.
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(strings.ToLower(s), strings.ToLower(strings.TrimSuffix(pattern, "*")))
	}
	return strings.EqualFold(s, pattern)
}

// AddPath prepends paths when absent, mirroring Add-Path
// (lib/system.ps1:96-120). Force prepends regardless.
func AddPath(store EnvStore, paths []string, targetVar string, global, force bool) {
	if targetVar == "" {
		targetVar = "PATH"
	}
	current := store.Get(targetVar, global)
	in, _ := SplitPathLike(paths, current)
	if in == "" || force {
		combined := append(append([]string{}, paths...), strings.Split(current, ";")...)
		var clean []string
		for _, p := range combined {
			if p != "" {
				clean = append(clean, p)
			}
		}
		_ = store.Set(targetVar, strings.Join(clean, ";"), global)
		fmt.Printf("Adding %s to %s path.\n", strings.Join(paths, ";"), scopeName(global))
	}
}

// RemovePath strips paths, mirroring Remove-Path
// (lib/system.ps1:122-149).
func RemovePath(store EnvStore, paths []string, targetVar string, global bool) {
	if targetVar == "" {
		targetVar = "PATH"
	}
	current := store.Get(targetVar, global)
	in, rest := SplitPathLike(paths, current)
	if in != "" {
		_ = store.Set(targetVar, rest, global)
		fmt.Printf("Removing %s from %s path.\n", strings.Join(paths, ";"), scopeName(global))
	}
}

func scopeName(global bool) string {
	if global {
		return "global"
	}
	return "your"
}

// ApplyEnvAddPath joins manifest paths under the install dir and
// appends them, mirroring env_add_path (lib/install.ps1:309-319).
// Entries escaping the install dir are dropped.
func ApplyEnvAddPath(entries []string, dir string, global bool) {
	ApplyEnvAddPathWith(entries, dir, global, DefaultStore())
}

// ApplyEnvAddPathWith is the testable form of ApplyEnvAddPath.
func ApplyEnvAddPathWith(entries []string, dir string, global bool, store EnvStore) {
	if len(entries) == 0 {
		return
	}
	dir = strings.TrimRight(dir, `\`)
	var paths []string
	for _, e := range entries {
		if e == "" {
			continue
		}
		abs := filepath.Join(dir, e)
		if !isInDir(dir, abs) {
			continue
		}
		paths = append(paths, abs)
	}
	if len(paths) == 0 {
		return
	}
	AddPath(store, paths, isolatedVar(), global, true)
}

// ApplyEnvSet expands and sets variables, mirroring env_set
// (lib/install.ps1:331-342).
func ApplyEnvSet(vars map[string]string, global bool) {
	ApplyEnvSetWith(vars, global, DefaultStore())
}

// ApplyEnvSetWith is the testable form of ApplyEnvSet.
func ApplyEnvSetWith(vars map[string]string, global bool, store EnvStore) {
	for name, val := range vars {
		expanded := os.ExpandEnv(val)
		fmt.Printf("Setting %s environment variable: %s = %s\n", scopeName(global), name, expanded)
		_ = store.Set(name, expanded, global)
	}
}

// RemoveEnv mirrors env_rm_path plus env_rm
// (lib/install.ps1:321-353).
func RemoveEnv(entries []string, vars map[string]string, dir string, global bool, store EnvStore) {
	if store == nil {
		store = DefaultStore()
	}
	if len(entries) > 0 {
		var paths []string
		for _, e := range entries {
			if e == "" {
				continue
			}
			abs := filepath.Join(strings.TrimRight(dir, `\`), e)
			if isInDir(dir, abs) {
				paths = append(paths, abs)
			}
		}
		RemovePath(store, paths, "PATH", global)
		RemovePath(store, paths, isolatedVar(), global)
	}
	for name := range vars {
		fmt.Printf("Removing %s environment variable: %s\n", scopeName(global), name)
		_ = store.Set(name, "", global)
	}
}

func isolatedVar() string {
	if v := os.Getenv("SCOOP_PATH_VAR"); v != "" {
		return v
	}
	return "PATH"
}

// EnsureInPSModulePath prepends dir to PSModulePath when absent,
// mirroring ensure_in_psmodulepath (lib/psmodules.ps1:44-54).
func EnsureInPSModulePath(dir string, global bool) {
	EnsureInPSModulePathWith(dir, global, DefaultStore())
}

// EnsureInPSModulePathWith is the testable form.
func EnsureInPSModulePathWith(dir string, global bool, store EnvStore) {
	current := store.Get("PSModulePath", global)
	if current == "" && !global {
		if home, err := os.UserHomeDir(); err == nil {
			current = filepath.Join(home, "Documents", "WindowsPowerShell", "Modules")
		}
	}
	for _, part := range strings.Split(current, ";") {
		if strings.EqualFold(part, dir) {
			return
		}
	}
	fmt.Printf("Adding %s to %s PowerShell module path.\n", dir, scopeName(global))
	_ = store.Set("PSModulePath", dir+";"+current, global)
}
