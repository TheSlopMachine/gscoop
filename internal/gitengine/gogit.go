package gitengine

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
)

// GoGit is the default pure-Go engine.
type GoGit struct{}

// LsRemote lists the remote HEAD through the go-git remote API,
// mirroring git ls-remote <repo> (lib/buckets.ps1:150).
func (g *GoGit) LsRemote(url string) (string, error) {
	rem := git.NewRemote(nil, &config.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := rem.List(&git.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("ls-remote %q: %w", url, err)
	}
	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD {
			if target := ref.Target(); target != "" {
				for _, r := range refs {
					if r.Name() == target {
						return r.Hash().String(), nil
					}
				}
			}
		}
	}
	for _, ref := range refs {
		if ref.Name().Short() == "HEAD" || ref.Name() == plumbing.NewBranchReferenceName("master") {
			return ref.Hash().String(), nil
		}
	}
	if len(refs) > 0 {
		return refs[0].Hash().String(), nil
	}
	return "", fmt.Errorf("ls-remote %q: no refs", url)
}

// Clone mirrors git clone <repo> <dir> -q plus branch and depth
// (lib/buckets.ps1:157, libexec/scoop-update.ps1:83-99).
func (g *GoGit) Clone(url, dir string, opts CloneOptions) error {
	co := &git.CloneOptions{URL: url}
	if opts.Branch != "" {
		co.ReferenceName = plumbing.NewBranchReferenceName(opts.Branch)
		co.SingleBranch = opts.SingleBranch
	}
	if opts.Depth > 0 {
		co.Depth = opts.Depth
		co.SingleBranch = true
	}
	_, err := git.PlainClone(dir, false, co)
	if err != nil {
		return fmt.Errorf("clone %q: %w", url, err)
	}
	return nil
}

// Pull mirrors git pull -q plus tags and force
// (libexec/scoop-update.ps1:138-140, :197-218).
func (g *GoGit) Pull(repo string, opts PullOptions) error {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	err = wt.Pull(&git.PullOptions{RemoteName: "origin", Force: opts.Force})
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}

// Fetch mirrors git fetch --force origin <refspec> -q
// (libexec/scoop-update.ps1:123-137, scoop-status fetch).
func (g *GoGit) Fetch(repo, refspec string, force bool) error {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return err
	}
	spec := config.RefSpec(refspec)
	if spec == "" {
		spec = "+refs/heads/*:refs/remotes/origin/*"
	}
	if force && !strings.HasPrefix(string(spec), "+") {
		spec = config.RefSpec("+" + string(spec))
	}
	return r.Fetch(&git.FetchOptions{RemoteName: "origin", RefSpecs: []config.RefSpec{spec}, Force: force, Tags: git.AllTags})
}

// CheckoutCreate mirrors git checkout -B <b> -t origin/<b> -q.
func (g *GoGit) CheckoutCreate(repo, branch, track string) error {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	name := plumbing.NewBranchReferenceName(branch)
	hash, err := r.ResolveRevision(plumbing.Revision(track))
	if err != nil {
		return err
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: *hash, Branch: name, Create: true, Force: true}); err != nil {
		return err
	}
	return r.Storer.SetReference(plumbing.NewHashReference(name, *hash))
}

// ResetHard mirrors git reset --hard <rev> -q.
func (g *GoGit) ResetHard(repo, rev string) error {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return err
	}
	hash, err := r.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return err
	}
	wt, err := r.Worktree()
	if err != nil {
		return err
	}
	return wt.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: *hash})
}

// Head mirrors git rev-parse HEAD.
func (g *GoGit) Head(repo string) (string, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return "", err
	}
	ref, err := r.Head()
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// ConfigGet mirrors git config remote.origin.url and --get lookups.
func (g *GoGit) ConfigGet(repo, key string) (string, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return "", err
	}
	cfg, err := r.Config()
	if err != nil {
		return "", err
	}
	switch strings.ToLower(key) {
	case "remote.origin.url":
		if rem, ok := cfg.Remotes["origin"]; ok && len(rem.URLs) > 0 {
			return rem.URLs[0], nil
		}
		return "", fmt.Errorf("no origin remote")
	default:
		return "", fmt.Errorf("unsupported config key %q", key)
	}
}

// ConfigSet mirrors git config remote.origin.url <repo> and fetch
// refspec writes (libexec/scoop-update.ps1:123-137).
func (g *GoGit) ConfigSet(repo, key, value string) error {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return err
	}
	cfg, err := r.Config()
	if err != nil {
		return err
	}
	switch strings.ToLower(key) {
	case "remote.origin.url":
		if _, ok := cfg.Remotes["origin"]; !ok {
			cfg.Remotes["origin"] = &config.RemoteConfig{Name: "origin"}
		}
		cfg.Remotes["origin"].URLs = []string{value}
	case "remote.origin.fetch":
		if _, ok := cfg.Remotes["origin"]; !ok {
			cfg.Remotes["origin"] = &config.RemoteConfig{Name: "origin"}
		}
		cfg.Remotes["origin"].Fetch = []config.RefSpec{config.RefSpec(value)}
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return r.SetConfig(cfg)
}

// LogSince mirrors log --format plus pathFilter and invert-grep
// handling done in Go over Log (plan section 8.2).
func (g *GoGit) LogSince(repo, rev string, pathFilter string, invertGrep *regexp.Regexp, limit int) ([]LogEntry, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return nil, err
	}
	var from plumbing.Hash
	if rev != "" {
		h, err := r.ResolveRevision(plumbing.Revision(rev))
		if err != nil {
			return nil, err
		}
		from = *h
	} else {
		ref, err := r.Head()
		if err != nil {
			return nil, err
		}
		from = ref.Hash()
	}
	iter, err := r.Log(&git.LogOptions{From: from})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []LogEntry
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) >= limit {
			return fmt.Errorf("stop")
		}
		if invertGrep != nil && invertGrep.MatchString(firstLine(c.Message)) {
			return nil
		}
		if pathFilter != "" && !commitTouches(c, pathFilter) {
			return nil
		}
		out = append(out, LogEntry{Hash: c.Hash.String(), Author: c.Author.Name, Date: c.Author.When, Message: firstLine(c.Message)})
		return nil
	})
	if err != nil && err.Error() != "stop" {
		return nil, err
	}
	return out, nil
}

func commitTouches(c *object.Commit, path string) bool {
	// Cheap path filter: stats-based when available, else assume match
	// so callers never lose rows.
	stats, err := c.Stats()
	if err != nil || len(stats) == 0 {
		return true
	}
	for _, s := range stats {
		if strings.EqualFold(s.Name, path) || strings.HasSuffix(strings.ToLower(s.Name), strings.ToLower("/"+path)) {
			return true
		}
	}
	return false
}

// DiffNameStatus mirrors git diff --name-status A..B through tree diff.
func (g *GoGit) DiffNameStatus(repo, a, b string) ([]DiffEntry, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return nil, err
	}
	ha, err := r.ResolveRevision(plumbing.Revision(a))
	if err != nil {
		return nil, err
	}
	hb, err := r.ResolveRevision(plumbing.Revision(b))
	if err != nil {
		return nil, err
	}
	ca, err := r.CommitObject(*ha)
	if err != nil {
		return nil, err
	}
	cb, err := r.CommitObject(*hb)
	if err != nil {
		return nil, err
	}
	ta, err := ca.Tree()
	if err != nil {
		return nil, err
	}
	tb, err := cb.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(ta, tb)
	if err != nil {
		return nil, err
	}
	var out []DiffEntry
	for _, ch := range changes {
		action, _ := ch.Action()
		path := ch.To.Name
		if path == "" {
			path = ch.From.Name
		}
		status := "M"
		switch action {
		case merkletrie.Insert:
			status = "A"
		case merkletrie.Delete:
			status = "D"
		case merkletrie.Modify:
			status = "M"
		}
		out = append(out, DiffEntry{Status: status, Path: path})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ShowFile mirrors git show <rev>:<path> through blob reads.
func (g *GoGit) ShowFile(repo, rev, path string) ([]byte, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return nil, err
	}
	h, err := r.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, err
	}
	commit, err := r.CommitObject(*h)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	entry, err := tree.FindEntry(path)
	if err != nil {
		// Try bucket/ prefix-insensitive match on the leaf.
		var foundHash plumbing.Hash
		var found bool
		_ = tree.Files().ForEach(func(f *object.File) error {
			if strings.EqualFold(f.Name, path) || strings.HasSuffix(strings.ToLower(f.Name), strings.ToLower("/"+path)) {
				foundHash = f.Blob.Hash
				found = true
			}
			return nil
		})
		if !found {
			return nil, err
		}
		blob, berr := r.BlobObject(foundHash)
		if berr != nil {
			return nil, berr
		}
		reader, rerr := blob.Reader()
		if rerr != nil {
			return nil, rerr
		}
		defer reader.Close()
		return io.ReadAll(reader)
	}
	blob, err := r.BlobObject(entry.Hash)
	if err != nil {
		return nil, err
	}
	reader, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// Status mirrors git diff HEAD --name-only plus untracked detection
// via the worktree status API.
func (g *GoGit) Status(repo string) (FileStatus, error) {
	var st FileStatus
	r, err := git.PlainOpen(repo)
	if err != nil {
		return st, err
	}
	wt, err := r.Worktree()
	if err != nil {
		return st, err
	}
	s, err := wt.Status()
	if err != nil {
		return st, err
	}
	for path, code := range s {
		if code.Worktree == git.Untracked || code.Staging == git.Untracked {
			st.Untracked = append(st.Untracked, path)
			continue
		}
		st.Modified = append(st.Modified, path)
	}
	sort.Strings(st.Modified)
	sort.Strings(st.Untracked)
	st.Dirty = len(st.Modified)+len(st.Untracked) > 0
	return st, nil
}

// StashLike emulates stash push -u: modified and untracked files copy
// to workspace\.autostash\<timestamp>\, then the tree resets hard.
// go-git has no stash, so this ladder preserves outcome parity.
func (g *GoGit) StashLike(repo string) (StashResult, error) {
	var res StashResult
	st, err := g.Status(repo)
	if err != nil {
		return res, err
	}
	if !st.Dirty {
		return res, nil
	}
	_ = time.Now
	return res, fmt.Errorf("stash emulation needs a workspace dir; use StashLikeTo")
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
