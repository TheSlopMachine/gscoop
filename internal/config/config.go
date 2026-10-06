// Package config manages ~/config/scoop/config.json for gscoop.
//
// Lookup is case-insensitive: names lowercase on store and on lookup
// (lib/core.ps1:118-124, lib/core.ps1:133). Set converts "True"/"False"
// strings to booleans (lib/core.ps1:140-142), removes the property on null
// (lib/core.ps1:153-155), and persists UTF8-no-BOM JSON (lib/core.ps1:157).
// Unknown keys are tolerated (lib/core.ps1:118-124,
// libexec/scoop-config.ps1:166-189), so Go-only keys are safe to add.
// Root, global, and cache directories resolve from environment first, then
// config, then defaults (lib/core.ps1:1375-1385). No emojis.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Go-only keys (technical plan Appendix C). Classic ignores unknown keys,
// so these never affect classic Scoop.
const (
	KeyMaxDownloads   = "max_downloads"
	KeySplitDownloads = "split_downloads"
	KeyShallowBuckets = "shallow_buckets"
	KeyUseExternalGit = "use_external_git"
	KeyPSHost         = "pshost"
	KeyGscoopChannel  = "gscoop_channel"
)

// Defaults maps every key with a documented default to that default.
// Classic key defaults come from spec/config-keys.md section 2; Go-only
// defaults come from the technical plan Appendix C.
func Defaults() map[string]any {
	return map[string]any{
		"aria2-enabled":                   true,
		"aria2-warning-enabled":           true,
		"aria2-fallback-enabled":          true,
		"aria2-retry-wait":                2,
		"aria2-split":                     5,
		"aria2-max-connection-per-server": 5,
		"aria2-min-split-size":            "5M",
		"autostash_on_conflict":           false,
		"debug":                           false,
		"force_update":                    false,
		"scoop_branch":                    "master",
		"scoop_repo":                      "https://github.com/ScoopInstaller/Scoop",
		"shim":                            "kiennq",
		"show_manifest":                   false,
		"show_update_log":                 true,
		"use_git_history":                 true,
		KeyMaxDownloads:                   4,
		KeySplitDownloads:                 1,
		KeyShallowBuckets:                 false,
		KeyUseExternalGit:                 false,
		KeyPSHost:                         "powershell.exe",
		KeyGscoopChannel:                  "stable",
	}
}

// KnownKeys lists every consumed key in lowercase canonical spelling:
// spec/config-keys.md section 2 plus the Go-only additions. Aria2 keys use
// the dash spelling because that is what the code reads
// (lib/download.ps1:263, lib/download.ps1:348-350, lib/download.ps1:459).
// Unknown keys are still stored and returned; this list is documentation,
// never a gate.
func KnownKeys() []string {
	return []string{
		"alias",
		"aria2-enabled",
		"aria2-warning-enabled",
		"aria2-fallback-enabled",
		"aria2-retry-wait",
		"aria2-split",
		"aria2-max-connection-per-server",
		"aria2-min-split-size",
		"aria2-options",
		"autostash_on_conflict",
		"cache_path",
		"cat_style",
		"debug",
		"default_architecture",
		"force_update",
		"gh_token",
		"global_path",
		"hold_update_until",
		"ignore_running_processes",
		"last_update",
		"no_junction",
		"private_hosts",
		"proxy",
		"root_path",
		"scoop_branch",
		"scoop_repo",
		"shim",
		"show_manifest",
		"show_update_log",
		"update_nightly",
		"use_external_7zip",
		"use_git_history",
		"use_isolated_path",
		"use_lessmsi",
		"use_sqlite_cache",
		"virustotal_api_key",
		KeyMaxDownloads,
		KeySplitDownloads,
		KeyShallowBuckets,
		KeyUseExternalGit,
		KeyPSHost,
		KeyGscoopChannel,
	}
}

// Store holds one config.json document with stable key order. Order follows
// the file on load with new keys appended, so saves stay diff-clean
// (technical plan section 6.1: ordered writer, never map iteration).
type Store struct {
	path   string
	order  []string
	values map[string]json.RawMessage
}

// Load reads path into a Store. A missing file yields an empty Store (like
// load_cfg returning null, lib/core.ps1:103-116); malformed JSON is an
// error. A leading UTF-8 BOM is tolerated on read and never written.
func Load(path string) (*Store, error) {
	s := &Store{path: path, values: make(map[string]json.RawMessage)}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("loading %s: empty config file", path)
	}
	if string(trimmed) == "null" {
		return s, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", path, err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("loading %s: config root must be an object", path)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("loading %s: object key must be a string", path)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}
		lower := strings.ToLower(key)
		if _, seen := s.values[lower]; !seen {
			s.order = append(s.order, lower)
		}
		s.values[lower] = value
	}
	return s, nil
}

// LoadDefault loads ConfigFilePath().
func LoadDefault() (*Store, error) {
	return Load(ConfigFilePath())
}

// Path returns the file backing this Store.
func (s *Store) Path() string {
	return s.path
}

// Keys returns stored keys in file order.
func (s *Store) Keys() []string {
	return append([]string(nil), s.order...)
}

// Len returns the number of stored keys.
func (s *Store) Len() int {
	return len(s.order)
}

// Get returns the raw value for name, case-insensitively
// (lib/core.ps1:118-124).
func (s *Store) Get(name string) (json.RawMessage, bool) {
	v, ok := s.values[strings.ToLower(name)]
	return v, ok
}

// Value unmarshals the value for name to any, case-insensitively. Numbers
// decode as float64, matching encoding/json defaults.
func (s *Store) Value(name string) (any, bool) {
	raw, ok := s.Get(name)
	if !ok {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	return v, true
}

// ValueOr returns Value(name), or def when unset or null
// (lib/core.ps1:120-122: the default applies only when stored null).
func (s *Store) ValueOr(name string, def any) any {
	v, ok := s.Value(name)
	if !ok || v == nil {
		return def
	}
	return v
}

// GetString returns the string value for name. JSON strings decode
// directly; bools and numbers render in PowerShell display form.
func (s *Store) GetString(name string) (string, bool) {
	v, ok := s.Value(name)
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		if t {
			return "True", true
		}
		return "False", true
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t)), true
		}
		return fmt.Sprintf("%v", t), true
	default:
		compact, err := json.Marshal(t)
		if err != nil {
			return "", false
		}
		return string(compact), true
	}
}

// GetBool returns the bool value for name, accepting JSON bools and
// "True"/"False" strings in any case.
func (s *Store) GetBool(name string) (bool, bool) {
	v, ok := s.Value(name)
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		if strings.EqualFold(t, "true") {
			return true, true
		}
		if strings.EqualFold(t, "false") {
			return false, true
		}
	}
	return false, false
}

// Set stores value under the lowercased name (lib/core.ps1:133). String
// values "True"/"False" in any case coerce to booleans, matching the
// case-insensitive -eq in lib/core.ps1:140-142. A nil value removes the
// property (lib/core.ps1:153-155). New keys append to file order.
// Complete-ConfigChange side effects (USE_ISOLATED_PATH moves,
// USE_SQLITE_CACHE bootstrap, lib/core.ps1:162-235) are not run here; the
// mutation phase performs them explicitly.
func (s *Store) Set(name string, value any) error {
	lower := strings.ToLower(name)
	if value == nil {
		s.Remove(lower)
		return nil
	}
	if text, ok := value.(string); ok {
		if strings.EqualFold(text, "true") {
			value = true
		} else if strings.EqualFold(text, "false") {
			value = false
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, seen := s.values[lower]; !seen {
		s.order = append(s.order, lower)
	}
	s.values[lower] = raw
	return nil
}

// Remove deletes name, case-insensitively, and reports whether it existed
// (libexec/scoop-config.ps1:172-174: scoop config rm <name>).
func (s *Store) Remove(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := s.values[lower]; !ok {
		return false
	}
	delete(s.values, lower)
	for i, k := range s.order {
		if k == lower {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return true
}

// Save persists the Store as UTF8-no-BOM JSON with an ordered writer and a
// trailing newline (lib/core.ps1:157-158).
func (s *Store) Save() error {
	return s.SaveAs(s.path)
}

// SaveAs persists the Store to path and repoints the Store at it.
func (s *Store) SaveAs(path string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range s.order {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, s.values[key], "", "  "); err != nil {
			return err
		}
		lines := strings.Split(pretty.String(), "\n")
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return err
		}
		buf.WriteString("  ")
		buf.Write(keyJSON)
		buf.WriteString(": ")
		buf.WriteString(lines[0])
		for _, line := range lines[1:] {
			buf.WriteString("\n  ")
			buf.WriteString(line)
		}
		if i+1 < len(s.order) {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	s.path = path
	return nil
}

// Display formats a value the way scoop config prints it
// (libexec/scoop-config.ps1:179-188): ok is false for null, matching
// "'<name>' is not set". DateTime values print in o format; ISO-8601
// strings re-emit as RFC3339Nano, the Go equivalent.
func (s *Store) Display(name string) (text string, ok bool) {
	v, found := s.Value(name)
	if !found || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		if parsed, err := parseDateTime(t); err == nil {
			return parsed.Format(time.RFC3339Nano), true
		}
		return t, true
	case bool:
		if t {
			return "True", true
		}
		return "False", true
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t)), true
		}
		return fmt.Sprintf("%v", t), true
	default:
		compact, err := json.Marshal(t)
		if err != nil {
			return "", false
		}
		return string(compact), true
	}
}

// parseDateTime accepts the o round-trip format plus the YYYY-MM-DD and
// YYYY/MM/DD forms the hold help references
// (libexec/scoop-config.ps1:116-120), mirroring [DateTime]::Parse
// tolerance for the shapes Scoop writes.
func parseDateTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
		"2006-01-02T15:04:05.9999999",
	}
	var err error
	var parsed time.Time
	for _, layout := range layouts {
		if parsed, err = time.Parse(layout, s); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable date %q", s)
}

// ConfigDir returns the config home: the first non-empty value of
// XDG_CONFIG_HOME and ~/.config (lib/core.ps1:1347). Classic pipes the pair
// through Select-Object without filtering, which drops $null but keeps "";
// Go uses first non-empty, which matches whenever XDG is unset.
func ConfigDir(xdg, home string) string {
	if xdg != "" {
		return filepath.Join(xdg, "scoop")
	}
	return filepath.Join(home, ".config", "scoop")
}

// ConfigFilePathFor resolves the config file path for explicit inputs so
// the precedence is testable. When exePath sits under apps/scoop/current
// and <root>/config.json exists, that portable path wins
// (lib/core.ps1:1349-1372); otherwise XDG/home applies (lib/core.ps1:1347).
func ConfigFilePathFor(exePath, xdg, home string, exists func(string) bool) string {
	if exePath != "" {
		lowered := strings.ToLower(filepath.ToSlash(exePath))
		if idx := strings.Index(lowered, "apps/scoop/current"); idx >= 0 {
			root := exePath[:idx]
			portable := filepath.Join(root, "config.json")
			if exists(portable) {
				return portable
			}
		}
	}
	return filepath.Join(ConfigDir(xdg, home), "config.json")
}

// ConfigFilePath resolves the live config file path from the environment.
func ConfigFilePath() string {
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	return ConfigFilePathFor(exe, os.Getenv("XDG_CONFIG_HOME"), home, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

// ResolveScoopDir selects the first non-empty value of the SCOOP
// environment variable, ROOT_PATH config, and ~/scoop
// (lib/core.ps1:1375). The checkout-grandparent fallback applies to the
// PowerShell core layout only; the Go binary has no lib/ checkout, so it is
// omitted.
func ResolveScoopDir(envScoop, rootPath, home string) string {
	for _, candidate := range []string{envScoop, rootPath, filepath.Join(home, "scoop")} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// ResolveGlobalDir selects the first non-empty value of SCOOP_GLOBAL,
// GLOBAL_PATH config, and CommonApplicationData\scoop
// (lib/core.ps1:1378).
func ResolveGlobalDir(envGlobal, globalPath, commonAppData string) string {
	for _, candidate := range []string{envGlobal, globalPath, filepath.Join(commonAppData, "scoop")} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// ResolveCacheDir selects the first non-empty value of SCOOP_CACHE,
// CACHE_PATH config, and scoopdir\cache (lib/core.ps1:1385).
func ResolveCacheDir(envCache, cachePath, scoopDir string) string {
	for _, candidate := range []string{envCache, cachePath, filepath.Join(scoopDir, "cache")} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// ScoopDir resolves the live scoop root from environment plus this Store.
func (s *Store) ScoopDir() string {
	rootPath, _ := s.GetString("root_path")
	home, _ := os.UserHomeDir()
	return ResolveScoopDir(os.Getenv("SCOOP"), rootPath, home)
}

// GlobalDir resolves the live global root from environment plus this Store.
func (s *Store) GlobalDir() string {
	globalPath, _ := s.GetString("global_path")
	commonAppData := os.Getenv("ProgramData")
	if commonAppData == "" {
		commonAppData = `C:\ProgramData`
	}
	return ResolveGlobalDir(os.Getenv("SCOOP_GLOBAL"), globalPath, commonAppData)
}

// CacheDir resolves the live cache directory from environment plus this
// Store.
func (s *Store) CacheDir() string {
	cachePath, _ := s.GetString("cache_path")
	return ResolveCacheDir(os.Getenv("SCOOP_CACHE"), cachePath, s.ScoopDir())
}

// ResolveGitHubToken selects the first non-empty value of SCOOP_GH_TOKEN,
// GH_TOKEN config, GH_TOKEN, and GITHUB_TOKEN (lib/download.ps1:589-591).
func ResolveGitHubToken(envScoopGH, cfgGH, envGH, envGitHub string) string {
	for _, candidate := range []string{envScoopGH, cfgGH, envGH, envGitHub} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// GitHubToken resolves the live token from environment plus this Store.
func (s *Store) GitHubToken() string {
	cfgGH, _ := s.GetString("gh_token")
	return ResolveGitHubToken(os.Getenv("SCOOP_GH_TOKEN"), cfgGH, os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
}

// DebugEnabled reports whether debug output is on: DEBUG config or
// SCOOP_DEBUG equal-fold "true" (lib/core.ps1:316).
func DebugEnabled(debugValue any, scoopDebugEnv string) bool {
	if text, ok := debugValue.(string); ok {
		if strings.EqualFold(text, "true") {
			return true
		}
	} else if flag, ok := debugValue.(bool); ok && flag {
		return true
	}
	return strings.EqualFold(scoopDebugEnv, "true")
}

// Debug reports DebugEnabled for this Store plus the live environment.
func (s *Store) Debug() bool {
	v, _ := s.Value("debug")
	return DebugEnabled(v, os.Getenv("SCOOP_DEBUG"))
}
