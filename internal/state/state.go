// Package state derives Scoop on-disk paths and reads installed-app
// metadata for gscoop.
//
// Layout mirrors lib/core.ps1:366-387 and lib/buckets.ps1:1: apps,
// persist, cache, buckets, shims, modules, workspace, and scoop.db under
// the user or global root. Installed-version resolution mirrors
// Select-CurrentVersion (lib/versions.ps1:31-71) and Get-InstalledVersion
// (lib/versions.ps1:96-101); installed/failed mirror lib/core.ps1:407-430;
// cache naming mirrors cache_path (lib/core.ps1:388-403); hold detection
// mirrors libexec/scoop-hold.ps1, libexec/scoop-unhold.ps1, and
// Test-ScoopCoreOnHold (lib/core.ps1:1263-1284).
//
// The package takes explicit directories and flags instead of reading
// config or environment so every rule stays unit-testable. No emojis.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Roots holds the three derived roots: user scoopdir, globaldir, and
// cachedir (lib/core.ps1:1375-1385).
type Roots struct {
	// Scoop is the user root ($scoopdir).
	Scoop string
	// Global is the global-apps root ($globaldir).
	Global string
	// Cache is the download cache root ($cachedir).
	Cache string
}

// Base returns the root for a scope (lib/core.ps1:366).
func (r Roots) Base(global bool) string {
	if global {
		return r.Global
	}
	return r.Scoop
}

// Apps returns the apps directory for a scope (lib/core.ps1:367).
func (r Roots) Apps(global bool) string {
	return filepath.Join(r.Base(global), "apps")
}

// Shims returns the shims directory for a scope (lib/core.ps1:368).
func (r Roots) Shims(global bool) string {
	return filepath.Join(r.Base(global), "shims")
}

// Modules returns the modules directory for a scope (lib/core.ps1:369).
func (r Roots) Modules(global bool) string {
	return filepath.Join(r.Base(global), "modules")
}

// App returns the app directory (lib/core.ps1:370).
func (r Roots) App(app string, global bool) string {
	return filepath.Join(r.Apps(global), app)
}

// Version returns the version directory (lib/core.ps1:371).
func (r Roots) Version(app, version string, global bool) string {
	return filepath.Join(r.App(app, global), version)
}

// Current returns the current directory: under NO_JUNCTION (except app
// scoop) it resolves via the current version, otherwise it is the current
// junction (lib/core.ps1:373-383).
func (r Roots) Current(app string, global, noJunction bool, currentVersion string) string {
	if noJunction && app != "scoop" {
		return filepath.Join(r.App(app, global), currentVersion)
	}
	return filepath.Join(r.App(app, global), "current")
}

// Persist returns the persist directory (lib/core.ps1:385).
func (r Roots) Persist(app string, global bool) string {
	return filepath.Join(r.Base(global), "persist", app)
}

// Workspace returns the user-manifests directory. It has no global
// variant (lib/core.ps1:386).
func (r Roots) Workspace() string {
	return filepath.Join(r.Base(false), "workspace")
}

// UserManifest returns the workspace manifest path for an app
// (lib/core.ps1:387).
func (r Roots) UserManifest(app string) string {
	return filepath.Join(r.Workspace(), app+".json")
}

// Buckets returns the buckets directory (lib/buckets.ps1:1).
func (r Roots) Buckets() string {
	return filepath.Join(r.Scoop, "buckets")
}

// Bucket returns the bucket directory for name, defaulting empty names to
// main and descending into the bucket subdirectory when present unless
// root is set (lib/buckets.ps1:3-29).
func (r Roots) Bucket(name string, root bool) string {
	if name == "" {
		name = "main"
	}
	dir := filepath.Join(r.Buckets(), name)
	if !root {
		if info, err := os.Stat(filepath.Join(dir, "bucket")); err == nil && info.IsDir() {
			return filepath.Join(dir, "bucket")
		}
	}
	return dir
}

// ScoopDB returns the SQLite search-cache path
// (lib/database.ps1:87).
func (r Roots) ScoopDB() string {
	return filepath.Join(r.Scoop, "scoop.db")
}

// AppName strips a bucket/ prefix, mirroring installed/failed
// (lib/core.ps1:407-430).
func AppName(app string) string {
	if i := strings.LastIndexAny(app, "/\\"); i >= 0 {
		return app[i+1:]
	}
	return app
}

// InstalledApps lists child directories of the apps dir excluding scoop
// (lib/core.ps1:417-422). Results sort alphabetically for determinism.
func InstalledApps(appsDir string) []string {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil
	}
	var apps []string
	for _, e := range entries {
		if !e.IsDir() || strings.EqualFold(e.Name(), "scoop") {
			continue
		}
		apps = append(apps, e.Name())
	}
	sort.Strings(apps)
	return apps
}

// InstalledVersions returns installed version names oldest to newest,
// mirroring Get-InstalledVersion (lib/versions.ps1:96-101): version
// directories containing scoop-install.json or install.json, ordered by
// LastWriteTimeUtc, unique, excluding current and _*.old*.
func InstalledVersions(appPath string) []string {
	entries, err := os.ReadDir(appPath)
	if err != nil {
		return nil
	}
	type candidate struct {
		name string
		mod  time.Time
	}
	var found []candidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.EqualFold(name, "current") || isOldBackup(name) {
			continue
		}
		dir := filepath.Join(appPath, name)
		best := time.Time{}
		seen := false
		for _, meta := range []string{"scoop-install.json", "install.json"} {
			info, err := os.Stat(filepath.Join(dir, meta))
			if err != nil || info.IsDir() {
				continue
			}
			if !seen || info.ModTime().Before(best) {
				best = info.ModTime()
			}
			seen = true
		}
		if seen {
			found = append(found, candidate{name: name, mod: best})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].mod.Equal(found[j].mod) {
			return found[i].name < found[j].name
		}
		return found[i].mod.Before(found[j].mod)
	})
	names := make([]string, 0, len(found))
	for _, c := range found {
		names = append(names, c.name)
	}
	return names
}

// isOldBackup mirrors $_ -notlike '_*.old*' (lib/versions.ps1:101):
// case-insensitive leading underscore with .old later in the name.
func isOldBackup(name string) bool {
	lowered := strings.ToLower(name)
	return strings.HasPrefix(lowered, "_") && strings.Contains(lowered, ".old")
}

// ManifestVersion extracts the version field from manifest JSON bytes.
// Numbers stringify; missing or null versions report false.
func ManifestVersion(data []byte) (string, bool) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false
	}
	raw, ok := doc["version"]
	if !ok || string(raw) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if f == float64(int64(f)) {
			return fmt.Sprintf("%d", int64(f)), true
		}
		return fmt.Sprintf("%v", f), true
	}
	return "", false
}

// ManifestFile returns the installed-manifest path for a version dir:
// scoop-manifest.json wins, manifest.json is the legacy fallback
// (lib/manifest.ps1:134-139).
func ManifestFile(versionDir string) (string, bool) {
	for _, name := range []string{"scoop-manifest.json", "manifest.json"} {
		path := filepath.Join(versionDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

// ReadManifestVersion reads the installed manifest version for a version
// dir, following the scoop-manifest.json/manifest.json fallback. It
// mirrors the manifest reads in Select-CurrentVersion
// (lib/versions.ps1:54-55). Unreadable or unparsable manifests report
// false so callers fall through, matching parse_json returning null
// (lib/manifest.ps1:5-11).
func ReadManifestVersion(versionDir string) (string, bool) {
	path, ok := ManifestFile(versionDir)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return ManifestVersion(data)
}

// SelectCurrentVersion resolves the current version for an installed app
// directory (lib/versions.ps1:31-71). With junctions enabled it reads
// current/scoop-manifest.json (legacy manifest.json fallback); a nightly
// manifest version resolves to the junction target leaf
// (lib/versions.ps1:57-59). With no junction version available it returns
// the newest time-ordered installed version, else false.
func SelectCurrentVersion(appPath string, noJunction bool) (string, bool) {
	if !noJunction {
		if version, ok := ReadManifestVersion(filepath.Join(appPath, "current")); ok {
			if strings.EqualFold(version, "nightly") {
				if target, err := os.Readlink(filepath.Join(appPath, "current")); err == nil {
					return filepath.Base(target), true
				}
			} else {
				return version, true
			}
		}
	}
	installed := InstalledVersions(appPath)
	if len(installed) == 0 {
		return "", false
	}
	return installed[len(installed)-1], true
}

// IsInstalled reports whether the app directory resolves to a current
// version (lib/core.ps1:407-416).
func IsInstalled(appPath string, noJunction bool) bool {
	_, ok := SelectCurrentVersion(appPath, noJunction)
	return ok
}

// IsFailed reports a failed install: the app path exists without (current
// linkage and an installed version) (lib/core.ps1:425-430).
func IsFailed(appPath string, noJunction bool) bool {
	if _, err := os.Stat(appPath); err != nil {
		return false
	}
	hasCurrent := noJunction
	if !hasCurrent {
		if _, err := os.Lstat(filepath.Join(appPath, "current")); err == nil {
			hasCurrent = true
		}
	}
	return !(hasCurrent && IsInstalled(appPath, noJunction))
}

// CachePath names the cache file for an app version URL
// (lib/core.ps1:388-403). Each maximal run of characters outside
// word/dot/hyphen becomes one underscore; when that legacy file exists it
// wins. Otherwise the tail is replaced by the first 7 lowercase hex
// characters of SHA256(UTF8(url)) plus the URL extension. Go checks the
// legacy form first, permanently.
func CachePath(cacheDir, app, version, url string) string {
	underscored := underscoreURL(url)
	legacy := filepath.Join(cacheDir, app+"#"+version+"#"+underscored)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	sum := sha256.Sum256([]byte(url))
	sha := hex.EncodeToString(sum[:])[:7]
	tail := sha + urlExtension(url)
	if underscored == "" {
		return filepath.Join(cacheDir, app+"#"+version+"#"+tail)
	}
	return strings.ReplaceAll(legacy, underscored, tail)
}

// underscoreURL mirrors $url -replace '[^\w\.\-]+', '_'
// (lib/core.ps1:389): maximal disallowed runs collapse to one underscore.
func underscoreURL(url string) string {
	var b strings.Builder
	b.Grow(len(url))
	pending := false
	flush := func() {
		if pending {
			b.WriteByte('_')
			pending = false
		}
	}
	for _, r := range url {
		if r == '.' || r == '-' || r == '_' || isWordRune(r) {
			flush()
			b.WriteRune(r)
		} else {
			pending = true
		}
	}
	flush()
	return b.String()
}

// isWordRune approximates .NET \w for URL text.
func isWordRune(r rune) bool {
	if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_' {
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// urlExtension mirrors [IO.Path]::GetExtension($url)
// (lib/core.ps1:399): the suffix from the last dot after the last path
// separator, or "" when absent.
func urlExtension(url string) string {
	sep := strings.LastIndexAny(url, "/\\")
	dot := strings.LastIndex(url, ".")
	if dot < 0 || dot < sep {
		return ""
	}
	return url[dot:]
}

// InstallInfo mirrors the scoop-install.json payload recorded by
// install_app (lib/install.ps1:71-73) with hold added by scoop hold
// (libexec/scoop-hold.ps1:49-68).
type InstallInfo struct {
	Architecture string
	Bucket       string
	URL          string
	Hold         bool
}

// ParseInstallInfo decodes install-info JSON. Hold is true only for JSON
// true or "true" in any case, matching install_info.hold -eq $true
// (lib/core.ps1:576). Unknown fields are ignored.
func ParseInstallInfo(data []byte) (InstallInfo, error) {
	var raw struct {
		Architecture string          `json:"architecture"`
		Bucket       string          `json:"bucket"`
		URL          string          `json:"url"`
		Hold         json.RawMessage `json:"hold"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return InstallInfo{}, err
	}
	info := InstallInfo{Architecture: raw.Architecture, Bucket: raw.Bucket, URL: raw.URL}
	if len(raw.Hold) == 0 || string(raw.Hold) == "null" {
		return info, nil
	}
	var flag bool
	if err := json.Unmarshal(raw.Hold, &flag); err == nil {
		info.Hold = flag
		return info, nil
	}
	var text string
	if err := json.Unmarshal(raw.Hold, &text); err == nil {
		info.Hold = strings.EqualFold(text, "true")
	}
	return info, nil
}

// InstallInfoFile returns the install-info path for a version dir:
// scoop-install.json wins, install.json is the legacy fallback
// (lib/manifest.ps1:149-154).
func InstallInfoFile(versionDir string) (string, bool) {
	for _, name := range []string{"scoop-install.json", "install.json"} {
		path := filepath.Join(versionDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

// ReadInstallInfo reads install info for a version dir with legacy
// fallback. Missing or unparsable files report false.
func ReadInstallInfo(versionDir string) (InstallInfo, bool) {
	path, ok := InstallInfoFile(versionDir)
	if !ok {
		return InstallInfo{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return InstallInfo{}, false
	}
	info, err := ParseInstallInfo(data)
	if err != nil {
		return InstallInfo{}, false
	}
	return info, true
}

// CoreHoldState evaluates HOLD_UPDATE_UNTIL at now, mirroring
// Test-ScoopCoreOnHold (lib/core.ps1:1263-1284). Empty means not held. A
// future date holds (clear false). An expired or unparsable value clears,
// so the caller removes the config key and warns.
func CoreHoldState(holdUntil string, now time.Time) (held, clear bool) {
	if holdUntil == "" {
		return false, false
	}
	parsed, err := parseHoldDate(holdUntil)
	if err != nil {
		return false, true
	}
	if parsed.After(now) {
		return true, false
	}
	return false, true
}

// parseHoldDate accepts the o round-trip format plus the YYYY-MM-DD and
// YYYY/MM/DD forms from the config help
// (libexec/scoop-config.ps1:116-120).
func parseHoldDate(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
	}
	var err error
	var parsed time.Time
	for _, layout := range layouts {
		if parsed, err = time.Parse(layout, s); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable hold date %q", s)
}
