package search

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gscoop/internal/manifest"
)

func layoutBuckets(t *testing.T, buckets map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for bucket, apps := range buckets {
		dir := filepath.Join(root, "buckets", bucket, "bucket")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for app, body := range apps {
			if err := os.WriteFile(filepath.Join(dir, app+".json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

const toolManifest = `{
    "version": "3.1.0",
    "homepage": "https://example.com",
    "license": "MIT",
    "description": "tooling",
    "url": "https://example.com/tool-3.1.0.zip",
    "bin": ["tool.exe", ["helper.exe", "helpalias"]],
    "shortcuts": [["tool.exe", "Tool App"]]
}`

func TestSearchLocalNamePrecedence(t *testing.T) {
	root := layoutBuckets(t, map[string]map[string]string{"main": {"tool": toolManifest}})
	results, literal, err := SearchLocal(root, []string{"main"}, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if literal {
		t.Error("valid pattern must not use literal fallback")
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].Binaries != "" || results[0].Version != "3.1.0" || results[0].Source != "main" {
		t.Errorf("name match row = %+v", results[0])
	}
}

func TestSearchLocalBinAndAlias(t *testing.T) {
	root := layoutBuckets(t, map[string]map[string]string{"main": {"tool": toolManifest}})
	results, _, err := SearchLocal(root, []string{"main"}, "helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Binaries != "helper.exe" {
		t.Errorf("exe stem match = %+v", results)
	}
	results, _, err = SearchLocal(root, []string{"main"}, "helpalias")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Binaries != "helpalias" {
		t.Errorf("alias match = %+v", results)
	}
}

func TestSearchLocalShortcut(t *testing.T) {
	body := `{"version": "1", "homepage": "https://example.com", "license": "MIT", "shortcuts": [["run.exe", "Fancy Thing"]]}` + "\n"
	root := layoutBuckets(t, map[string]map[string]string{"main": {"other": body}})
	results, _, err := SearchLocal(root, []string{"main"}, "Fancy")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "other" {
		t.Errorf("shortcut match = %+v", results)
	}
}

func TestSearchLocalSkipsInvalidJSON(t *testing.T) {
	root := layoutBuckets(t, map[string]map[string]string{"main": {"broken": "{not json", "tool": toolManifest}})
	results, _, err := SearchLocal(root, []string{"main"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "tool" {
		t.Errorf("invalid JSON skipped, results = %+v", results)
	}
}

func TestSearchLocalEmptyQueryListsAll(t *testing.T) {
	root := layoutBuckets(t, map[string]map[string]string{"main": {"b": toolManifest, "a": toolManifest}})
	results, _, err := SearchLocal(root, []string{"main"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Name != "a" || results[1].Name != "b" {
		t.Errorf("empty query lists all in order, results = %+v", results)
	}
}

func TestSearchLocalLiteralFallback(t *testing.T) {
	root := layoutBuckets(t, map[string]map[string]string{"main": {"tool": toolManifest}})
	_, literal, err := SearchLocal(root, []string{"main"}, "(?<=a)b")
	if err != nil {
		t.Fatal(err)
	}
	if !literal {
		t.Error("lookbehind is unsupported in RE2 and must use literal fallback")
	}
	matcher, fallback := CompileQuery("(?P<named>a)(?P=named)")
	if !fallback {
		t.Error("backreference must use literal fallback")
	}
	if !matcher.MatchString("xx(?P<named>a)(?P=named)yy") {
		t.Error("literal fallback must match substrings case-insensitively")
	}
}

func TestBuildEntryColumns(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "buckets", "main", "bucket")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
    "version": "2.0",
    "homepage": "https://example.com",
    "license": "MIT",
    "description": "demo",
    "bin": ["app.exe", ["server.exe", "serve"]],
    "shortcuts": [["app.exe", "Demo"]],
    "depends": "git",
    "suggest": {"jdk": ["oraclejdk", "openjdk"]}
}`
	path := filepath.Join(dir, "demo.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, ok, err := BuildEntry(path, manifest.Arch64bit)
	if err != nil || !ok {
		t.Fatalf("BuildEntry = %+v, %v", entry, err)
	}
	if entry.Name != "demo" || entry.Bucket != "main" || entry.Version != "2.0" {
		t.Errorf("identity = %+v", entry)
	}
	if entry.Binary != "app | serve" {
		t.Errorf("binary = %q", entry.Binary)
	}
	if entry.Shortcut != "Demo" {
		t.Errorf("shortcut = %q", entry.Shortcut)
	}
	if entry.Dependency != "git" {
		t.Errorf("dependency = %q", entry.Dependency)
	}
	if entry.Suggest != "oraclejdk | openjdk" {
		t.Errorf("suggest = %q", entry.Suggest)
	}
}

func TestDBRoundTrip(t *testing.T) {
	root := t.TempDir()
	db, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	entries := []Entry{
		{Name: "tool", Description: "d", Version: "1.9", Bucket: "main", Manifest: "{}", Binary: "tool", Shortcut: "", Dependency: "", Suggest: ""},
		{Name: "tool", Description: "d", Version: "1.10", Bucket: "main", Manifest: "{}", Binary: "tool", Shortcut: "", Dependency: "", Suggest: ""},
		{Name: "tool", Description: "d", Version: "1.9", Bucket: "extras", Manifest: "{}", Binary: "tool", Shortcut: "", Dependency: "", Suggest: ""},
	}
	if err := Upsert(db, entries); err != nil {
		t.Fatal(err)
	}
	found, err := Find(db, "tool", []string{"name", "binary", "shortcut"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("latest per bucket = %+v", found)
	}
	for _, row := range found {
		if row.Source == "main" && row.Version != "1.10" {
			t.Errorf("main latest = %+v", row)
		}
	}
	got, err := GetRow(db, "tool", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Version != "1.10" {
		t.Errorf("GetRow latest = %+v", got)
	}
	pinned, err := GetRow(db, "tool", "main", "1.9")
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 1 || pinned[0].Version != "1.9" {
		t.Errorf("GetRow pinned = %+v", pinned)
	}
	if err := Remove(db, "main", "tool"); err != nil {
		t.Fatal(err)
	}
	remaining, err := Find(db, "tool", []string{"name"})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Source != "extras" {
		t.Errorf("after remove = %+v", remaining)
	}
	if _, err := Find(db, "tool", []string{"bogus"}); err == nil {
		t.Error("unknown column must error")
	}
}

func TestGitHubTokenPrecedence(t *testing.T) {
	t.Setenv("SCOOP_GH_TOKEN", "scoop-token")
	t.Setenv("GH_TOKEN", "gh-token")
	t.Setenv("GITHUB_TOKEN", "github-token")
	if got := GitHubToken("config-token"); got != "scoop-token" {
		t.Errorf("SCOOP_GH_TOKEN wins, got %q", got)
	}
	t.Setenv("SCOOP_GH_TOKEN", "")
	if got := GitHubToken("config-token"); got != "config-token" {
		t.Errorf("config token second, got %q", got)
	}
}

func TestRemoteSearchAgainstTestServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/rate_limit"):
			fmt.Fprint(w, `{"rate": {"remaining": 0}}`)
		case strings.Contains(r.URL.Path, "/git/trees/"):
			fmt.Fprint(w, `{"tree": [{"path": "bucket/tool.json"}, {"path": "bucket/other.txt"}, {"path": "other/tool.json"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	oldBase := GitHubAPIBase
	GitHubAPIBase = server.URL
	defer func() { GitHubAPIBase = oldBase }()

	reached, err := RateLimitReached(DefaultClient(), "")
	if err != nil || !reached {
		t.Errorf("rate limit = %v, %v", reached, err)
	}
	names, err := SearchRemote(DefaultClient(), "https://github.com/ScoopInstaller/Main.git", "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "tool" {
		t.Errorf("remote names = %v", names)
	}
	remotes := SearchRemotes(DefaultClient(),
		map[string]string{"main": "https://github.com/ScoopInstaller/Main"},
		[]string{"extras"}, "tool", "")
	if len(remotes) != 1 || remotes[0].Source != "main" {
		t.Errorf("remotes = %+v", remotes)
	}
}

// TestCorpusAvailableBuckets parses every manifest in the classic bucket
// directories present on this machine. Parse failures fail the test;
// schema mismatches are reported with a count.
func TestCorpusAvailableBuckets(t *testing.T) {
	roots := []string{}
	for _, candidate := range []string{
		os.Getenv("SCOOP"),
		filepath.Join(os.Getenv("USERPROFILE"), "scoop"),
	} {
		if candidate == "" {
			continue
		}
		dir := filepath.Join(candidate, "buckets")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			roots = append(roots, dir)
		}
	}
	if len(roots) == 0 {
		t.Log("no classic bucket directories present; corpus scan skipped")
		return
	}
	var schema *manifest.Schema
	if loaded, err := manifest.LoadSchema(`C:\devel\Scoop\schema.json`); err == nil {
		schema = loaded
	}
	parsed, parseErrors := 0, []string{}
	nonManifest := 0
	schemaSamples := map[string][]string{}
	schemaGauge := map[string]int{}
	schemaTotal := 0
	for _, dir := range roots {
		_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".vscode":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				parseErrors = append(parseErrors, path+": "+err.Error())
				return nil
			}
			parsedManifest, err := manifest.Parse(data, manifest.AppNameFromURL(path), "", "", path)
			if err != nil {
				if underManifestDir(dir, path) {
					parseErrors = append(parseErrors, path+": "+err.Error())
				} else {
					t.Logf("skip non-manifest JSON: %s: %s", path, firstLine(err.Error()))
				}
				return nil
			}
			if !parsedManifest.Root.Has("version") {
				nonManifest++
				return nil
			}
			parsed++
			if schema != nil && parsedManifest.Root.Has("version") {
				if err := schema.ValidateManifest(parsedManifest); err != nil {
					schemaTotal++
					name := bucketOf(dir, path)
					schemaGauge[name]++
					if len(schemaSamples[name]) < 3 {
						schemaSamples[name] = append(schemaSamples[name], path+": "+headLines(err.Error(), 4))
					}
				}
			}
			return nil
		})
	}
	t.Logf("corpus: %d parsed, %d non-manifest, %d parse errors, %d schema mismatches %v", parsed, nonManifest, len(parseErrors), schemaTotal, schemaGauge)
	for _, name := range sortedKeys(schemaSamples) {
		for _, details := range schemaSamples[name] {
			t.Logf("schema mismatch: %s", details)
		}
	}
	for _, details := range parseErrors {
		t.Errorf("parse error: %s", details)
	}
}

func firstLine(text string) string {
	if index := strings.Index(text, "\n"); index >= 0 {
		return text[:index]
	}
	return text
}

func headLines(text string, max int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > max {
		lines = lines[:max]
	}
	return strings.Join(lines, "\n")
}

func sortedKeys(groups map[string][]string) []string {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// bucketOf returns the bucket name for a path under a buckets directory.
func bucketOf(bucketsDir, path string) string {
	relative, err := filepath.Rel(bucketsDir, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// underManifestDir reports whether path sits in a bucket manifest
// directory: buckets/<name>/bucket/... when that subdir exists, or
// buckets/<name>/... otherwise.
func underManifestDir(bucketsDir, path string) bool {
	relative, err := filepath.Rel(bucketsDir, path)
	if err != nil {
		return false
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) < 2 {
		return false
	}
	bucketRoot := filepath.Join(bucketsDir, parts[0])
	rest := parts[1:]
	if info, err := os.Stat(filepath.Join(bucketRoot, "bucket")); err == nil && info.IsDir() {
		return len(rest) > 0 && rest[0] == "bucket"
	}
	return true
}
