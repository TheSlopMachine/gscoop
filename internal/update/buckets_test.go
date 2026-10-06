package update

import (
	"os"
	"path/filepath"
	"testing"

	"gscoop/internal/gitengine"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "update", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSyncBuckets(t *testing.T) {
	root := t.TempDir()
	engine := newFakeEngine()
	for _, name := range []string{"main", "extras"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		engine.heads[filepath.Join(root, name)] = "before-" + name
	}
	// Plain directory without .git reports as skipped.
	if err := os.MkdirAll(filepath.Join(root, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	var emitted []string
	report := SyncBuckets(engine, root, []string{"main", "extras", "local"}, true, func(s string) {
		emitted = append(emitted, s)
	})
	if len(report.Buckets) != 3 {
		t.Fatalf("buckets = %+v, want 3 results", report.Buckets)
	}
	for _, b := range report.Buckets {
		if b.Name == "local" && !b.Skipped {
			t.Error("non-git bucket must report as skipped")
		}
		if (b.Name == "main" || b.Name == "extras") && b.Before == "" {
			t.Errorf("bucket %s must record HEAD", b.Name)
		}
	}
	if len(engine.pulled) != 2 {
		t.Errorf("pulled = %v, want main and extras", engine.pulled)
	}
	if len(report.Errors) != 0 {
		t.Errorf("errors = %v, want none", report.Errors)
	}
	found := false
	for _, line := range emitted {
		if line == "'local' is not a git repository. Skipped." {
			found = true
		}
	}
	if !found {
		t.Errorf("emitted = %v, want skip notice for local", emitted)
	}
}

func TestSyncBucketsErrorIsolated(t *testing.T) {
	root := t.TempDir()
	engine := newFakeEngine()
	for _, name := range []string{"main", "extras"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		engine.heads[filepath.Join(root, name)] = "head"
	}
	engine.pullErr[filepath.Join(root, "extras")] = os.ErrPermission
	report := SyncBuckets(engine, root, []string{"main", "extras"}, false, nil)
	if len(report.Buckets) != 1 || report.Buckets[0].Name != "main" {
		t.Errorf("buckets = %+v, want only main", report.Buckets)
	}
	if _, ok := report.Errors["extras"]; !ok {
		t.Errorf("errors = %v, want extras failure", report.Errors)
	}
}

func TestFindHistoricalManifest(t *testing.T) {
	oldRaw := loadFixture(t, "pinned-1.0.0.json")
	newRaw := loadFixture(t, "pinned-2.0.0.json")
	if manifestVersion(oldRaw) != "1.0.0" || manifestVersion(newRaw) != "2.0.0" {
		t.Fatal("synthetic fixtures must carry their pinned versions")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	engine := newFakeEngine()
	engine.heads[root] = "new-commit"
	engine.files[root] = map[string][]byte{
		"HEAD|bucket/pinned.json":       newRaw,
		"new-commit|bucket/pinned.json": newRaw,
		"old-commit|bucket/pinned.json": oldRaw,
	}
	engine.log[root] = []gitengine.LogEntry{
		{Hash: "new-commit", Message: "pinned: update to 2.0.0"},
		{Hash: "old-commit", Message: "pinned: update to 1.0.0"},
	}
	// HEAD fast path resolves the current version.
	raw, rev, err := FindHistoricalManifest(engine, root, "bucket", "pinned", "2.0.0", true)
	if err != nil || rev == "" || manifestVersion(raw) != "2.0.0" {
		t.Errorf("HEAD resolution = %q,%q,%v, want 2.0.0", raw, rev, err)
	}
	// Older pins resolve through the log plus blob read.
	raw, _, err = FindHistoricalManifest(engine, root, "bucket", "pinned", "1.0.0", true)
	if err != nil || manifestVersion(raw) != "1.0.0" {
		t.Errorf("history resolution = %q,%v, want 1.0.0", raw, err)
	}
	// Disabled history never resolves.
	if _, _, err := FindHistoricalManifest(engine, root, "bucket", "pinned", "1.0.0", false); err != ErrManifestNotFound {
		t.Errorf("disabled history err = %v, want ErrManifestNotFound", err)
	}
	// Unknown versions miss.
	if _, _, err := FindHistoricalManifest(engine, root, "bucket", "pinned", "9.9.9", true); err != ErrManifestNotFound {
		t.Errorf("unknown version err = %v, want ErrManifestNotFound", err)
	}
}

func TestFindHistoricalManifestShallow(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "shallow"), []byte("commits"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsShallowRepo(root) {
		t.Error("shallow marker must detect")
	}
	engine := newFakeEngine()
	engine.heads[root] = "tip"
	engine.files[root] = map[string][]byte{
		"HEAD|bucket/pinned.json": loadFixture(t, "pinned-2.0.0.json"),
	}
	if _, _, err := FindHistoricalManifest(engine, root, "bucket", "pinned", "1.0.0", true); err != ErrShallowHistory {
		t.Errorf("shallow miss err = %v, want ErrShallowHistory", err)
	}
}
