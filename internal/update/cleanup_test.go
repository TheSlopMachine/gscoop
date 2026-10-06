package update

import (
	"os"
	"path/filepath"
	"testing"

	"gscoop/internal/gitengine"
)

func dirtied() gitengine.FileStatus {
	return gitengine.FileStatus{Modified: []string{"config.ps1"}, Dirty: true}
}

func TestStaleVersions(t *testing.T) {
	appDir := t.TempDir()
	for _, v := range []string{"1.0", "2.0"} {
		writeFile(t, filepath.Join(appDir, v, "scoop-install.json"), `{}`)
	}
	// The current link reads as a directory entry named current.
	if err := os.MkdirAll(filepath.Join(appDir, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := StaleVersions(appDir, "2.0")
	if len(got) != 1 || got[0] != "1.0" {
		t.Errorf("StaleVersions = %v, want [1.0]", got)
	}
}

func TestRemoveVersions(t *testing.T) {
	appDir := t.TempDir()
	writeFile(t, filepath.Join(appDir, "1.0", "scoop-manifest.json"), `{}`)
	writeFile(t, filepath.Join(appDir, "2.0", "scoop-manifest.json"), `{}`)
	var unlinked []string
	removed := RemoveVersions(appDir, []string{"1.0"}, func(dir string) {
		unlinked = append(unlinked, dir)
	})
	if len(removed) != 1 || removed[0] != "1.0" {
		t.Errorf("removed = %v, want [1.0]", removed)
	}
	if len(unlinked) != 1 {
		t.Errorf("unlinked = %v, want one persist unlink", unlinked)
	}
	if _, err := os.Stat(filepath.Join(appDir, "1.0")); !os.IsNotExist(err) {
		t.Error("1.0 must be gone")
	}
}

func TestPruneCache(t *testing.T) {
	cache := t.TempDir()
	writeFile(t, filepath.Join(cache, "git#2.0#abc1234.zip"), "new")
	writeFile(t, filepath.Join(cache, "git#1.0#def5678.zip"), "old")
	writeFile(t, filepath.Join(cache, "other#1.0#abc1234.zip"), "other")
	removed := PruneCache(cache, "git", "2.0")
	if len(removed) != 1 {
		t.Errorf("removed = %v, want the 1.0 entry only", removed)
	}
	if _, err := os.Stat(filepath.Join(cache, "git#2.0#abc1234.zip")); err != nil {
		t.Error("current cache entry must survive")
	}
	if _, err := os.Stat(filepath.Join(cache, "other#1.0#abc1234.zip")); err != nil {
		t.Error("other apps must survive")
	}
	// Held install metadata stays readable after cleanup planning.
	held := loadFixture(t, "install-held.json")
	if manifestVersion(held) != "" {
		t.Error("install metadata carries no version field")
	}
}

func TestPruneDownloads(t *testing.T) {
	cache := t.TempDir()
	writeFile(t, filepath.Join(cache, "git#2.0#abc1234.zip.download"), "partial")
	writeFile(t, filepath.Join(cache, "git#2.0#abc1234.zip"), "done")
	removed := PruneDownloads(cache)
	if len(removed) != 1 {
		t.Errorf("removed = %v, want the .download file only", removed)
	}
	if _, err := os.Stat(filepath.Join(cache, "git#2.0#abc1234.zip")); err != nil {
		t.Error("finished cache entry must survive")
	}
}

func TestEnsureMainGit(t *testing.T) {
	root := t.TempDir()
	engine := newFakeEngine()
	// Absent main needs nothing.
	if err := EnsureMainGit(engine, root, "https://example.com/Main.git", nil); err != nil {
		t.Errorf("absent main err = %v", err)
	}
	// Git-backed main needs nothing.
	if err := os.MkdirAll(filepath.Join(root, "main", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMainGit(engine, root, "https://example.com/Main.git", nil); err != nil {
		t.Errorf("git main err = %v", err)
	}
	// Plain main converts through clone.
	if err := os.RemoveAll(filepath.Join(root, "main", ".git")); err != nil {
		t.Fatal(err)
	}
	var emitted []string
	if err := EnsureMainGit(engine, root, "https://example.com/Main.git", func(s string) {
		emitted = append(emitted, s)
	}); err != nil {
		t.Errorf("plain main err = %v", err)
	}
	if len(engine.cloned) != 1 {
		t.Errorf("cloned = %v, want one re-clone", engine.cloned)
	}
	if len(emitted) != 1 {
		t.Errorf("emitted = %v, want the converting notice", emitted)
	}
}

func TestFastForwardClassic(t *testing.T) {
	core := t.TempDir()
	if err := os.MkdirAll(filepath.Join(core, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	engine := newFakeEngine()
	engine.remotes[core] = "https://github.com/ScoopInstaller/Scoop"
	if err := FastForwardClassic(engine, core, "https://github.com/ScoopInstaller/Scoop", "master", false, "", nil); err != nil {
		t.Errorf("clean checkout err = %v", err)
	}
	if len(engine.pulled) != 1 {
		t.Errorf("pulled = %v, want one pull", engine.pulled)
	}
	// Dirty trees abort without autostash.
	engine.status[core] = dirtied()
	if err := FastForwardClassic(engine, core, "https://github.com/ScoopInstaller/Scoop", "master", false, "", nil); err == nil {
		t.Error("dirty checkout without autostash must fail")
	}
	// Autostash backs up through the workspace ladder, then pulls.
	workspace := t.TempDir()
	writeFile(t, filepath.Join(core, "config.ps1"), "local change")
	engine.status[core] = dirtied()
	if err := FastForwardClassic(engine, core, "https://github.com/ScoopInstaller/Scoop", "master", true, workspace, nil); err != nil {
		t.Errorf("autostash err = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ".autostash"))
	if err != nil || len(entries) != 1 {
		t.Errorf("autostash entries = %v,%v, want one backup dir", entries, err)
	}
	// Non-git checkouts report for WARN, never fatal to callers.
	if err := FastForwardClassic(engine, t.TempDir(), "https://github.com/ScoopInstaller/Scoop", "master", false, "", nil); err == nil {
		t.Error("non-git core must report an error")
	}
}
