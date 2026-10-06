package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gscoop/internal/gitengine"
)

// ErrShallowHistory reports that version-pinned resolution needs full
// history while the bucket is shallow. SHALLOW_BUCKETS trades
// app@version installs for clone cost (plan section 8.2).
var ErrShallowHistory = errors.New("bucket history is shallow; app@version needs full history (unset SHALLOW_BUCKETS and re-clone)")

// ErrManifestNotFound reports that no commit carries the pinned version.
var ErrManifestNotFound = errors.New("manifest version not found in bucket history")

// FindHistoricalManifest resolves app@version from bucket git history,
// mirroring Find-HistoricalManifestInGit (lib/manifest.ps1:210-274).
// bucketRoot is the bucket clone root, app the manifest name, version
// the pin, relDir the manifests subdirectory (bucket or root).
// It returns the manifest bytes plus the commit hash. The HEAD fast
// path runs first; then the pickaxe log locates the version commit and
// both the commit and its parent are version-checked. useGitHistory
// mirrors USE_GIT_HISTORY (default true); false skips history.
func FindHistoricalManifest(engine gitengine.GitEngine, bucketRoot, relDir, app, version string, useGitHistory bool) ([]byte, string, error) {
	if !useGitHistory {
		return nil, "", ErrManifestNotFound
	}
	if _, err := os.Stat(filepath.Join(bucketRoot, ".git")); err != nil {
		return nil, "", ErrManifestNotFound
	}
	rel := manifestRelPath(relDir, app)
	if head, err := engine.Head(bucketRoot); err == nil && head != "" {
		if raw, err := engine.ShowFile(bucketRoot, "HEAD", rel); err == nil {
			if manifestVersion(raw) == version {
				return raw, head, nil
			}
		}
	}
	shallow := IsShallowRepo(bucketRoot)
	// Pickaxe: log commits touching the manifest path, newest first.
	entries, err := engine.LogSince(bucketRoot, "", rel, nil, 0)
	if err != nil {
		return nil, "", err
	}
	pickaxe := versionPickaxe(version)
	for _, e := range entries {
		raw, err := engine.ShowFile(bucketRoot, e.Hash, rel)
		if err != nil || !pickaxe.Match(raw) {
			continue
		}
		if manifestVersion(raw) == version {
			return raw, e.Hash, nil
		}
	}
	if shallow {
		return nil, "", ErrShallowHistory
	}
	return nil, "", ErrManifestNotFound
}

// manifestRelPath builds the git-relative manifest path for lookups.
func manifestRelPath(relDir, app string) string {
	rel := strings.Trim(filepath.ToSlash(filepath.Join(relDir, app+".json")), "/")
	return rel
}

// manifestVersion parses the version field without a manifest engine.
func manifestVersion(raw []byte) string {
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return doc.Version
}

// versionPickaxe matches the version assignment line, mirroring the
// -G 'version.: .<version>' plus -S literal fallback
// (lib/manifest.ps1:245-258). Matching runs in Go over blob bytes.
func versionPickaxe(version string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(version)
	return regexp.MustCompile(`(?m)^\s*"version"\s*:\s*".{0,8}` + quoted + `"`)
}

// IsShallowRepo reports whether the clone carries a .git/shallow file,
// which means USE_GIT_HISTORY pins beyond HEAD cannot resolve.
func IsShallowRepo(repo string) bool {
	fi, err := os.Stat(filepath.Join(repo, ".git", "shallow"))
	return err == nil && !fi.IsDir()
}
