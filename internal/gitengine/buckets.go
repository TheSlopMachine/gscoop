package gitengine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AddBucket mirrors add_bucket (lib/buckets.ps1:123-170): duplicate
// names return 2, invalid repos return 1, success returns 0. Failures
// remove the target dir. known maps known names to repo URLs for the
// short form.
func AddBucket(engine GitEngine, bucketsDir, name, repo string, known map[string]string, local []BucketRef) int {
	if repo == "" {
		if r, ok := known[name]; ok {
			repo = r
		} else {
			return 1
		}
	}
	dir := filepath.Join(bucketsDir, name)
	if _, err := os.Stat(dir); err == nil {
		return 2
	}
	uni, err := ConvertRepositoryURI(repo)
	if err != nil {
		return 1
	}
	for _, b := range local {
		if SameRemoteURL(b.Remote, uni) {
			return 2
		}
	}
	if _, err := engine.LsRemote(repo); err != nil {
		return 1
	}
	if err := os.MkdirAll(bucketsDir, 0o755); err != nil {
		return 1
	}
	if err := engine.Clone(repo, dir, CloneOptions{}); err != nil {
		_ = os.RemoveAll(dir)
		return 1
	}
	return 0
}

// BucketRef carries one local bucket remote for duplicate detection.
type BucketRef struct {
	Name   string
	Remote string
}

// SameRemoteURL compares a stored remote against a normalized URI.
func SameRemoteURL(remote, normalized string) bool {
	norm, err := ConvertRepositoryURI(remote)
	if err != nil {
		return false
	}
	return norm == normalized
}

// RemoveBucket mirrors rm_bucket (lib/buckets.ps1:172-186): plain
// directory removal. Missing buckets return 1.
func RemoveBucket(bucketsDir, name string) int {
	dir := filepath.Join(bucketsDir, name)
	if _, err := os.Stat(dir); err != nil {
		return 1
	}
	if err := os.RemoveAll(dir); err != nil {
		return 1
	}
	return 0
}

// LocalBuckets lists bucket dirs with known buckets first, mirroring
// Get-LocalBucket (lib/buckets.ps1:56-75).
func LocalBuckets(bucketsDir string, known []string) []string {
	entries, err := os.ReadDir(bucketsDir)
	if err != nil {
		return []string{}
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	var ordered []string
	seen := map[string]bool{}
	for _, k := range known {
		for _, n := range names {
			if equalFold(n, k) && !seen[n] {
				ordered = append(ordered, n)
				seen[n] = true
			}
		}
	}
	for _, n := range names {
		if !seen[n] {
			ordered = append(ordered, n)
		}
	}
	return ordered
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		// Case-insensitive compare without strings import cycle risk.
		if len(a) == 0 || len(b) == 0 {
			return false
		}
	}
	la, lb := toLower(a), toLower(b)
	return la == lb
}

func toLower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// StashLikeTo emulates stash push -u into workspace/.autostash/<ts>/,
// then hard-resets. It is the documented ladder for the go-git gap.
func StashLikeTo(engine GitEngine, repo, workspace string) (StashResult, error) {
	st, err := engine.Status(repo)
	if err != nil {
		return StashResult{}, err
	}
	files := append(append([]string{}, st.Modified...), st.Untracked...)
	if len(files) == 0 {
		return StashResult{}, nil
	}
	dest := filepath.Join(workspace, ".autostash", time.Now().Format("20060102-150405"))
	for _, f := range files {
		src := filepath.Join(repo, f)
		dst := filepath.Join(dest, f)
		if err := copyPath(src, dst); err != nil {
			return StashResult{}, err
		}
	}
	if err := engine.ResetHard(repo, "HEAD"); err != nil {
		return StashResult{}, fmt.Errorf("stash backup at %s; reset failed: %w", dest, err)
	}
	return StashResult{BackupDir: dest, Files: files}, nil
}

func copyPath(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		// Directories and special files: ensure the dir exists.
		return os.MkdirAll(dst, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
