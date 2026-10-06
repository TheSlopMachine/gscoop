// Package gitengine confines git access behind the GitEngine seam
// (plan Appendix B, spec/git-contract.md). go-git is the default;
// git.exe remains as the USE_EXTERNAL_GIT fallback for private-bucket
// credential helpers. Repositories stay ordinary git repositories in
// both directions (phase-0 round-trip spike). No emojis.
package gitengine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CloneOptions mirrors the clone call sites: branch plus single-branch
// plus depth (spec/git-contract.md section 3).
type CloneOptions struct {
	Branch       string
	SingleBranch bool
	Depth        int
}

// PullOptions mirrors pull call sites: force plus tags.
type PullOptions struct {
	Force bool
	Tags  bool
}

// LogEntry is one commit row for LogSince callers.
type LogEntry struct {
	Hash    string
	Author  string
	Date    time.Time
	Message string
}

// DiffEntry is one diff --name-status row.
type DiffEntry struct {
	Status string
	Path   string
}

// FileStatus mirrors Status output: dirty paths plus untracked paths.
type FileStatus struct {
	Modified  []string
	Untracked []string
	Dirty     bool
}

// StashResult records the emulated stash location for dirty trees
// (workspace\.autostash\<timestamp>\ plus restore instructions).
type StashResult struct {
	BackupDir string
	Files     []string
}

// GitEngine is the minimum interface from plan Appendix B.
type GitEngine interface {
	LsRemote(url string) (string, error)
	Clone(url, dir string, opts CloneOptions) error
	Pull(repo string, opts PullOptions) error
	Fetch(repo, refspec string, force bool) error
	CheckoutCreate(repo, branch, track string) error
	ResetHard(repo, rev string) error
	Head(repo string) (string, error)
	ConfigGet(repo, key string) (string, error)
	ConfigSet(repo, key, value string) error
	LogSince(repo, rev string, pathFilter string, invertGrep *regexp.Regexp, limit int) ([]LogEntry, error)
	DiffNameStatus(repo, a, b string) ([]DiffEntry, error)
	ShowFile(repo, rev, path string) ([]byte, error)
	Status(repo string) (FileStatus, error)
	StashLike(repo string) (StashResult, error)
}

// New selects the engine: execgit when useExternal is true, else gogit.
func New(useExternal bool) GitEngine {
	if useExternal {
		return &ExecGit{}
	}
	return &GoGit{}
}

// ConvertRepositoryURI normalizes git URLs for duplicate detection,
// mirroring Convert-RepositoryUri (lib/buckets.ps1:83-102):
// provider/user/repo from ssh, https, and git protocols.
func ConvertRepositoryURI(uri string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(uri, "/"))
	re := regexp.MustCompile(`(?:@|/{1,3})(?:www\.|.*@)?(?P<provider>[^/]+?)(?::\d+)?[:/](?P<user>.+)/(?P<repo>.+?)(?:\.git)?/?$`)
	m := re.FindStringSubmatch(trimmed)
	if m == nil {
		return "", fmt.Errorf("%s is not a valid Git URL", uri)
	}
	names := re.SubexpNames()
	parts := map[string]string{}
	for i, n := range names {
		if i > 0 && n != "" {
			parts[n] = m[i]
		}
	}
	return parts["provider"] + "/" + parts["user"] + "/" + parts["repo"], nil
}

// SameRemote reports whether two URLs normalize equally. Unparsable
// URLs compare by exact string.
func SameRemote(a, b string) bool {
	na, errA := ConvertRepositoryURI(a)
	nb, errB := ConvertRepositoryURI(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return strings.EqualFold(na, nb)
}
