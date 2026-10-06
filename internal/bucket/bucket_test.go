package bucket

import (
	"os"
	"path/filepath"
	"testing"
)

func layoutScoop(t *testing.T, buckets map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for bucket, apps := range buckets {
		dir := filepath.Join(root, "buckets", bucket, "bucket")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for app, ver := range apps {
			content := `{"version": "` + ver + `", "homepage": "https://example.com", "license": "MIT"}` + "\n"
			if err := os.WriteFile(filepath.Join(dir, app+".json"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestManifestDirSubdir(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{"main": {"git": "2.47.0"}})
	if got := ManifestDir(root, "main"); got != filepath.Join(root, "buckets", "main", "bucket") {
		t.Errorf("ManifestDir = %q", got)
	}
	if got := Root(root, ""); got != filepath.Join(root, "buckets", "main") {
		t.Errorf("empty name defaults to main, got %q", got)
	}
	// Bucket without a bucket/ subdir resolves to the root.
	plain := filepath.Join(root, "buckets", "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ManifestDir(root, "plain"); got != plain {
		t.Errorf("plain ManifestDir = %q", got)
	}
}

func TestLocalKnownFirst(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{
		"zeta":  {"a": "1"},
		"main":  {"a": "1"},
		"alpha": {"a": "1"},
	})
	known := []string{"main", "extras"}
	got := Local(root, known)
	if len(got) != 3 || got[0] != "main" {
		t.Errorf("known buckets first, got %v", got)
	}
	if got := Local(filepath.Join(root, "missing"), known); len(got) != 0 {
		t.Errorf("missing buckets dir yields empty, got %v", got)
	}
}

func TestManifestPathRecursive(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{"main": {"git": "2.47.0"}})
	nested := filepath.Join(root, "buckets", "main", "bucket", "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"version": "1", "homepage": "https://example.com", "license": "MIT"}` + "\n"
	if err := os.WriteFile(filepath.Join(nested, "nested.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ManifestPath(root, "nested", "main"); got != filepath.Join(nested, "nested.json") {
		t.Errorf("ManifestPath = %q", got)
	}
	if got := ManifestPath(root, "missing", "main"); got != "" {
		t.Errorf("missing app yields empty, got %q", got)
	}
}

func TestFindAppBucketFirstMatch(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{
		"main":   {"git": "2.47.0"},
		"extras": {"git": "2.48.0"},
	})
	path, bucket := FindAppBucket(root, "git", []string{"main", "extras"})
	if bucket != "main" || path == "" {
		t.Errorf("FindAppBucket = %q, %q", path, bucket)
	}
	if path, bucket := FindAppBucket(root, "missing", nil); path != "" || bucket != "" {
		t.Errorf("missing app yields empty, got %q, %q", path, bucket)
	}
}

func TestLatestManifestSemantic(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{
		"main":   {"tool": "1.9"},
		"extras": {"tool": "1.10"},
	})
	_, bucket, ver, err := LatestManifest(root, "tool", []string{"main", "extras"})
	if err != nil {
		t.Fatal(err)
	}
	if bucket != "extras" || ver != "1.10" {
		t.Errorf("latest = %q %q, want extras 1.10", bucket, ver)
	}
	if _, _, _, err := LatestManifest(root, "missing", nil); err == nil {
		t.Error("missing app should error")
	}
}

func TestAppsInBucket(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{"main": {"b": "1", "a": "1"}})
	apps := AppsInBucket(ManifestDir(root, "main"))
	if len(apps) != 2 || apps[0] != "a" || apps[1] != "b" {
		t.Errorf("AppsInBucket = %v", apps)
	}
}

func TestRegistryFromClassicFile(t *testing.T) {
	registry, err := LoadRegistry(`C:\devel\Scoop\buckets.json`)
	if err != nil {
		t.Skipf("registry unavailable: %v", err)
	}
	if len(registry.Names) == 0 || registry.Names[0] != "main" {
		t.Errorf("registry order = %v", registry.Names)
	}
	if repo := registry.Repo("main"); repo != "https://github.com/ScoopInstaller/Main" {
		t.Errorf("main repo = %q", repo)
	}
	if !registry.Known("extras") || registry.Known("missing") {
		t.Errorf("Known lookup wrong: %+v", registry.Repos)
	}
}

func TestStoreImplementsManifestBuckets(t *testing.T) {
	root := layoutScoop(t, map[string]map[string]string{"main": {"git": "2.47.0"}})
	store := NewStore(root, []string{"main"})
	if got := store.Local(); len(got) != 1 || got[0] != "main" {
		t.Errorf("store Local = %v", got)
	}
	if got := store.Dir("main"); got != ManifestDir(root, "main") {
		t.Errorf("store Dir = %q", got)
	}
}
