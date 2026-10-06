// Tests for path derivation (lib/core.ps1:366-387, lib/buckets.ps1:1),
// installed-version resolution (lib/versions.ps1:31-101), installed/failed
// (lib/core.ps1:407-430), cache naming (lib/core.ps1:388-403), and hold
// detection (lib/core.ps1:1263-1284). SHA vectors verified against the
// PowerShell oracle. No emojis.
package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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

func TestRootsDerivation(t *testing.T) {
	r := Roots{Scoop: `D:\scoop`, Global: `C:\ProgramData\scoop`, Cache: `D:\scoop\cache`}
	if got := r.Apps(false); got != filepath.Join(`D:\scoop`, "apps") {
		t.Fatalf("apps = %q", got)
	}
	if got := r.Apps(true); got != filepath.Join(`C:\ProgramData\scoop`, "apps") {
		t.Fatalf("global apps = %q", got)
	}
	if got := r.Shims(false); got != filepath.Join(`D:\scoop`, "shims") {
		t.Fatalf("shims = %q", got)
	}
	if got := r.App("git", false); got != filepath.Join(`D:\scoop`, "apps", "git") {
		t.Fatalf("app = %q", got)
	}
	if got := r.Version("git", "2.47.0", false); got != filepath.Join(`D:\scoop`, "apps", "git", "2.47.0") {
		t.Fatalf("version = %q", got)
	}
	if got := r.Current("git", false, false, ""); got != filepath.Join(`D:\scoop`, "apps", "git", "current") {
		t.Fatalf("current = %q", got)
	}
	if got := r.Current("git", false, true, "2.47.0"); got != filepath.Join(`D:\scoop`, "apps", "git", "2.47.0") {
		t.Fatalf("no-junction current = %q", got)
	}
	// App scoop always uses the junction path (lib/core.ps1:374).
	if got := r.Current("scoop", false, true, "1.0"); got != filepath.Join(`D:\scoop`, "apps", "scoop", "current") {
		t.Fatalf("scoop current = %q", got)
	}
	if got := r.Persist("git", false); got != filepath.Join(`D:\scoop`, "persist", "git") {
		t.Fatalf("persist = %q", got)
	}
	if got := r.UserManifest("runat"); got != filepath.Join(`D:\scoop`, "workspace", "runat.json") {
		t.Fatalf("usermanifest = %q", got)
	}
	if got := r.ScoopDB(); got != filepath.Join(`D:\scoop`, "scoop.db") {
		t.Fatalf("scoop.db = %q", got)
	}
}

func TestBucketDirectory(t *testing.T) {
	root := t.TempDir()
	r := Roots{Scoop: root}
	if got := r.Bucket("", false); got != filepath.Join(root, "buckets", "main") {
		t.Fatalf("empty defaults to main: %q", got)
	}
	// A bucket/ subdirectory wins unless root is set (lib/buckets.ps1:22-26).
	if err := os.MkdirAll(filepath.Join(root, "buckets", "extras", "bucket"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := r.Bucket("extras", false); got != filepath.Join(root, "buckets", "extras", "bucket") {
		t.Fatalf("got %q", got)
	}
	if got := r.Bucket("extras", true); got != filepath.Join(root, "buckets", "extras") {
		t.Fatalf("root should skip bucket subdir: %q", got)
	}
}

func TestAppNameStripsBucket(t *testing.T) {
	if got := AppName("extras/git"); got != "git" {
		t.Fatalf("got %q", got)
	}
	if got := AppName(`extras\git`); got != "git" {
		t.Fatalf("got %q", got)
	}
	if got := AppName("git"); got != "git" {
		t.Fatalf("got %q", got)
	}
}

func TestInstalledAppsExcludesScoop(t *testing.T) {
	appsDir := filepath.Join(t.TempDir(), "apps")
	for _, app := range []string{"git", "fd", "scoop", "Scoop", "ripgrep"} {
		if err := os.MkdirAll(filepath.Join(appsDir, app), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(appsDir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []string{"fd", "git", "ripgrep"}
	if got := InstalledApps(appsDir); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if got := InstalledApps(filepath.Join(appsDir, "missing")); len(got) != 0 {
		t.Fatalf("missing dir: %#v", got)
	}
}

// fixtureApp builds an app dir with version dirs holding install metadata
// at controlled modification times.
func fixtureApp(t *testing.T, versions map[string]string) string {
	t.Helper()
	appPath := filepath.Join(t.TempDir(), "apps", "git")
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	i := 0
	for version, meta := range versions {
		dir := filepath.Join(appPath, version)
		path := filepath.Join(dir, meta)
		writeFile(t, path, `{"architecture":"64bit","bucket":"main"}`)
		mtime := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		i++
	}
	return appPath
}

func TestInstalledVersionsOrderAndFilters(t *testing.T) {
	appPath := fixtureApp(t, map[string]string{
		"1.0":          "scoop-install.json",
		"2.0":          "install.json",
		"current":      "scoop-install.json",
		"_1.0.old123":  "scoop-install.json",
		"unversioned":  "notes.txt",
		"UPPER.oldbak": "scoop-install.json",
	})
	// Plain directory without metadata must not count.
	if err := os.MkdirAll(filepath.Join(appPath, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Map iteration is random; assert set membership plus newest-last by
	// hourly mtimes is unstable, so only check exact exclusion rules here.
	got := InstalledVersions(appPath)
	for _, excluded := range []string{"current", "_1.0.old123", "unversioned", "empty"} {
		for _, name := range got {
			if name == excluded {
				t.Fatalf("%q should be excluded from %#v", excluded, got)
			}
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
}

func TestInstalledVersionsOldestToNewest(t *testing.T) {
	appPath := filepath.Join(t.TempDir(), "apps", "git")
	base := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	order := []string{"1.0", "1.1", "2.0"}
	for i, v := range order {
		path := filepath.Join(appPath, v, "scoop-install.json")
		writeFile(t, path, `{}`)
		mtime := base.Add(time.Duration(i) * 24 * time.Hour)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if got := InstalledVersions(appPath); !reflect.DeepEqual(got, order) {
		t.Fatalf("got %#v, want %#v", got, order)
	}
}

func TestSelectCurrentVersionPrefersJunction(t *testing.T) {
	appPath := fixtureApp(t, map[string]string{
		"1.0": "scoop-install.json",
		"2.0": "scoop-install.json",
	})
	// A newer 3.0 install exists, but current/ pins 1.0.
	path := filepath.Join(appPath, "3.0", "scoop-install.json")
	writeFile(t, path, `{}`)
	newTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appPath, "current", "scoop-manifest.json"), `{"version":"1.0"}`)
	got, ok := SelectCurrentVersion(appPath, false)
	if !ok || got != "1.0" {
		t.Fatalf("got %q, %v", got, ok)
	}
	// NO_JUNCTION skips the junction read (lib/versions.ps1:53).
	got, ok = SelectCurrentVersion(appPath, true)
	if !ok || got != "3.0" {
		t.Fatalf("no-junction got %q, %v", got, ok)
	}
}

func TestSelectCurrentVersionLegacyManifest(t *testing.T) {
	appPath := fixtureApp(t, map[string]string{"1.0": "scoop-install.json"})
	writeFile(t, filepath.Join(appPath, "current", "manifest.json"), `{"version":"1.0"}`)
	got, ok := SelectCurrentVersion(appPath, false)
	if !ok || got != "1.0" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSelectCurrentVersionFallsBackToNewest(t *testing.T) {
	appPath := fixtureApp(t, map[string]string{
		"1.0": "install.json",
		"2.0": "scoop-install.json",
	})
	got, ok := SelectCurrentVersion(appPath, false)
	if !ok || got != "2.0" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSelectCurrentVersionMissing(t *testing.T) {
	if _, ok := SelectCurrentVersion(filepath.Join(t.TempDir(), "nope"), false); ok {
		t.Fatal("missing app should report false")
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := SelectCurrentVersion(empty, false); ok {
		t.Fatal("app without versions should report false")
	}
}

func TestInstalledAndFailed(t *testing.T) {
	appPath := fixtureApp(t, map[string]string{"1.0": "scoop-install.json"})
	// Metadata without current linkage still resolves (Select-CurrentVersion
	// falls back to the newest installed version, lib/versions.ps1:61-68),
	// and it is failed for the missing linkage (lib/core.ps1:425-430).
	if !IsInstalled(appPath, false) {
		t.Fatal("metadata resolves even without current linkage")
	}
	if !IsFailed(appPath, false) {
		t.Fatal("version dir without current linkage is failed (lib/core.ps1:425-430)")
	}
	writeFile(t, filepath.Join(appPath, "current", "scoop-manifest.json"), `{"version":"1.0"}`)
	if !IsInstalled(appPath, false) {
		t.Fatal("should be installed with current manifest")
	}
	if IsFailed(appPath, false) {
		t.Fatal("linked app is not failed")
	}
	// Under NO_JUNCTION the app counts as having current; versions decide.
	if !IsInstalled(appPath, true) {
		t.Fatal("no-junction should install via newest version")
	}
	if IsFailed(appPath, true) {
		t.Fatal("no-junction installed app is not failed")
	}
}

func TestCachePathLegacyWins(t *testing.T) {
	cacheDir := t.TempDir()
	url := "https://example.com/dl/app-1.0.zip"
	legacy := filepath.Join(cacheDir, "app#1.0#https_example.com_dl_app-1.0.zip")
	writeFile(t, legacy, "cached")
	if got := CachePath(cacheDir, "app", "1.0", url); got != legacy {
		t.Fatalf("legacy should win: %q", got)
	}
}

func TestCachePathHashed(t *testing.T) {
	cacheDir := t.TempDir()
	cases := []struct {
		app, version, url, want string
	}{
		{"app", "1.0", "https://example.com/dl/app-1.0.zip", "app#1.0#6688255.zip"},
		{"app", "2.0", "https://example.com/get?file=app.exe", "app#2.0#a7310cc.exe"},
		{"git", "2.47.0", "https://github.com/user/repo/releases/download/v2.47.0/git-2.47.0-64-bit.tar.bz2", "git#2.47.0#db81d9b.bz2"},
	}
	for _, c := range cases {
		got := CachePath(cacheDir, c.app, c.version, c.url)
		if got != filepath.Join(cacheDir, c.want) {
			t.Fatalf("CachePath(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestParseInstallInfoHold(t *testing.T) {
	info, err := ParseInstallInfo([]byte(`{"architecture":"64bit","bucket":"main","hold":true}`))
	if err != nil || !info.Hold {
		t.Fatalf("hold true: %#v, %v", info, err)
	}
	info, err = ParseInstallInfo([]byte(`{"hold":"True"}`))
	if err != nil || !info.Hold {
		t.Fatalf("hold string: %#v, %v", info, err)
	}
	docs := []string{"{}", `{"hold":false}`, `{"hold":null}`, `{"hold":"yes"}`}
	for _, doc := range docs {
		info, err = ParseInstallInfo([]byte(doc))
		if err != nil || info.Hold {
			t.Fatalf("doc %s: %#v, %v", doc, info, err)
		}
	}
}

func TestReadInstallInfoFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "1.0")
	writeFile(t, filepath.Join(dir, "install.json"), `{"bucket":"extras"}`)
	info, ok := ReadInstallInfo(dir)
	if !ok || info.Bucket != "extras" {
		t.Fatalf("legacy fallback: %#v, %v", info, ok)
	}
	writeFile(t, filepath.Join(dir, "scoop-install.json"), `{"bucket":"main"}`)
	info, ok = ReadInstallInfo(dir)
	if !ok || info.Bucket != "main" {
		t.Fatalf("current name wins: %#v, %v", info, ok)
	}
}

func TestCoreHoldState(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if held, clear := CoreHoldState("", now); held || clear {
		t.Fatalf("empty: %v %v", held, clear)
	}
	if held, clear := CoreHoldState("2026-10-07", now); !held || clear {
		t.Fatalf("future: %v %v", held, clear)
	}
	if held, clear := CoreHoldState("2026-10-05", now); held || !clear {
		t.Fatalf("expired: %v %v", held, clear)
	}
	if held, clear := CoreHoldState("not-a-date", now); held || !clear {
		t.Fatalf("unparsable clears: %v %v", held, clear)
	}
	future := now.Add(24 * time.Hour).Format(time.RFC3339Nano)
	if held, clear := CoreHoldState(future, now); !held || clear {
		t.Fatalf("o format future: %v %v", held, clear)
	}
}
