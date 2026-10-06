// Package manifest implements the Scoop manifest engine: parsing,
// architecture-specific property resolution, app-spec handling, and
// workspace manifest generation.
//
// The JSON model preserves document key order end to end. The ordered
// writer emits the same shape as ConvertToPrettyJson: 4-space indent,
// CRLF line endings, ": " after colons, and single-element arrays
// collapsed to scalars. No map[string]any is used for output.
// No emojis.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Architectures recognized by arch_specific (lib/manifest.ps1:156).
const (
	Arch64bit = "64bit"
	Arch32bit = "32bit"
	ArchARM64 = "arm64"
)

// ArchList is the resolution order used by bucket scans.
var ArchList = []string{Arch64bit, Arch32bit, ArchARM64}

// Object is an ordered JSON object. Keys holds first-seen document order;
// Values holds the decoded value for each key.
type Object struct {
	Keys   []string
	Values map[string]any
}

// Get returns the value for key.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.Values[key]
	return v, ok
}

// Has reports whether key is present.
func (o *Object) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.Values[key]
	return ok
}

// String returns the string value for key, or "" when absent or not a string.
func (o *Object) String(key string) string {
	v, ok := o.Get(key)
	if !ok {
		return ""
	}
	text, _ := v.(string)
	return text
}

// Sub returns the nested object for key, or nil.
func (o *Object) Sub(key string) *Object {
	v, ok := o.Get(key)
	if !ok {
		return nil
	}
	obj, _ := v.(*Object)
	return obj
}

// AsObject converts v to *Object when possible.
func AsObject(v any) *Object {
	obj, _ := v.(*Object)
	return obj
}

// ParseBytes decodes data as JSON, preserving key order. Numbers keep
// their literal form. Trailing data after the top-level value is an error.
func ParseBytes(data []byte) (*Object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected trailing data")
	}
	obj, ok := value.(*Object)
	if !ok {
		return nil, fmt.Errorf("manifest root must be an object")
	}
	return obj, nil
}

// ParseFile reads and decodes the manifest at path.
func ParseFile(path string) (*Object, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(data)
}

func parseValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok := token.(type) {
	case json.Delim:
		switch tok {
		case '{':
			obj := &Object{Values: make(map[string]any)}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key must be a string")
				}
				value, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				if _, exists := obj.Values[key]; !exists {
					obj.Keys = append(obj.Keys, key)
				}
				obj.Values[key] = value
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			items := []any{}
			for dec.More() {
				value, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				items = append(items, value)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return items, nil
		}
		return nil, fmt.Errorf("unexpected delimiter %v", tok)
	default:
		return scalarValue(token)
	}
}

func scalarValue(token json.Token) (any, error) {
	switch v := token.(type) {
	case nil:
		return nil, nil
	case bool:
		return v, nil
	case string:
		return v, nil
	case json.Number:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %v", token)
	}
}

// Manifest is a decoded manifest document with its source identity.
type Manifest struct {
	// Raw holds the source bytes.
	Raw []byte
	// Root is the ordered document tree.
	Root *Object
	// Name is the app name (file base name or URL-derived name).
	Name string
	// Bucket is the owning bucket, or "" for URL/workspace manifests.
	Bucket string
	// SourceURL is the manifest URL for URL installs, or "".
	SourceURL string
	// Path is the file the manifest was read from, or "".
	Path string
}

// Parse builds a Manifest from data with the given identity.
func Parse(data []byte, name, bucket, sourceURL, path string) (*Manifest, error) {
	root, err := ParseBytes(data)
	if err != nil {
		return nil, err
	}
	return &Manifest{Raw: data, Root: root, Name: name, Bucket: bucket, SourceURL: sourceURL, Path: path}, nil
}

// Load reads the manifest file at path with identity name and bucket.
func Load(path, name, bucket string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, name, bucket, "", path)
}

// Version returns the manifest version.
func (m *Manifest) Version() string {
	return m.Root.String("version")
}

// Homepage returns the manifest homepage.
func (m *Manifest) Homepage() string {
	return m.Root.String("homepage")
}

// Description returns the manifest description, or "".
func (m *Manifest) Description() string {
	return m.Root.String("description")
}

// License returns the raw license value (string or object).
func (m *Manifest) License() any {
	v, _ := m.Root.Get("license")
	return v
}

// Depends returns the arch-independent plus arch-specific dependency list.
func (m *Manifest) Depends(arch string) []string {
	out := StringList(propOf(m.Root, "depends"))
	for _, extra := range StringList(m.archRaw("depends", arch)) {
		if !containsFold(out, extra) {
			out = append(out, extra)
		}
	}
	return out
}

// Suggest flattens the suggest map values into one list
// (lib/database.ps1:226-232).
func (m *Manifest) Suggest() []string {
	v, ok := m.Root.Get("suggest")
	if !ok {
		return nil
	}
	obj := AsObject(v)
	if obj == nil {
		return nil
	}
	out := []string{}
	for _, key := range obj.Keys {
		out = append(out, StringList(obj.Values[key])...)
	}
	return out
}

// ArchValue resolves prop for arch, preferring
// architecture.<arch>.<prop> over the top-level prop.
// Mirrors lib/manifest.ps1 arch_specific.
func (m *Manifest) ArchValue(prop, arch string) (any, bool) {
	if v := m.archRaw(prop, arch); v != nil {
		return v, true
	}
	return m.Root.Get(prop)
}

func (m *Manifest) archRaw(prop, arch string) any {
	archRoot := m.Root.Sub("architecture")
	if archRoot == nil {
		return nil
	}
	section := archRoot.Sub(arch)
	if section == nil {
		return nil
	}
	v, ok := section.Get(prop)
	if !ok || v == nil {
		return nil
	}
	return v
}

// URLs returns the arch-specific URL list.
func (m *Manifest) URLs(arch string) []string {
	v, ok := m.ArchValue("url", arch)
	if !ok {
		return nil
	}
	return StringList(v)
}

// Hashes returns the arch-specific hash list.
func (m *Manifest) Hashes(arch string) []string {
	v, ok := m.ArchValue("hash", arch)
	if !ok {
		return nil
	}
	return StringList(v)
}

// ExtractDir returns the arch-specific extract_dir list.
func (m *Manifest) ExtractDir(arch string) []string {
	v, ok := m.ArchValue("extract_dir", arch)
	if !ok {
		return nil
	}
	return StringList(v)
}

// ExtractTo returns the arch-specific extract_to list.
func (m *Manifest) ExtractTo(arch string) []string {
	v, ok := m.ArchValue("extract_to", arch)
	if !ok {
		return nil
	}
	return StringList(v)
}

// Installer returns the arch-specific installer object, or nil.
func (m *Manifest) Installer(arch string) *Object {
	v, ok := m.ArchValue("installer", arch)
	if !ok {
		return nil
	}
	return AsObject(v)
}

// Uninstaller returns the arch-specific uninstaller object, or nil.
func (m *Manifest) Uninstaller(arch string) *Object {
	v, ok := m.ArchValue("uninstaller", arch)
	if !ok {
		return nil
	}
	return AsObject(v)
}

// BinEntry is one bin triple: executable, alias, and args.
// String forms carry only Exe; array forms carry [exe, alias, args]
// with trailing elements optional (lib/install.ps1:171-193).
type BinEntry struct {
	Exe   string
	Alias string
	Args  string
}

// Bins returns the arch-specific bin entries.
func (m *Manifest) Bins(arch string) []BinEntry {
	v, ok := m.ArchValue("bin", arch)
	if !ok {
		return nil
	}
	return BinEntries(v)
}

// BinEntries converts a raw bin value to triples.
func BinEntries(v any) []BinEntry {
	out := []BinEntry{}
	appendEntry := func(item any) {
		switch entry := item.(type) {
		case string:
			out = append(out, BinEntry{Exe: entry})
		case []any:
			parts := make([]string, 0, len(entry))
			for _, p := range entry {
				text, ok := p.(string)
				if !ok {
					return
				}
				parts = append(parts, text)
			}
			if len(parts) == 0 {
				return
			}
			bin := BinEntry{Exe: parts[0]}
			if len(parts) > 1 {
				bin.Alias = parts[1]
			}
			if len(parts) > 2 {
				bin.Args = parts[2]
			}
			out = append(out, bin)
		}
	}
	switch list := v.(type) {
	case string:
		appendEntry(list)
	case []any:
		for _, item := range list {
			appendEntry(item)
		}
	}
	return out
}

// Shortcut is one shortcuts entry: target, display name, args, icon.
// Entries carry 2 to 4 elements (schema.json shortcutsArray).
type Shortcut struct {
	Target string
	Name   string
	Args   string
	Icon   string
}

// Shortcuts returns the arch-specific shortcut entries.
func (m *Manifest) Shortcuts(arch string) []Shortcut {
	v, ok := m.ArchValue("shortcuts", arch)
	if !ok {
		return nil
	}
	return ShortcutEntries(v)
}

// ShortcutEntries converts a raw shortcuts value to entries.
func ShortcutEntries(v any) []Shortcut {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := []Shortcut{}
	for _, item := range list {
		parts, ok := item.([]any)
		if !ok || len(parts) < 2 {
			continue
		}
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			text, ok := p.(string)
			if !ok {
				texts = nil
				break
			}
			texts = append(texts, text)
		}
		if texts == nil || len(texts) < 2 || len(texts) > 4 {
			continue
		}
		entry := Shortcut{Target: texts[0], Name: texts[1]}
		if len(texts) > 2 {
			entry.Args = texts[2]
		}
		if len(texts) > 3 {
			entry.Icon = texts[3]
		}
		out = append(out, entry)
	}
	return out
}

// StringList converts a string-or-array value to a string list.
// Non-string items are skipped.
func StringList(v any) []string {
	switch list := v.(type) {
	case nil:
		return nil
	case string:
		return []string{list}
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func propOf(obj *Object, key string) any {
	if obj == nil {
		return nil
	}
	v, _ := obj.Get(key)
	return v
}

func containsFold(list []string, candidate string) bool {
	for _, item := range list {
		if strings.EqualFold(item, candidate) {
			return true
		}
	}
	return false
}

// SupportedArchitecture selects the architecture for install, mirroring
// lib/manifest.ps1 Get-SupportedArchitecture.
//
// An arm64 request without any arm64 mention in the manifest falls back to
// 64bit on Windows 11 (build >= 22000) and 32bit on older builds.
// windowsBuild carries the OS build number; values below 1 select the
// 64bit fallback. The result is "" when the manifest has no URL for the
// selected architecture, which aborts the install in classic.
func SupportedArchitecture(m *Manifest, requested string, windowsBuild int) string {
	arch := requested
	if arch == ArchARM64 && !mentionsARM64(m.Raw) {
		if windowsBuild >= 22000 {
			arch = Arch64bit
		} else if windowsBuild >= 1 {
			arch = Arch32bit
		} else {
			arch = Arch64bit
		}
	}
	if len(m.URLs(arch)) == 0 {
		return ""
	}
	return arch
}

var arm64Mention = regexp.MustCompile(`['"]arm64['"]`)

func mentionsARM64(raw []byte) bool {
	return arm64Mention.Match(raw)
}

// FormatArchitecture normalizes user-supplied architecture spellings to
// 64bit, 32bit, or arm64. It mirrors lib/core.ps1 Format-ArchitectureString.
func FormatArchitecture(arch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "64bit", "64", "x64", "amd64", "x86_64", "x86-64":
		return Arch64bit, nil
	case "32bit", "32", "x86", "i386", "386", "i686":
		return Arch32bit, nil
	case "arm64", "arm", "aarch64":
		return ArchARM64, nil
	case "":
		return "", fmt.Errorf("empty architecture")
	default:
		return "", fmt.Errorf("invalid architecture: %q", arch)
	}
}

// DefaultArchitecture maps a GOARCH-style runtime architecture to the
// manifest architecture name. It mirrors the OS-derived half of
// lib/core.ps1 Get-DefaultArchitecture; config overrides live in the
// config package.
func DefaultArchitecture(goarch string) string {
	switch strings.ToLower(goarch) {
	case "amd64":
		return Arch64bit
	case "386":
		return Arch32bit
	case "arm64":
		return ArchARM64
	default:
		return Arch64bit
	}
}

// SanitaryPath strips characters illegal in manifest file names.
// Mirrors lib/core.ps1 sanitary_path.
func SanitaryPath(path string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '?', ':', '*', '<', '>', '|':
			return -1
		}
		return r
	}, path)
}

// StripExtension removes the final dot extension, mirroring
// lib/core.ps1 strip_ext.
func StripExtension(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[:index]
	}
	return name
}

var (
	urlPrefix  = regexp.MustCompile(`^(ht|f)tps?://|\\\\`)
	appPattern = regexp.MustCompile(`^(?:([a-zA-Z0-9\-_.]+)/)?(.*\.json|[a-zA-Z0-9\-_.]+)(?:@(.*))?$`)
	jsonSuffix = regexp.MustCompile(`(?s).json$`)
)

// IsURL reports whether spec is a remote URL or UNC path
// (lib/manifest.ps1 Get-Manifest match).
func IsURL(spec string) bool {
	return urlPrefix.MatchString(spec)
}

// AppNameFromURL derives the app name from a manifest URL or path:
// the leaf name without a trailing .json marker
// (lib/core.ps1 appname_from_url).
func AppNameFromURL(rawurl string) string {
	leaf := rawurl
	if index := strings.LastIndexAny(leaf, "/\\"); index >= 0 {
		leaf = leaf[index+1:]
	}
	return jsonSuffix.ReplaceAllString(leaf, "")
}

// Spec is a parsed app specifier: bucket/app@version.
type Spec struct {
	// App is the app name, or the URL/path for URL specs.
	App string
	// Bucket is the explicit bucket, or "".
	Bucket string
	// Version is the requested version, or "".
	Version string
	// URL is true for remote URL or UNC specs.
	URL bool
}

// ParseSpec parses app, bucket/app, app@version, and URL/path specs.
// It mirrors lib/core.ps1 parse_app plus the URL gate in Get-Manifest.
func ParseSpec(spec string) Spec {
	trimmed := strings.TrimLeft(spec, "/")
	if IsURL(trimmed) {
		return Spec{App: trimmed, URL: true}
	}
	parts := appPattern.FindStringSubmatch(trimmed)
	if parts == nil {
		return Spec{App: trimmed}
	}
	return Spec{App: parts[2], Bucket: parts[1], Version: parts[3]}
}

// Buckets exposes bucket directory lookup to spec resolution without
// importing the bucket package.
type Buckets interface {
	// Dir returns the manifest directory for bucket, honoring the
	// bucket/ subdirectory convention.
	Dir(name string) string
	// Local returns local bucket names with known buckets first.
	Local() []string
}

// Resolved is the outcome of spec resolution.
type Resolved struct {
	// App is the plain app name.
	App string
	// Manifest is the decoded manifest.
	Manifest *Manifest
	// Bucket is the owning bucket, or "" for URL/workspace manifests.
	Bucket string
	// SourceURL is the manifest URL for URL installs.
	SourceURL string
	// Path is the manifest file path (bucket or workspace copy).
	Path string
	// Version is the requested version for app@version specs.
	Version string
}

// ResolveOptions configures Resolve.
type ResolveOptions struct {
	// WorkspaceDir receives generated workspace manifests for URL and
	// local-path specs. Empty disables workspace copies.
	WorkspaceDir string
	// FetchURL downloads manifest bytes for URL specs. Nil selects the
	// default HTTP fetch with the Scoop user agent.
	FetchURL func(url string) ([]byte, error)
}

// Resolve loads the manifest for spec, mirroring lib/manifest.ps1
// Get-Manifest for the bucket and URL/path forms.
//
// URL and local-path specs generate a workspace manifest copy under
// WorkspaceDir. Explicit bucket/app specs honor the named bucket.
// Bare app specs scan local buckets in order and keep the first match.
// The installed-manifest fast path in Get-Manifest needs install state
// and stays with the state package.
func Resolve(buckets Buckets, spec string, opts ResolveOptions) (*Resolved, error) {
	parsed := ParseSpec(spec)
	if parsed.URL {
		return resolveURL(parsed.App, opts)
	}
	if isLocalPath(parsed.App) && parsed.Bucket == "" {
		return resolvePath(parsed.App, opts)
	}
	if parsed.Bucket != "" {
		manifest, path, err := loadBucketManifest(buckets, parsed.Bucket, parsed.App)
		if err != nil {
			return nil, err
		}
		return &Resolved{App: parsed.App, Manifest: manifest, Bucket: parsed.Bucket, Path: path, Version: parsed.Version}, nil
	}
	manifest, bucket, path, err := findAppBucket(buckets, parsed.App)
	if err != nil {
		return nil, err
	}
	return &Resolved{App: parsed.App, Manifest: manifest, Bucket: bucket, Path: path, Version: parsed.Version}, nil
}

func resolveURL(rawurl string, opts ResolveOptions) (*Resolved, error) {
	fetch := opts.FetchURL
	if fetch == nil {
		fetch = FetchURLManifest
	}
	data, err := fetch(rawurl)
	if err != nil {
		return nil, err
	}
	app := AppNameFromURL(strings.SplitN(rawurl, "@", 2)[0])
	manifest, err := Parse(data, app, "", rawurl, "")
	if err != nil {
		return nil, err
	}
	path := ""
	if opts.WorkspaceDir != "" {
		path, err = WriteWorkspace(opts.WorkspaceDir, app, data)
		if err != nil {
			return nil, err
		}
		manifest.Path = path
	}
	return &Resolved{App: app, Manifest: manifest, SourceURL: rawurl, Path: path}, nil
}

func resolvePath(path string, opts ResolveOptions) (*Resolved, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	app := AppNameFromURL(path)
	manifest, err := Parse(data, app, "", "", path)
	if err != nil {
		return nil, err
	}
	workspacePath := ""
	if opts.WorkspaceDir != "" {
		workspacePath, err = WriteWorkspace(opts.WorkspaceDir, app, data)
		if err != nil {
			return nil, err
		}
		manifest.Path = workspacePath
	}
	return &Resolved{App: app, Manifest: manifest, Path: firstNonEmpty(workspacePath, path)}, nil
}

func loadBucketManifest(buckets Buckets, bucket, app string) (*Manifest, string, error) {
	path := FindManifestFile(buckets.Dir(bucket), app)
	if path == "" {
		return nil, "", fmt.Errorf("manifest %q not found in bucket %q", app, bucket)
	}
	manifest, err := Load(path, app, bucket)
	if err != nil {
		return nil, "", err
	}
	return manifest, path, nil
}

func findAppBucket(buckets Buckets, app string) (*Manifest, string, string, error) {
	var first *Manifest
	var firstBucket, firstPath string
	for _, name := range buckets.Local() {
		path := FindManifestFile(buckets.Dir(name), app)
		if path == "" {
			continue
		}
		manifest, err := Load(path, app, name)
		if err != nil {
			continue
		}
		if first == nil {
			first, firstBucket, firstPath = manifest, name, path
		}
	}
	if first == nil {
		return nil, "", "", fmt.Errorf("manifest %q not found in local buckets", app)
	}
	return first, firstBucket, firstPath, nil
}

// FindManifestFile locates sanitary(app).json under dir, searching
// recursively. It mirrors lib/manifest.ps1 manifest_path.
func FindManifestFile(dir, app string) string {
	if dir == "" {
		return ""
	}
	target := strings.ToLower(SanitaryPath(app) + ".json")
	var found string
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if entry.IsDir() {
			if strings.EqualFold(entry.Name(), ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(entry.Name()) == target {
			found = path
		}
		return nil
	})
	return found
}

// WriteWorkspace stores manifest bytes as <dir>/<app>.json, creating dir
// as needed. It mirrors Write-ManifestToUserCache.
func WriteWorkspace(dir, app string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, SanitaryPath(app)+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// FetchURLManifest downloads manifest bytes with the Scoop user agent.
func FetchURLManifest(rawurl string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	request, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", UserAgent())
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("manifest URL %q returned status %s", rawurl, response.Status)
	}
	return io.ReadAll(response.Body)
}

// UserAgent mirrors lib/download.ps1 Get-UserAgent with the Go runtime
// in place of the PowerShell host details.
func UserAgent() string {
	return "Scoop/1.0 (+http://scoop.sh/) gscoop/1.0"
}

func isLocalPath(spec string) bool {
	if spec == "" {
		return false
	}
	if IsURL(spec) {
		return false
	}
	info, err := os.Stat(spec)
	return err == nil && !info.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
