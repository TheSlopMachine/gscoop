package commands

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"
)

// InstallInfo mirrors the scoop-install.json payload written by
// save_install_info in lib/manifest.ps1.
type InstallInfo struct {
	Architecture string `json:"architecture"`
	Bucket       string `json:"bucket"`
	URL          string `json:"url"`
	Hold         bool   `json:"hold"`
}

// Manifest holds the manifest fields needed by the read-only commands.
// The full contract stays in schema.json; this struct covers display,
// dependency, and status decisions only.
//
// TODO(manifest): replace parsing and arch_specific resolution with
// internal/manifest when it lands.
type Manifest struct {
	Version      string         `json:"version"`
	Description  string         `json:"description"`
	Homepage     string         `json:"homepage"`
	License      any            `json:"license"`
	Depends      any            `json:"depends"`
	Bin          any            `json:"bin"`
	Shortcuts    any            `json:"shortcuts"`
	Suggest      map[string]any `json:"suggest"`
	Notes        any            `json:"notes"`
	Architecture map[string]struct {
		Bin        any            `json:"bin"`
		Shortcuts  any            `json:"shortcuts"`
		EnvSet     map[string]any `json:"env_set"`
		EnvAddPath any            `json:"env_add_path"`
		URL        any            `json:"url"`
	} `json:"architecture"`
	EnvSet     map[string]any `json:"env_set"`
	EnvAddPath any            `json:"env_add_path"`
}

// DependsList normalizes the depends field to a string slice.
func (m *Manifest) DependsList() []string {
	return anyToStrings(m.Depends)
}

// BinList returns the arch-specific bin entries.
func (m *Manifest) BinList(arch string) []any {
	if m == nil {
		return nil
	}
	if a, ok := m.Architecture[arch]; ok && a.Bin != nil {
		return anyToSlice(a.Bin)
	}
	return anyToSlice(m.Bin)
}

// ShortcutsList returns the arch-specific shortcut entries.
func (m *Manifest) ShortcutsList(arch string) []any {
	if m == nil {
		return nil
	}
	if a, ok := m.Architecture[arch]; ok && a.Shortcuts != nil {
		return anyToSlice(a.Shortcuts)
	}
	return anyToSlice(m.Shortcuts)
}

func anyToSlice(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	default:
		return []any{v}
	}
}

func anyToStrings(v any) []string {
	var out []string
	for _, item := range anyToSlice(v) {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseManifestFile(path string) *Manifest {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseManifestBytes(data)
}

func parseManifestBytes(data []byte) *Manifest {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return &m
}

// InstalledApps mirrors installed_apps in lib/core.ps1, excluding scoop.
//
// TODO(state): replace with internal/state when it lands.
func (e *Env) InstalledApps(global bool) []string {
	entries, err := os.ReadDir(e.AppsDir(global))
	if err != nil {
		return nil
	}
	var apps []string
	for _, ent := range entries {
		if ent.IsDir() && ent.Name() != "scoop" {
			apps = append(apps, ent.Name())
		}
	}
	sort.Strings(apps)
	return apps
}

// InstalledVersion mirrors Get-InstalledVersion in lib/versions.ps1:
// version dirs carrying install metadata, oldest to newest by mtime.
//
// TODO(state): replace with internal/state when it lands.
func (e *Env) InstalledVersions(app string, global bool) []string {
	appPath := e.AppDir(app, global)
	entries, err := os.ReadDir(appPath)
	if err != nil {
		return nil
	}
	type cand struct {
		name string
		mod  int64
	}
	var cands []cand
	for _, ent := range entries {
		name := ent.Name()
		if name == "current" || strings.HasPrefix(name, "_") && strings.Contains(name, ".old") {
			continue
		}
		full := filepath.Join(appPath, name)
		info, err := os.Stat(full)
		if err != nil || !info.IsDir() {
			continue
		}
		for _, meta := range []string{"scoop-install.json", "install.json"} {
			fi, err := os.Stat(filepath.Join(full, meta))
			if err != nil {
				continue
			}
			cands = append(cands, cand{name, fi.ModTime().UnixNano()})
			break
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod < cands[j].mod })
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if !seen[c.name] {
			seen[c.name] = true
			out = append(out, c.name)
		}
	}
	return out
}

// CurrentVersion mirrors Select-CurrentVersion in lib/versions.ps1.
//
// TODO(state): replace with internal/state when it lands.
func (e *Env) CurrentVersion(app string, global bool) string {
	if !e.NoJunction {
		current := filepath.Join(e.AppDir(app, global), "current")
		for _, name := range []string{"scoop-manifest.json", "manifest.json"} {
			if m := parseManifestFile(filepath.Join(current, name)); m != nil && m.Version != "" {
				if m.Version == "nightly" {
					if target, err := os.Readlink(current); err == nil {
						return filepath.Base(filepath.Clean(target))
					}
				}
				return m.Version
			}
		}
		if _, err := os.Lstat(current); err == nil {
			// Junction exists but carries no readable manifest; fall through
			// to the newest version dir below.
		}
	}
	vers := e.InstalledVersions(app, global)
	if len(vers) == 0 {
		return ""
	}
	return vers[len(vers)-1]
}

// Installed mirrors installed() in lib/core.ps1.
func (e *Env) Installed(app string, global *bool) bool {
	base := strings.Split(strings.ReplaceAll(app, "\\", "/"), "/")
	name := base[len(base)-1]
	if global == nil {
		t, f := true, false
		return e.Installed(name, &t) || e.Installed(name, &f)
	}
	return e.CurrentVersion(name, *global) != ""
}

// Failed mirrors failed() in lib/core.ps1.
//
// TODO(state): replace with internal/state when it lands.
func (e *Env) Failed(app string, global bool) bool {
	base := strings.Split(strings.ReplaceAll(app, "\\", "/"), "/")
	name := base[len(base)-1]
	appPath := e.AppDir(name, global)
	if _, err := os.Stat(appPath); err != nil {
		return false
	}
	hasCurrent := e.NoJunction
	if !hasCurrent {
		_, err := os.Lstat(filepath.Join(appPath, "current"))
		hasCurrent = err == nil
	}
	g := global
	return !(hasCurrent && e.Installed(name, &g))
}

// InstallInfoFor mirrors install_info in lib/manifest.ps1, including the
// legacy install.json fallback.
//
// TODO(state): replace with internal/state when it lands.
func (e *Env) InstallInfoFor(app, version string, global bool) (*InstallInfo, string) {
	if version == "" {
		return nil, ""
	}
	dir := e.VersionDir(app, version, global)
	for _, name := range []string{"scoop-install.json", "install.json"} {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var info InstallInfo
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, ""
		}
		return &info, p
	}
	return nil, ""
}

// InstalledManifest mirrors installed_manifest in lib/manifest.ps1.
//
// TODO(manifest): replace with internal/manifest when it lands.
func (e *Env) InstalledManifest(app, version string, global bool) (*Manifest, []byte) {
	if version == "" {
		return nil, nil
	}
	dir := e.VersionDir(app, version, global)
	for _, name := range []string{"scoop-manifest.json", "manifest.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		return parseManifestBytes(data), data
	}
	return nil, nil
}

// ManifestHit is the result of resolving an app spec to a manifest.
type ManifestHit struct {
	Name     string
	Manifest *Manifest
	Bucket   string
	URL      string
	Raw      []byte
	Path     string
}

// FindManifest mirrors Get-Manifest in lib/manifest.ps1 for local sources:
// installed metadata, explicit bucket/app, bucket scan, and local paths.
// Remote URLs need the download layer and are reported as unsupported.
//
// TODO(manifest,bucket): replace with internal/manifest and internal/bucket
// when they land.
func (e *Env) FindManifest(spec string, out io.Writer) *ManifestHit {
	app := strings.TrimLeft(spec, "/")
	if isURL(app) {
		return &ManifestHit{Name: appNameFromURL(app), URL: app}
	}
	if !strings.Contains(app, "/") && e.Installed(app, nil) {
		g := true
		global := e.Installed(app, &g)
		ver := e.CurrentVersion(app, global)
		if info, _ := e.InstallInfoFor(app, ver, global); info != nil {
			if info.Bucket != "" {
				if m, raw, p := e.bucketManifest(app, info.Bucket); m != nil {
					return &ManifestHit{Name: app, Manifest: m, Bucket: info.Bucket, Raw: raw, Path: p}
				}
			} else if info.URL != "" {
				if isURL(info.URL) {
					return &ManifestHit{Name: app, URL: info.URL}
				}
				if m, raw := e.pathManifest(info.URL); m != nil {
					return &ManifestHit{Name: app, Manifest: m, URL: info.URL, Raw: raw, Path: info.URL}
				}
			}
			if m, raw := e.InstalledManifest(app, ver, global); m != nil {
				return &ManifestHit{Name: app, Manifest: m, Raw: raw, Path: e.VersionDir(app, ver, global)}
			}
			return &ManifestHit{Name: app}
		}
	}
	name, bucket := splitAppSpec(app)
	if bucket != "" {
		if m, raw, p := e.bucketManifest(name, bucket); m != nil {
			return &ManifestHit{Name: name, Manifest: m, Bucket: bucket, Raw: raw, Path: p}
		}
		return &ManifestHit{Name: name, Bucket: bucket}
	}
	if m, raw := e.pathManifest(app); m != nil {
		return &ManifestHit{Name: appNameFromURL(app), Manifest: m, URL: app, Raw: raw, Path: app}
	}
	manifest, foundBucket, multiples := e.scanBuckets(name)
	if manifest != nil {
		if len(multiples) > 1 && out != nil {
			Warnf(out, "Multiple buckets contain manifest '%s', the current selection is '%s/%s'.", name, foundBucket, name)
		}
		return &ManifestHit{Name: name, Manifest: manifest.Manifest, Bucket: foundBucket, Raw: manifest.Raw, Path: manifest.Path}
	}
	if strings.Contains(app, "/") || strings.HasSuffix(app, ".json") {
		return &ManifestHit{Name: appNameFromURL(app), URL: app}
	}
	return &ManifestHit{Name: appNameFromURL(app)}
}

func (e *Env) bucketManifest(app, bucket string) (*Manifest, []byte, string) {
	p := e.ManifestPath(app, bucket)
	if p == "" {
		deprecated := filepath.Join(e.bucketRoot(bucket), "deprecated")
		entries, err := os.ReadDir(deprecated)
		if err == nil {
			for _, ent := range entries {
				if !ent.IsDir() && strings.EqualFold(ent.Name(), sanitaryPath(app)+".json") {
					p = filepath.Join(deprecated, ent.Name())
					break
				}
			}
		}
	}
	if p == "" {
		return nil, nil, ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, ""
	}
	return parseManifestBytes(data), data, p
}

func (e *Env) pathManifest(path string) (*Manifest, []byte) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	return parseManifestBytes(data), data
}

type scannedHit struct {
	Manifest *Manifest
	Raw      []byte
	Path     string
}

func (e *Env) scanBuckets(app string) (*scannedHit, string, []string) {
	var hit *scannedHit
	var bucket string
	var multiples []string
	for _, b := range e.ListBucketNames() {
		p := e.ManifestPath(app, b)
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		m := parseManifestBytes(data)
		if m == nil {
			continue
		}
		multiples = append(multiples, b)
		if hit == nil {
			hit = &scannedHit{m, data, p}
			bucket = b
		}
	}
	return hit, bucket, multiples
}

// ManifestPath mirrors manifest_path in lib/manifest.ps1: recursive search
// for the sanitary app name under the bucket directory.
//
// TODO(bucket): replace with internal/bucket when it lands.
func (e *Env) ManifestPath(app, bucket string) string {
	dir := e.FindBucketDirectory(bucket)
	var found string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), sanitaryPath(app)+".json") {
			found = p
		}
		return nil
	})
	return found
}

// FindBucketDirectory mirrors Find-BucketDirectory in lib/buckets.ps1.
//
// TODO(bucket): replace with internal/bucket when it lands.
func (e *Env) FindBucketDirectory(name string) string {
	if name == "" {
		name = "main"
	}
	bucket := filepath.Join(e.BucketsDir(), name)
	if fi, err := os.Stat(filepath.Join(bucket, "bucket")); err == nil && fi.IsDir() {
		return filepath.Join(bucket, "bucket")
	}
	return bucket
}

func (e *Env) bucketRoot(name string) string {
	if name == "" {
		name = "main"
	}
	return filepath.Join(e.BucketsDir(), name)
}

// ListBucketNames mirrors Get-LocalBucket ordering: known buckets first.
//
// TODO(bucket): replace with internal/bucket when it lands.
func (e *Env) ListBucketNames() []string {
	entries, err := os.ReadDir(e.BucketsDir())
	if err != nil {
		return nil
	}
	var names []string
	for _, ent := range entries {
		if ent.IsDir() {
			names = append(names, ent.Name())
		}
	}
	known := []string{"main", "extras", "versions", "java", "games", "nirsoft", "sysinternals", "php", "nerd-fonts"}
	ordered := []string{}
	for _, k := range known {
		for _, n := range names {
			if strings.EqualFold(n, k) {
				ordered = append(ordered, n)
			}
		}
	}
	for _, n := range names {
		dup := false
		for _, o := range ordered {
			if strings.EqualFold(o, n) {
				dup = true
				break
			}
		}
		if !dup {
			ordered = append(ordered, n)
		}
	}
	return ordered
}

// Bucket describes one local bucket for list/export output.
type Bucket struct {
	Name      string
	Source    string
	Updated   string
	Manifests int
}

// ListBuckets mirrors list_buckets in lib/buckets.ps1. Git remotes record
// the origin URL and HEAD date; plain directories record the local path and
// mtime.
//
// TODO(bucket,gitengine): read remotes and dates through internal/bucket and
// the gitengine seam when they land.
func (e *Env) ListBuckets() []Bucket {
	var out []Bucket
	for _, name := range e.ListBucketNames() {
		root := e.bucketRoot(name)
		b := Bucket{Name: name}
		if url, when := gitOriginInfo(root); url != "" {
			b.Source = url
			b.Updated = when
		} else {
			b.Source = friendlyPath(root)
			if fi, err := os.Stat(root); err == nil {
				b.Updated = formatTime(fi.ModTime())
			}
		}
		b.Manifests = countManifests(e.FindBucketDirectory(name))
		out = append(out, b)
	}
	return out
}

func countManifests(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			n++
		}
		return nil
	})
	return n
}

// gitOriginInfo reads the origin URL and HEAD commit date without shelling
// out. The date is the HEAD commit date, an approximation of the classic
// `git log --format=%aI -n 1` output used by list_buckets.
//
// TODO(gitengine): replace with the go-git engine log query when it lands.
func gitOriginInfo(root string) (string, string) {
	repo, err := git.PlainOpen(root)
	if err != nil {
		return "", ""
	}
	url := ""
	if cfg, err := repo.Config(); err == nil {
		if rem, ok := cfg.Remotes["origin"]; ok && len(rem.URLs) > 0 {
			url = rem.URLs[0]
		}
	}
	when := ""
	if head, err := repo.Head(); err == nil {
		if commit, err := repo.CommitObject(head.Hash()); err == nil {
			when = formatTime(commit.Author.When)
		}
	}
	return url, when
}

// gitFileInfo reports the HEAD commit date and author for a file under a git
// checkout, used by info. It falls back to ok=false so callers use the file
// mtime, matching the classic fallback path.
//
// TODO(gitengine): replace the HEAD approximation with a per-file log query
// when the gitengine seam lands.
func gitFileInfo(path string) (when, author string, ok bool) {
	dir := path
	for i := 0; i < 8; i++ {
		dir = filepath.Dir(dir)
		if dir == "" || dir == "." {
			return "", "", false
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			continue
		}
		head, err := repo.Head()
		if err != nil {
			return "", "", false
		}
		commit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return "", "", false
		}
		return formatTime(commit.Author.When), commit.Author.Name, true
	}
	return "", "", false
}

func splitAppSpec(spec string) (app, bucket string) {
	// Strip an @version pin; version-pinned resolution belongs to Phase 3.
	if i := strings.LastIndex(spec, "@"); i >= 0 && !strings.Contains(spec[i:], "/") && !strings.Contains(spec[i:], "\\") {
		spec = spec[:i]
	}
	parts := strings.SplitN(spec, "/", 2)
	if len(parts) == 2 {
		return parts[1], parts[0]
	}
	return spec, ""
}

func isURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") ||
		strings.HasPrefix(l, "ftp://") || strings.HasPrefix(l, "ftps://") ||
		strings.HasPrefix(s, `\\`)
}

func appNameFromURL(url string) string {
	base := strings.ReplaceAll(url, "\\", "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.Index(base, "?"); i >= 0 {
		base = base[:i]
	}
	if i := strings.Index(base, "#"); i >= 0 {
		base = base[:i]
	}
	return strings.TrimSuffix(base, ".json")
}

func sanitaryPath(p string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("/\\?:*<>|", r) {
			return -1
		}
		return r
	}, p)
}
