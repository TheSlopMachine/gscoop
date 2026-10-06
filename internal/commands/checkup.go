package commands

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	git "github.com/go-git/go-git/v5"
)

// RunCheckup mirrors libexec/scoop-checkup.ps1: main-bucket and helper
// presence plus doctor-style contract checks over junctions, shims,
// install metadata, buckets, and PATH. Findings print as WARN lines; the
// command always exits 0. Windows Defender, long-paths, developer-mode,
// and NTFS checks need registry and service APIs and are deferred.
func RunCheckup(env *Env, out io.Writer, _ []string) int {
	issues := 0
	if !hasBucket(env, "main") {
		Warnf(out, "Main bucket is not added.")
		Successf(out, "  run 'scoop bucket add main'")
		issues++
	}
	// TODO(config): gate the 7zip check on USE_EXTERNAL_7ZIP through
	// internal/config when that seam lands.
	if !env.Installed("7zip", nil) {
		Warnf(out, "'7-Zip' is not installed! It's required for unpacking most programs. Please Run 'scoop install 7zip'.")
		issues++
	}
	if !env.Installed("innounp", nil) {
		Warnf(out, "'Inno Setup Unpacker' is not installed! It's required for unpacking InnoSetup files. Please run 'scoop install innounp'.")
		issues++
	}
	if !env.Installed("dark", nil) {
		Warnf(out, "'dark' is not installed! It's required for unpacking installers created with the WiX Toolset. Please run 'scoop install dark' or 'scoop install wixtoolset'.")
		issues++
	}
	issues += checkJunctions(env, out)
	issues += checkShims(env, out)
	issues += checkMetadata(env, out)
	issues += checkBuckets(env, out)
	issues += checkPath(env, out)
	if issues > 0 {
		Warnf(out, "Found %d potential %s.", issues, pluralize(issues, "problem", "problems"))
	} else {
		Successf(out, "No problems identified!")
	}
	return 0
}

func hasBucket(env *Env, name string) bool {
	for _, b := range env.ListBucketNames() {
		if strings.EqualFold(b, name) {
			return true
		}
	}
	return false
}

// checkJunctions verifies every current link points at an existing
// version directory and flags installed apps with no current link.
//
// TODO(junction): validate reparse-point internals through
// internal/junction when it lands; Lstat plus target existence covers the
// read-only contract here.
func checkJunctions(env *Env, out io.Writer) int {
	issues := 0
	for _, global := range []bool{false, true} {
		for _, app := range env.InstalledApps(global) {
			link := filepath.Join(env.AppDir(app, global), "current")
			fi, err := os.Lstat(link)
			if err != nil {
				if !env.NoJunction && len(env.InstalledVersions(app, global)) > 0 {
					Warnf(out, "App '%s' has no 'current' link.", app)
					issues++
				}
				continue
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(link)
			if err != nil {
				Warnf(out, "Junction '%s' is unreadable.", link)
				issues++
				continue
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(env.AppDir(app, global), target)
			}
			if _, err := os.Stat(target); err != nil {
				Warnf(out, "Junction '%s' points at missing '%s'.", link, target)
				issues++
			}
		}
	}
	return issues
}

// checkShims verifies shim directories exist and every .shim target
// resolves to a file on disk.
func checkShims(env *Env, out io.Writer) int {
	issues := 0
	for _, global := range []bool{false, true} {
		dir := env.ShimDir(global)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".shim") {
				continue
			}
			target := shimTarget(filepath.Join(dir, entry.Name()))
			if target == "" {
				Warnf(out, "Shim '%s' has no path target.", entry.Name())
				issues++
				continue
			}
			if _, err := os.Stat(target); err != nil {
				Warnf(out, "Shim '%s' points at missing '%s'.", entry.Name(), target)
				issues++
			}
		}
	}
	return issues
}

// checkMetadata verifies install metadata parses for every installed
// version directory.
func checkMetadata(env *Env, out io.Writer) int {
	issues := 0
	for _, global := range []bool{false, true} {
		for _, app := range env.InstalledApps(global) {
			vers := env.InstalledVersions(app, global)
			if len(vers) == 0 && env.Failed(app, global) {
				Warnf(out, "App '%s' failed to install.", app)
				issues++
				continue
			}
			for _, v := range vers {
				if info, _ := env.InstallInfoFor(app, v, global); info == nil {
					Warnf(out, "Install metadata for '%s (%s)' is unreadable.", app, v)
					issues++
				}
			}
		}
	}
	return issues
}

// checkBuckets verifies local bucket directories are readable and
// git-backed buckets open as repositories.
//
// TODO(gitengine): read remotes and log dates through the gitengine seam
// when it lands.
func checkBuckets(env *Env, out io.Writer) int {
	issues := 0
	for _, name := range env.ListBucketNames() {
		root := filepath.Join(env.BucketsDir(), name)
		if _, err := os.Stat(env.FindBucketDirectory(name)); err != nil {
			Warnf(out, "Bucket '%s' directory is unreadable.", name)
			issues++
			continue
		}
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			if _, err := git.PlainOpen(root); err != nil {
				Warnf(out, "Bucket '%s' has an unreadable git repository.", name)
				issues++
			}
		}
	}
	return issues
}

// checkPath verifies user and global shim directories are on PATH.
func checkPath(env *Env, out io.Writer) int {
	issues := 0
	entries := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	onPath := func(dir string) bool {
		for _, e := range entries {
			if runtime.GOOS == "windows" {
				if strings.EqualFold(strings.TrimSpace(e), dir) {
					return true
				}
				continue
			}
			if strings.TrimSpace(e) == dir {
				return true
			}
		}
		return false
	}
	for _, global := range []bool{false, true} {
		dir := env.ShimDir(global)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if !onPath(dir) {
			Warnf(out, "Shim directory '%s' is not in PATH.", friendlyPath(dir))
			issues++
		}
	}
	return issues
}
