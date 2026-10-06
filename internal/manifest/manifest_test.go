package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleManifest = `{
    "version": "1.2.3",
    "homepage": "https://example.com",
    "license": "MIT",
    "description": "sample",
    "url": "https://example.com/top-1.2.3.zip",
    "hash": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "bin": "top.exe",
    "architecture": {
        "64bit": {
            "url": "https://example.com/app-1.2.3-x64.zip",
            "bin": [
                "app.exe",
                ["tool.exe", "toolalias", "--serve"]
            ]
        },
        "32bit": {
            "url": "https://example.com/app-1.2.3-x86.zip"
        }
    },
    "shortcuts": [
        ["app.exe", "Sample App"]
    ]
}`

func parseSample(t *testing.T) *Manifest {
	t.Helper()
	m, err := Parse([]byte(sampleManifest), "sample", "main", "", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return m
}

func TestParsePreservesOrder(t *testing.T) {
	obj, err := ParseBytes([]byte(sampleManifest))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	want := []string{"version", "homepage", "license", "description", "url", "hash", "bin", "architecture", "shortcuts"}
	if len(obj.Keys) != len(want) {
		t.Fatalf("keys = %v, want %v", obj.Keys, want)
	}
	for i := range want {
		if obj.Keys[i] != want[i] {
			t.Fatalf("keys = %v, want %v", obj.Keys, want)
		}
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	if _, err := ParseBytes([]byte(`{"version":"1"} trailing`)); err == nil {
		t.Error("expected error for trailing data")
	}
	if _, err := ParseBytes([]byte(`[1,2]`)); err == nil {
		t.Error("expected error for non-object root")
	}
}

func TestArchValuePrecedence(t *testing.T) {
	m := parseSample(t)
	v, ok := m.ArchValue("url", Arch64bit)
	if !ok {
		t.Fatal("arch url missing")
	}
	if urls := StringList(v); len(urls) != 1 || urls[0] != "https://example.com/app-1.2.3-x64.zip" {
		t.Errorf("arch url = %v", urls)
	}
	v, ok = m.ArchValue("hash", Arch64bit)
	if !ok {
		t.Fatal("hash should fall back to top level")
	}
	if hashes := StringList(v); len(hashes) != 1 {
		t.Errorf("fallback hash = %v", hashes)
	}
	if _, ok := m.ArchValue("url", "mips"); !ok {
		t.Error("unknown arch falls back to the top-level url in classic")
	}
}

func TestURLsHashes(t *testing.T) {
	m := parseSample(t)
	if urls := m.URLs(Arch32bit); len(urls) != 1 || !strings.HasSuffix(urls[0], "x86.zip") {
		t.Errorf("32bit urls = %v", urls)
	}
	if hashes := m.Hashes(Arch32bit); len(hashes) != 1 {
		t.Errorf("32bit hashes = %v", hashes)
	}
}

func TestBinEntries(t *testing.T) {
	m := parseSample(t)
	if bins := m.Bins(Arch32bit); len(bins) != 1 || bins[0].Exe != "top.exe" {
		t.Errorf("fallback bins = %+v", bins)
	}
	bins := m.Bins(Arch64bit)
	if len(bins) != 2 {
		t.Fatalf("64bit bins = %+v", bins)
	}
	if bins[0].Exe != "app.exe" || bins[0].Alias != "" {
		t.Errorf("string bin = %+v", bins[0])
	}
	if bins[1].Exe != "tool.exe" || bins[1].Alias != "toolalias" || bins[1].Args != "--serve" {
		t.Errorf("triple bin = %+v", bins[1])
	}
}

func TestShortcutEntries(t *testing.T) {
	m := parseSample(t)
	shortcuts := m.Shortcuts(Arch64bit)
	if len(shortcuts) != 1 {
		t.Fatalf("shortcuts = %+v", shortcuts)
	}
	if shortcuts[0].Target != "app.exe" || shortcuts[0].Name != "Sample App" {
		t.Errorf("shortcut = %+v", shortcuts[0])
	}
}

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in   string
		want Spec
	}{
		{"git", Spec{App: "git"}},
		{"extras/git", Spec{App: "git", Bucket: "extras"}},
		{"git@2.47.0", Spec{App: "git", Version: "2.47.0"}},
		{"extras/git@2.47.0", Spec{App: "git", Bucket: "extras", Version: "2.47.0"}},
		{"https://example.com/app.json", Spec{App: "https://example.com/app.json", URL: true}},
		{`\\server\share\app.json`, Spec{App: `\\server\share\app.json`, URL: true}},
	}
	for _, c := range cases {
		if got := ParseSpec(c.in); got != c.want {
			t.Errorf("ParseSpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSanitaryPath(t *testing.T) {
	if got := SanitaryPath("a/b\\c:d*e?f<g>h|i"); got != "abcdefghi" {
		t.Errorf("SanitaryPath = %q", got)
	}
}

func TestAppNameFromURL(t *testing.T) {
	if got := AppNameFromURL("https://example.com/bucket/app.json"); got != "app" {
		t.Errorf("AppNameFromURL = %q", got)
	}
	if got := AppNameFromURL(`C:\manifests\tool.json`); got != "tool" {
		t.Errorf("AppNameFromURL path = %q", got)
	}
}

func TestSupportedArchitecture(t *testing.T) {
	m := parseSample(t)
	if got := SupportedArchitecture(m, Arch64bit, 0); got != Arch64bit {
		t.Errorf("64bit = %q", got)
	}
	// Classic falls back to the top-level url for unknown architectures,
	// so a manifest without any url is needed to exercise the empty path.
	urlLess, err := Parse([]byte(`{"version": "1", "homepage": "https://example.com", "license": "MIT"}`), "u", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := SupportedArchitecture(urlLess, "mips", 0); got != "" {
		t.Errorf("missing url should yield empty, got %q", got)
	}
	armManifest, err := Parse([]byte(`{"version":"1","url":"https://example.com/a.zip"}`), "a", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := SupportedArchitecture(armManifest, ArchARM64, 22000); got != Arch64bit {
		t.Errorf("arm64 fallback on new build = %q", got)
	}
	if got := SupportedArchitecture(urlLess, ArchARM64, 19045); got != "" {
		t.Errorf("arm64 fallback without any url yields empty, got %q", got)
	}
}

func TestFormatArchitecture(t *testing.T) {
	for in, want := range map[string]string{"x64": Arch64bit, "32": Arch32bit, "aarch64": ArchARM64, "64bit": Arch64bit} {
		got, err := FormatArchitecture(in)
		if err != nil || got != want {
			t.Errorf("FormatArchitecture(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := FormatArchitecture("mips"); err == nil {
		t.Error("expected error for unknown architecture")
	}
}

func TestMarshalShape(t *testing.T) {
	m := parseSample(t)
	out := string(MarshalManifest(m))
	if !strings.Contains(out, "\r\n") {
		t.Error("output must use CRLF")
	}
	if !strings.Contains(out, `"version": "1.2.3"`) {
		t.Error("output must separate colon with a space")
	}
	if !strings.HasPrefix(out, "{\r\n") {
		t.Error("output must open with brace and CRLF")
	}
	lines := strings.Split(out, "\r\n")
	if lines[1] != `    "version": "1.2.3",` {
		t.Errorf("first field line = %q", lines[1])
	}
	reparsed, err := ParseBytes([]byte(strings.ReplaceAll(out, "\r\n", "\n")))
	if err != nil {
		t.Fatalf("output must reparse: %v", err)
	}
	if reparsed.Keys[0] != "version" {
		t.Errorf("field order changed: %v", reparsed.Keys)
	}
}

func TestMarshalCollapsesSingleArrays(t *testing.T) {
	obj, err := ParseBytes([]byte(`{"version":"1","url":["https://example.com/a.zip"],"bin":[["a.exe","alias"]]}`))
	if err != nil {
		t.Fatal(err)
	}
	out := string(Marshal(obj))
	if !strings.Contains(out, `"url": "https://example.com/a.zip"`) {
		t.Errorf("single url array must collapse:\n%s", out)
	}
}

func TestFindManifestFile(t *testing.T) {
	root := t.TempDir()
	bucketDir := filepath.Join(root, "buckets", "main", "bucket")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bucketDir, "git.json")
	if err := os.WriteFile(target, []byte(sampleManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "buckets", "main", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindManifestFile(filepath.Join(root, "buckets", "main", "bucket"), "git"); got != target {
		t.Errorf("FindManifestFile = %q, want %q", got, target)
	}
	if got := FindManifestFile(filepath.Join(root, "buckets", "main", "bucket"), "missing"); got != "" {
		t.Errorf("missing app should yield empty, got %q", got)
	}
}

type stubBuckets struct {
	dirs  map[string]string
	order []string
}

func (s stubBuckets) Dir(name string) string { return s.dirs[name] }
func (s stubBuckets) Local() []string        { return s.order }

func writeBucketManifest(t *testing.T, dir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"version": "` + version + `", "homepage": "https://example.com", "license": "MIT"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveBucketApp(t *testing.T) {
	root := t.TempDir()
	mainDir := filepath.Join(root, "main")
	writeBucketManifest(t, mainDir, "git", "2.47.0")
	buckets := stubBuckets{dirs: map[string]string{"main": mainDir}, order: []string{"main"}}
	resolved, err := Resolve(buckets, "main/git", ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Bucket != "main" || resolved.Manifest.Version() != "2.47.0" {
		t.Errorf("resolved = %+v", resolved)
	}
}

func TestResolveBareAppScansBuckets(t *testing.T) {
	root := t.TempDir()
	mainDir := filepath.Join(root, "main")
	extraDir := filepath.Join(root, "extras")
	writeBucketManifest(t, mainDir, "git", "2.47.0")
	writeBucketManifest(t, extraDir, "git", "2.48.0")
	buckets := stubBuckets{dirs: map[string]string{"main": mainDir, "extras": extraDir}, order: []string{"main", "extras"}}
	resolved, err := Resolve(buckets, "git", ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Bucket != "main" {
		t.Errorf("first match wins, bucket = %q", resolved.Bucket)
	}
}

func TestResolvePathGeneratesWorkspace(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "custom.json")
	if err := os.WriteFile(source, []byte(sampleManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	buckets := stubBuckets{}
	resolved, err := Resolve(buckets, source, ResolveOptions{WorkspaceDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.App != "custom" {
		t.Errorf("app = %q", resolved.App)
	}
	if _, err := os.Stat(filepath.Join(workspace, "custom.json")); err != nil {
		t.Errorf("workspace copy missing: %v", err)
	}
}

func TestValidateAgainstSchema(t *testing.T) {
	schema, err := LoadSchema(`C:\devel\Scoop\schema.json`)
	if err != nil {
		t.Skipf("schema unavailable: %v", err)
	}
	if err := schema.ValidateManifest(parseSample(t)); err != nil {
		t.Errorf("sample should validate: %v", err)
	}
	bad := []struct {
		name string
		doc  string
	}{
		{"missing required", `{"version": "1"}`},
		{"bad version pattern", `{"version": "a b", "homepage": "https://example.com", "license": "MIT"}`},
		{"bad hash", `{"version": "1", "homepage": "https://example.com", "license": "MIT", "hash": "zzz"}`},
		{"unknown property", `{"version": "1", "homepage": "https://example.com", "license": "MIT", "frobnicate": true}`},
	}
	for _, c := range bad {
		m, err := Parse([]byte(c.doc), "bad", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.ValidateManifest(m); err == nil {
			t.Errorf("%s: expected validation error", c.name)
		}
	}
	dollar, err := Parse([]byte(`{"version": "1", "homepage": "https://example.com", "license": "MIT", "url": "https://example.com/$version/a.zip"}`), "bad", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.ValidateManifest(dollar); err == nil {
		t.Error("plain url with $ variables should fail")
	}
}
