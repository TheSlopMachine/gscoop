package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheSlopMachine/gscoop/internal/gitengine"
)

// Dirty buckets pull through the git.exe fallback instead of failing
// with go-git ErrUnstagedChanges. The fallback seam replaces the
// engine pull for dirty trees; an engine pull error must not surface.
func TestSyncBucketsDirtyUsesFallback(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main", "extras"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	engine := newFakeEngine()
	engine.heads[filepath.Join(root, "main")] = "head-main"
	engine.heads[filepath.Join(root, "extras")] = "head-extras"
	engine.status[filepath.Join(root, "extras")] = gitengine.FileStatus{Modified: []string{"app.json"}, Dirty: true}
	engine.pullErr[filepath.Join(root, "extras")] = errors.New("unstaged changes")

	prevAvailable := bucketGitAvailable
	prevPull := bucketFallbackPull
	bucketGitAvailable = func() bool { return true }
	var fallback []string
	bucketFallbackPull = func(dir string) error {
		fallback = append(fallback, dir)
		return nil
	}
	t.Cleanup(func() {
		bucketGitAvailable = prevAvailable
		bucketFallbackPull = prevPull
	})

	report := SyncBuckets(engine, root, []string{"main", "extras"}, false, nil)
	if _, ok := report.Errors["extras"]; ok {
		t.Fatalf("errors = %v, want no error for dirty extras", report.Errors)
	}
	if len(fallback) != 1 || fallback[0] != filepath.Join(root, "extras") {
		t.Fatalf("fallback = %v, want one pull for extras", fallback)
	}
	for _, repo := range engine.pulled {
		if repo == filepath.Join(root, "extras") {
			t.Fatalf("engine pull must not run for dirty extras, pulled = %v", engine.pulled)
		}
	}
	found := false
	for _, repo := range engine.pulled {
		if repo == filepath.Join(root, "main") {
			found = true
		}
	}
	if !found {
		t.Errorf("pulled = %v, want clean main through engine", engine.pulled)
	}
}

// Without git.exe a dirty bucket skips with Skipped set while clean
// siblings still sync through the engine.
func TestSyncBucketsDirtySkipsWithoutGit(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main", "dirty"} {
		if err := os.MkdirAll(filepath.Join(root, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	engine := newFakeEngine()
	engine.heads[filepath.Join(root, "main")] = "head-main"
	engine.heads[filepath.Join(root, "dirty")] = "head-dirty"
	engine.status[filepath.Join(root, "dirty")] = gitengine.FileStatus{Modified: []string{"app.json"}, Dirty: true}

	prevAvailable := bucketGitAvailable
	prevPull := bucketFallbackPull
	bucketGitAvailable = func() bool { return false }
	bucketFallbackPull = func(dir string) error {
		t.Error("fallback must not run without git.exe")
		return nil
	}
	t.Cleanup(func() {
		bucketGitAvailable = prevAvailable
		bucketFallbackPull = prevPull
	})

	var emitted []string
	report := SyncBuckets(engine, root, []string{"main", "dirty"}, false, func(s string) {
		emitted = append(emitted, s)
	})
	if _, ok := report.Errors["dirty"]; ok {
		t.Fatalf("errors = %v, want no error for skipped dirty bucket", report.Errors)
	}
	var skipped *BucketResult
	for i := range report.Buckets {
		if report.Buckets[i].Name == "dirty" {
			skipped = &report.Buckets[i]
		}
	}
	if skipped == nil || !skipped.Skipped {
		t.Fatalf("buckets = %+v, want dirty marked skipped", report.Buckets)
	}
	found := false
	for _, repo := range engine.pulled {
		if repo == filepath.Join(root, "main") {
			found = true
		}
	}
	if !found {
		t.Errorf("pulled = %v, want clean sibling through engine", engine.pulled)
	}
	want := "'dirty' has uncommitted changes and git.exe is unavailable. Skipped."
	seen := false
	for _, line := range emitted {
		if line == want {
			seen = true
		}
	}
	if !seen {
		t.Errorf("emitted = %v, want %q", emitted, want)
	}
}

// Untracked-only trees never block the classic core update, matching
// git diff HEAD --name-only. The dirty gate decides without network:
// a stubbed status plus engine pull proves the decision.
func TestFastForwardClassicUntrackedOnlyProceeds(t *testing.T) {
	core := t.TempDir()
	if err := os.MkdirAll(filepath.Join(core, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	engine := newFakeEngine()
	engine.remotes[core] = "https://github.com/ScoopInstaller/Scoop"
	engine.status[core] = gitengine.FileStatus{Untracked: []string{"notes.txt"}, Dirty: true}
	if err := FastForwardClassic(engine, core, "https://github.com/ScoopInstaller/Scoop", "master", false, "", nil); err != nil {
		t.Fatalf("untracked-only checkout err = %v, want pull to proceed", err)
	}
	if len(engine.pulled) != 1 || engine.pulled[0] != core {
		t.Fatalf("pulled = %v, want one pull for untracked-only core", engine.pulled)
	}
}
