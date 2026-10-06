package gitengine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// GitAvailable reports whether git.exe resolves from PATH, following
// the exec.LookPath pattern used for external helpers. Bucket sync
// uses it to select the git.exe pull fallback for dirty trees.
func GitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// ExecGit shells to git.exe. It covers private-bucket credential
// helpers and any go-git gap behind USE_EXTERNAL_GIT.
type ExecGit struct {
	// Bin overrides the git binary path. Empty means PATH lookup.
	Bin string
}

func (e *ExecGit) bin() string {
	if e.Bin != "" {
		return e.Bin
	}
	return "git"
}

func (e *ExecGit) run(dir string, args ...string) (string, error) {
	cmd := exec.Command(e.bin(), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out.String())
	}
	return out.String(), nil
}

// LsRemote mirrors git ls-remote <repo>.
func (e *ExecGit) LsRemote(url string) (string, error) {
	out, err := e.run("", "ls-remote", url)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.HasSuffix(fields[1], "HEAD") {
			return fields[0], nil
		}
	}
	if fields := strings.Fields(out); len(fields) >= 1 {
		return fields[0], nil
	}
	return "", fmt.Errorf("ls-remote %q: no refs", url)
}

// Clone mirrors git clone <repo> <dir> -q.
func (e *ExecGit) Clone(url, dir string, opts CloneOptions) error {
	args := []string{"clone", url, dir, "-q"}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
		if opts.SingleBranch {
			args = append(args, "--single-branch")
		}
	}
	if opts.Depth > 0 {
		args = append(args, "--depth", fmt.Sprint(opts.Depth))
	}
	_, err := e.run("", args...)
	return err
}

// Pull mirrors git pull -q with tags and force.
func (e *ExecGit) Pull(repo string, opts PullOptions) error {
	args := []string{"pull", "-q"}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.Tags {
		args = append(args, "--tags")
	}
	_, err := e.run(repo, args...)
	return err
}

// Fetch mirrors git fetch --force origin <refspec> -q.
func (e *ExecGit) Fetch(repo, refspec string, force bool) error {
	args := []string{"fetch"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "origin", refspec, "-q")
	_, err := e.run(repo, args...)
	return err
}

// CheckoutCreate mirrors git checkout -B <b> -t origin/<b> -q.
func (e *ExecGit) CheckoutCreate(repo, branch, track string) error {
	_, err := e.run(repo, "checkout", "-B", branch, "-t", track, "-q")
	return err
}

// ResetHard mirrors git reset --hard <rev> -q.
func (e *ExecGit) ResetHard(repo, rev string) error {
	_, err := e.run(repo, "reset", "--hard", rev, "-q")
	return err
}

// Head mirrors git rev-parse HEAD.
func (e *ExecGit) Head(repo string) (string, error) {
	out, err := e.run(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ConfigGet mirrors git config --get <key>.
func (e *ExecGit) ConfigGet(repo, key string) (string, error) {
	out, err := e.run(repo, "config", "--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ConfigSet mirrors git config <key> <value>.
func (e *ExecGit) ConfigSet(repo, key, value string) error {
	_, err := e.run(repo, "config", key, value)
	return err
}

// LogSince mirrors log with grep and path filters.
func (e *ExecGit) LogSince(repo, rev string, pathFilter string, invertGrep *regexp.Regexp, limit int) ([]LogEntry, error) {
	args := []string{"log", "--format=%H%x00%an%x00%aI%x00%s"}
	if rev != "" {
		args = append(args, rev)
	}
	if limit > 0 {
		args = append(args, "-n", fmt.Sprint(limit))
	}
	if pathFilter != "" {
		args = append(args, "--", pathFilter)
	}
	out, err := e.run(repo, args...)
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			continue
		}
		if invertGrep != nil && invertGrep.MatchString(parts[3]) {
			continue
		}
		entries = append(entries, LogEntry{Hash: parts[0], Author: parts[1], Message: parts[3]})
	}
	return entries, nil
}

// DiffNameStatus mirrors git diff --name-status.
func (e *ExecGit) DiffNameStatus(repo, a, b string) ([]DiffEntry, error) {
	out, err := e.run(repo, "diff", "--name-status", a, b)
	if err != nil {
		return nil, err
	}
	var entries []DiffEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		entries = append(entries, DiffEntry{Status: parts[0], Path: parts[1]})
	}
	return entries, nil
}

// ShowFile mirrors git show <rev>:<path>.
func (e *ExecGit) ShowFile(repo, rev, path string) ([]byte, error) {
	cmd := exec.Command(e.bin(), "show", rev+":"+path)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("show %s:%s: %w", rev, path, err)
	}
	return out, nil
}

// Status mirrors git diff HEAD --name-only plus untracked files.
func (e *ExecGit) Status(repo string) (FileStatus, error) {
	var st FileStatus
	out, err := e.run(repo, "status", "--porcelain")
	if err != nil {
		return st, err
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.HasPrefix(line, "??") {
			st.Untracked = append(st.Untracked, path)
		} else {
			st.Modified = append(st.Modified, path)
		}
	}
	st.Dirty = len(st.Modified)+len(st.Untracked) > 0
	return st, nil
}

// StashLike mirrors git stash push -m <msg> -u -q through git.exe.
func (e *ExecGit) StashLike(repo string) (StashResult, error) {
	if _, err := e.run(repo, "stash", "push", "-u", "-q"); err != nil {
		return StashResult{}, err
	}
	return StashResult{}, nil
}

var _ = os.Getenv
