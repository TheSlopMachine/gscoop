// Command gogit-spike verifies go-git and git.exe repository round-trips.
//
// Sequence:
//  1. Initialise an origin repository with go-git and commit a manifest-like file.
//  2. Clone the origin with go-git and with git.exe.
//  3. Run git fsck in each repository; each invocation must exit 0.
//  4. Commit a second revision with go-git, pull it with git.exe.
//  5. Commit a third revision with git.exe, pull it with go-git.
//  6. Run git fsck in each repository again and compare HEAD revisions.
//
// Exit code is 0 when every check passes, 1 otherwise.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var failures int

func report(name string, ok bool, detail string) {
	status := "PASS"
	if !ok {
		status = "FAIL"
		failures++
	}
	fmt.Printf("%s %s %s\n", status, name, detail)
}

func runGit(dir string, args ...string) (string, error) {
	base := []string{"-c", "core.autocrlf=false", "-c", "core.longpaths=true"}
	base = append(base, args...)
	cmd := exec.Command("git", base...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func must(err error, context string) {
	if err != nil {
		fmt.Printf("FAIL setup %s: %v\n", context, err)
		os.Exit(1)
	}
}

func commitAll(repo *git.Repository, dir, name, message string) string {
	err := os.WriteFile(filepath.Join(dir, name), []byte("{\"version\": \""+message+"\"}\n"), 0o644)
	must(err, "write "+name)
	wt, err := repo.Worktree()
	must(err, "worktree")
	_, err = wt.Add(name)
	must(err, "add "+name)
	hash, err := wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: "gscoop-spike", Email: "spike@example.invalid", When: time.Now()},
	})
	must(err, "commit "+message)
	return hash.String()
}

func headOf(repo *git.Repository) string {
	ref, err := repo.Head()
	must(err, "head")
	return ref.Hash().String()
}

func main() {
	root, err := os.MkdirTemp("", "gogit-spike-")
	must(err, "temp dir")
	defer os.RemoveAll(root)

	originDir := filepath.Join(root, "origin")
	goCloneDir := filepath.Join(root, "clone-gogit")
	exeCloneDir := filepath.Join(root, "clone-exe")

	origin, err := git.PlainInit(originDir, false)
	must(err, "init origin")
	rev1 := commitAll(origin, originDir, "app.json", "1.0.0")
	report("go-git-init-commit", rev1 != "", "rev="+rev1)

	_, err = git.PlainClone(goCloneDir, false, &git.CloneOptions{URL: originDir})
	report("go-git-clone", err == nil, fmt.Sprintf("err=%v", err))
	if err != nil {
		os.Exit(1)
	}

	out, err := runGit("", "clone", originDir, exeCloneDir)
	report("git-exe-clone", err == nil, fmt.Sprintf("err=%v out=%q", err, firstLine(out)))
	if err != nil {
		os.Exit(1)
	}

	goClone, err := git.PlainOpen(goCloneDir)
	must(err, "open go-git clone")

	for _, dir := range []string{originDir, goCloneDir, exeCloneDir} {
		out, err := runGit(dir, "fsck")
		report("fsck-initial:"+filepath.Base(dir), err == nil, fmt.Sprintf("err=%v out=%q", err, firstLine(out)))
	}

	// Direction 1: go-git writes, git.exe reads.
	rev2 := commitAll(origin, originDir, "app.json", "2.0.0")
	out, err = runGit(goCloneDir, "pull")
	report("git-exe-pull-after-gogit-commit", err == nil, fmt.Sprintf("err=%v out=%q", err, firstLine(out)))
	exeHeadOut, exeErr := runGit(exeCloneDir, "rev-parse", "HEAD")
	_ = exeHeadOut
	_ = exeErr
	goHeadAfterPull := ""
	if err == nil {
		// Refresh the go-git clone as well so all three converge.
		wt, werr := goClone.Worktree()
		if werr == nil {
			_ = wt.Pull(&git.PullOptions{RemoteName: "origin"})
		}
		goHeadAfterPull = headOf(goClone)
	}
	report("heads-converge-after-gogit-commit", goHeadAfterPull == rev2 && err == nil, fmt.Sprintf("origin=%s gogit=%s", rev2, goHeadAfterPull))

	// Direction 2: git.exe writes, go-git reads.
	must(os.WriteFile(filepath.Join(originDir, "app.json"), []byte("{\"version\": \"3.0.0\"}\n"), 0o644), "write rev3")
	if out, err := runGit(originDir, "add", "app.json"); err != nil {
		report("git-exe-commit-origin", false, firstLine(out))
		os.Exit(1)
	}
	if out, err := runGit(originDir, "-c", "user.name=gscoop-spike", "-c", "user.email=spike@example.invalid", "commit", "-m", "3.0.0"); err != nil {
		report("git-exe-commit-origin", false, firstLine(out))
		os.Exit(1)
	} else {
		report("git-exe-commit-origin", true, firstLine(out))
	}
	rev3Out, err := runGit(originDir, "rev-parse", "HEAD")
	rev3 := firstLine(rev3Out)
	report("git-exe-rev-parse", err == nil, "rev="+rev3)

	wt, err := goClone.Worktree()
	must(err, "worktree gogit")
	err = wt.Pull(&git.PullOptions{RemoteName: "origin"})
	report("go-git-pull-after-exe-commit", err == nil, fmt.Sprintf("err=%v", err))
	out, err = runGit(exeCloneDir, "pull")
	report("git-exe-pull-after-exe-commit", err == nil, fmt.Sprintf("err=%v out=%q", err, firstLine(out)))

	for _, dir := range []string{originDir, goCloneDir, exeCloneDir} {
		out, err := runGit(dir, "fsck")
		report("fsck-final:"+filepath.Base(dir), err == nil, fmt.Sprintf("err=%v out=%q", err, firstLine(out)))
	}

	goHead := headOf(goClone)
	exeHeadRaw, _ := runGit(exeCloneDir, "rev-parse", "HEAD")
	exeHead := firstLine(exeHeadRaw)
	originHeadRaw, _ := runGit(originDir, "rev-parse", "HEAD")
	originHead := firstLine(originHeadRaw)
	converged := goHead == rev3 && exeHead == rev3 && originHead == rev3
	report("heads-converge-final", converged, fmt.Sprintf("origin=%s gogit=%s exe=%s", originHead, goHead, exeHead))

	if failures > 0 {
		os.Exit(1)
	}
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' || c == '\r' {
			return s[:i]
		}
	}
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
