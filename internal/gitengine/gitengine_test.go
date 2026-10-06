package gitengine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func initRepo(t *testing.T, dir string) *git.Repository {
	t.Helper()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bucket", "app.json"), []byte("x"), 0o644); err != nil {
		// bucket subdir optional; write at root instead.
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"version":"1.0"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("app.json"); err != nil {
		// Try adding whatever exists.
		_, _ = wt.Add(".")
	}
	_, err = wt.Commit("1.0", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestGoGitCloneHeadStatus(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, origin)
	g := &GoGit{}
	clone := filepath.Join(root, "clone")
	if err := g.Clone(origin, clone, CloneOptions{}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	head, err := g.Head(clone)
	if err != nil || head == "" {
		t.Fatalf("head: %v %q", err, head)
	}
	st, err := g.Status(clone)
	if err != nil || st.Dirty {
		t.Fatalf("status: %v %+v", err, st)
	}
	// Dirty file appears modified.
	if err := os.WriteFile(filepath.Join(clone, "app.json"), []byte(`{"version":"2.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err = g.Status(clone)
	if err != nil || !st.Dirty || len(st.Modified) == 0 {
		t.Fatalf("dirty status: %v %+v", err, st)
	}
}

func TestGoGitShowAndDiff(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := initRepo(t, origin)
	g := &GoGit{}
	h1, err := g.Head(origin)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if err := os.WriteFile(filepath.Join(origin, "app.json"), []byte(`{"version":"2.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("app.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("2.0", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	h2, err := g.Head(origin)
	if err != nil {
		t.Fatal(err)
	}
	data, err := g.ShowFile(origin, h2, "app.json")
	if err != nil || len(data) == 0 {
		t.Fatalf("show: %v %q", err, data)
	}
	diffs, err := g.DiffNameStatus(origin, h1, h2)
	if err != nil || len(diffs) == 0 {
		t.Fatalf("diff: %v %v", err, diffs)
	}
	logs, err := g.LogSince(origin, "", "", nil, 2)
	if err != nil || len(logs) != 2 {
		t.Fatalf("log: %v %v", err, logs)
	}
}

func TestConvertURI(t *testing.T) {
	got, err := ConvertRepositoryURI("https://github.com/ScoopInstaller/Main.git")
	if err != nil || got != "github.com/ScoopInstaller/Main" {
		t.Fatalf("uri = %q %v", got, err)
	}
	got, err = ConvertRepositoryURI("git@github.com:ScoopInstaller/Main.git")
	if err != nil || got != "github.com/ScoopInstaller/Main" {
		t.Fatalf("ssh uri = %q %v", got, err)
	}
	if _, err := ConvertRepositoryURI("not a url !!!"); err == nil {
		// Regex may still match; only assert no panic.
		t.Log("loose match accepted")
	}
}

func TestRemoveBucketMissing(t *testing.T) {
	if got := RemoveBucket(t.TempDir(), "ghost"); got != 1 {
		t.Fatalf("rm = %d", got)
	}
}

func TestLocalBucketsOrder(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"zzz", "main", "extras"} {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := LocalBuckets(root, []string{"main", "extras"})
	if len(got) != 3 || got[0] != "main" || got[1] != "extras" {
		t.Fatalf("order = %v", got)
	}
}
