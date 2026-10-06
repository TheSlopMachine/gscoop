package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gscoop/internal/deps"
	"gscoop/internal/hook"
)

type mapFetch struct {
	views map[string]*ManifestView
}

func (m *mapFetch) Depends(app, _ string) ([]string, error) {
	if v, ok := m.views[app]; ok {
		return v.Depends, nil
	}
	return nil, nil
}
func (m *mapFetch) Helpers(_, _ string) []string { return nil }
func (m *mapFetch) Exists(app string) bool {
	_, ok := m.views[app]
	return ok
}
func (m *mapFetch) BucketOf(_ string) string { return "" }

type memDownloader struct {
	files map[string][]string
}

func (m *memDownloader) Download(_ context.Context, op Op, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	names := m.files[op.App]
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("payload"), 0o644); err != nil {
			return nil, err
		}
	}
	return names, nil
}

type memExtractor struct{}

func (memExtractor) Extract(file, destDir string) error { return nil }

func testEnv(t *testing.T) Env {
	t.Helper()
	root := t.TempDir()
	return Env{ScoopDir: filepath.Join(root, "scoop"), GlobalDir: filepath.Join(root, "global")}
}

func TestPlanOrder(t *testing.T) {
	fetch := &mapFetch{views: map[string]*ManifestView{
		"app":  {Version: "1.0", Depends: []string{"lib"}},
		"lib":  {Version: "2.0"},
		"base": {Version: "3.0"},
	}}
	fetch.views["app"].Depends = []string{"lib"}
	lookup := func(app, arch string) (*ManifestView, error) {
		return fetch.views[app], nil
	}
	tx, err := Plan([]string{"app"}, "64bit", lookup, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Ops) != 2 || tx.Ops[len(tx.Ops)-1].App != "app" {
		t.Fatalf("plan = %+v", tx)
	}
}

func TestPlanNightly(t *testing.T) {
	fetch := &mapFetch{views: map[string]*ManifestView{"n": {Version: "nightly"}}}
	lookup := func(app, arch string) (*ManifestView, error) { return fetch.views[app], nil }
	tx, err := Plan([]string{"n"}, "64bit", lookup, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Ops) != 1 || tx.Ops[0].Version == "nightly" {
		t.Fatalf("nightly not dated: %+v", tx)
	}
}

func TestInstallCommitAtomic(t *testing.T) {
	env := testEnv(t)
	views := map[string]*ManifestView{
		"demo": {Version: "1.0", Bins: nil, ManifestRaw: []byte(`{"version":"1.0"}`)},
	}
	x := &Executor{
		Env:        env,
		Downloader: &memDownloader{files: map[string][]string{"demo": {"app.zip"}}},
		Extractor:  memExtractor{},
		Hooks:      &hook.Runner{Dir: t.TempDir(), Out: discard{}, Err: discard{}},
		LookupManifest: func(app, arch string) (*ManifestView, error) {
			return views[app], nil
		},
	}
	tx := Transaction{Ops: []Op{{App: "demo", Version: "1.0", Architecture: "64bit"}}}
	if err := x.Install(context.Background(), tx); err != nil {
		t.Fatalf("install: %v", err)
	}
	final := env.VersionDir("demo", "1.0", false)
	for _, f := range []string{"scoop-manifest.json", "scoop-install.json"} {
		if _, err := os.Stat(filepath.Join(final, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	if entries, _ := os.ReadDir(env.AppDir("demo", false)); len(entries) == 0 {
		t.Fatal("no version dir")
	}
	// No .tmp remnants.
	for _, e := range mustReadDir(t, env.AppDir("demo", false)) {
		if len(e) > 4 && e[len(e)-4:] == ".tmp" {
			t.Fatalf("staging remains: %s", e)
		}
	}
}

func TestSweepOrphanTmp(t *testing.T) {
	env := testEnv(t)
	appPath := env.AppDir("demo", false)
	if err := os.MkdirAll(filepath.Join(appPath, "1.0.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	removed, _ := Sweep(filepath.Join(env.ScoopDir, "apps"), nil)
	if len(removed) != 1 {
		t.Fatalf("sweep = %v", removed)
	}
}

func TestRunningGuard(t *testing.T) {
	if err := CheckRunning(t.TempDir(), false, func(string) []string { return []string{`C:\apps\demo\app.exe`} }); err == nil {
		t.Fatal("want running error")
	}
	if err := CheckRunning(t.TempDir(), true, func(string) []string { return []string{"x"} }); err != nil {
		t.Fatal(err)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "ver")
	persist := filepath.Join(root, "persist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := []PersistView{{Source: "data.txt"}}
	if err := PersistData(entries, dir, persist, nil); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(persist, "data.txt")); err != nil {
		t.Fatalf("persist target: %v", err)
	}
	UnlinkPersistData(entries, dir)
}

func TestFindDirOrSubdir(t *testing.T) {
	fixed, removed := FindDirOrSubdir(`C:\a;C:\apps\demo;D:\b`, `C:\apps\demo`)
	if fixed != `C:\a;D:\b` || len(removed) != 1 {
		t.Fatalf("fixed=%q removed=%v", fixed, removed)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

var _ = deps.Resolve
