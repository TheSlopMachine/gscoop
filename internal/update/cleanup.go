package update

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StaleVersions lists version directories eligible for removal: every
// directory under appDir except currentVersion and the current link.
// Held state lives on the current version metadata, so older versions
// always qualify, matching scoop-cleanup.ps1:36-41.
func StaleVersions(appDir, currentVersion string) []string {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == "current" || name == currentVersion {
			continue
		}
		if !e.IsDir() {
			// Junction targets read as directories on Windows; plain
			// files never qualify.
			if !isLinkDir(filepath.Join(appDir, name)) {
				continue
			}
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func isLinkDir(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.IsDir() || fi.Mode()&os.ModeSymlink != 0
}

// RemoveVersions deletes version dirs after unlinking their persist
// links through unlink. It returns the removed names in order.
func RemoveVersions(appDir string, versions []string, unlink func(dir string)) []string {
	var removed []string
	for _, v := range versions {
		dir := filepath.Join(appDir, v)
		if unlink != nil {
			unlink(dir)
		}
		if err := os.RemoveAll(dir); err == nil {
			removed = append(removed, v)
		}
	}
	return removed
}

// PruneEmptyCurrent drops a dangling current link left behind when the
// last version goes away, then removes the app dir when nothing
// remains (scoop-cleanup.ps1:52-60). It reports whether the app dir
// itself was removed.
func PruneEmptyCurrent(appDir string) bool {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return false
	}
	if len(entries) == 1 && entries[0].Name() == "current" {
		link := filepath.Join(appDir, "current")
		if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(link)
		} else if err == nil && fi.IsDir() {
			// Junction or plain dir named current with no versions:
			// classic clears read-only and removes it.
			_ = os.Chmod(link, 0o755)
			_ = os.Remove(link)
		}
	}
	entries, err = os.ReadDir(appDir)
	if err != nil || len(entries) != 0 {
		return false
	}
	return os.Remove(appDir) == nil
}

// CacheCandidates lists cache files for app, oldest first by mtime.
func CacheCandidates(cacheDir, app string) []string {
	pattern := app + "#*"
	matches, err := filepath.Glob(filepath.Join(cacheDir, pattern))
	if err != nil {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		fi, errI := os.Stat(matches[i])
		fj, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] < matches[j]
		}
		return fi.ModTime().Before(fj.ModTime())
	})
	return matches
}

// PruneCache removes cache entries for app except the current
// version files (scoop-cleanup.ps1:32-34). keep is the exact infix
// app#current#; files carrying it stay. It returns removed paths.
func PruneCache(cacheDir, app, current string) []string {
	var removed []string
	keep := app + "#" + current + "#"
	for _, p := range CacheCandidates(cacheDir, app) {
		if current != "" && strings.Contains(filepath.Base(p), keep) {
			continue
		}
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed
}

// PruneDownloads removes stale *.download in-flight files
// (scoop-cleanup.ps1:79-81). It returns removed paths.
func PruneDownloads(cacheDir string) []string {
	matches, _ := filepath.Glob(filepath.Join(cacheDir, "*.download"))
	var removed []string
	for _, p := range matches {
		if err := os.Remove(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed
}
