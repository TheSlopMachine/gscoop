// Package doctor extends scoop checkup with contract checks over the
// on-disk state: junction targets, shim trio consistency,
// scoop-install.json readability, bucket repository integrity with pack
// count and size, PATH presence of shim directories, Defender exclusion
// hints, and lock plus orphan .tmp sweep reports.
//
// Reads reuse internal/state for paths, internal/shim for shim targets,
// internal/gitengine for bucket repositories, and internal/install for
// the elevation branch of the Defender hint. The package only reads; it
// never repairs. No emojis.
package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/gitengine"
	"github.com/TheSlopMachine/gscoop/internal/install"
	"github.com/TheSlopMachine/gscoop/internal/shim"
	"github.com/TheSlopMachine/gscoop/internal/state"
)

// Options selects the roots and junction mode for one check run.
// PathEnv overrides the process PATH for tests; empty means the live PATH.
type Options struct {
	Roots      state.Roots
	NoJunction bool
	PathEnv    string
}

// Finding is one check result. Issue findings count as problems;
// informational findings report facts such as pack sizes.
type Finding struct {
	Check   string
	Issue   bool
	Message string
}

// Report is the full result of Check.
type Report struct {
	Findings []Finding
}

// Issues counts findings flagged as problems.
func (r Report) Issues() int {
	n := 0
	for _, f := range r.Findings {
		if f.Issue {
			n++
		}
	}
	return n
}

// SwapBackupSuffix marks the classic trio backups written by the gscoop
// manifest post_install and restored by unswap.
const SwapBackupSuffix = ".classic"

// Check runs every doctor check and returns the combined report.
func Check(opts Options) Report {
	var r Report
	r.Findings = append(r.Findings, checkJunctions(opts)...)
	r.Findings = append(r.Findings, checkShimTrio(opts)...)
	r.Findings = append(r.Findings, checkMetadata(opts)...)
	r.Findings = append(r.Findings, checkBuckets(opts)...)
	r.Findings = append(r.Findings, checkPath(opts)...)
	r.Findings = append(r.Findings, defenderHints(opts)...)
	r.Findings = append(r.Findings, sweepReport(opts)...)
	return r
}

func issue(check, msg string) Finding {
	return Finding{Check: check, Issue: true, Message: msg}
}

func info(check, msg string) Finding {
	return Finding{Check: check, Message: msg}
}

// installedApps lists app directories under one scope, excluding scoop.
func installedApps(appsDir string) []string {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil
	}
	var apps []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "scoop" {
			apps = append(apps, e.Name())
		}
	}
	sort.Strings(apps)
	return apps
}

// versionDirs lists installed version directories: plain directories that
// are not the current link, force-archive markers, or staged .tmp dirs.
func versionDirs(appDir string) []string {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == "current" {
			continue
		}
		if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".new") {
			continue
		}
		if strings.HasPrefix(name, "_") && strings.Contains(name, ".old") {
			continue
		}
		full := filepath.Join(appDir, name)
		if fi, err := os.Stat(full); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, name)
	}
	return out
}

// hasMetadata reports whether a version directory carries install metadata.
func hasMetadata(dir string) bool {
	for _, name := range []string{"scoop-install.json", "install.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// checkJunctions verifies every current link points at an existing version
// directory and flags installed apps with no current link.
func checkJunctions(opts Options) []Finding {
	var out []Finding
	for _, global := range []bool{false, true} {
		appsDir := opts.Roots.Apps(global)
		for _, app := range installedApps(appsDir) {
			link := filepath.Join(opts.Roots.App(app, global), "current")
			fi, err := os.Lstat(link)
			if err != nil {
				if !opts.NoJunction && len(versionDirs(filepath.Join(appsDir, app))) > 0 {
					out = append(out, issue("junction", "App '"+app+"' has no 'current' link."))
				}
				continue
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, err := os.Readlink(link)
			if err != nil {
				out = append(out, issue("junction", "Junction '"+link+"' is unreadable."))
				continue
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Join(appsDir, app), target)
			}
			if _, err := os.Stat(target); err != nil {
				out = append(out, issue("junction", "Junction '"+link+"' points at missing '"+target+"'."))
			}
		}
	}
	return out
}

// checkShimTrio verifies every .shim has a sibling .exe, every .exe has a
// sibling .shim, and every .shim target resolves to a file on disk. It also
// reports the scoop/gscoop swap state.
func checkShimTrio(opts Options) []Finding {
	var out []Finding
	for _, global := range []bool{false, true} {
		dir := opts.Roots.Shims(global)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := map[string]bool{}
		for _, e := range entries {
			if !e.IsDir() {
				names[strings.ToLower(e.Name())] = true
			}
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if !strings.HasSuffix(lower, ".shim") {
				continue
			}
			base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if !names[strings.ToLower(base)+".exe"] {
				out = append(out, issue("shim", "Shim '"+e.Name()+"' is missing '"+base+".exe'."))
			}
			target := shim.GetShimTarget(filepath.Join(dir, e.Name()))
			if target == "" {
				out = append(out, issue("shim", "Shim '"+e.Name()+"' has no path target."))
				continue
			}
			if _, err := os.Stat(target); err != nil {
				out = append(out, issue("shim", "Shim '"+e.Name()+"' points at missing '"+target+"'."))
			}
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if !strings.HasSuffix(lower, ".exe") {
				continue
			}
			base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if strings.HasSuffix(strings.ToLower(base), SwapBackupSuffix) {
				continue
			}
			if !names[strings.ToLower(base)+".shim"] {
				out = append(out, issue("shim", "Shim '"+e.Name()+"' has no '"+base+".shim' descriptor."))
			}
		}
		out = append(out, swapState(dir)...)
	}
	return out
}

// swapState reports whether the scoop shim currently points at gscoop and
// whether classic backups are present.
func swapState(dir string) []Finding {
	var out []Finding
	target := shim.GetShimTarget(filepath.Join(dir, "scoop.shim"))
	if target != "" && strings.Contains(strings.ToLower(target), "gscoop") {
		out = append(out, info("shim", "Active 'scoop' shim points at gscoop ("+target+")."))
	}
	backed := false
	for _, ext := range []string{".exe", ".shim", ".cmd", ".ps1"} {
		if _, err := os.Stat(filepath.Join(dir, "scoop"+SwapBackupSuffix+ext)); err == nil {
			backed = true
			break
		}
	}
	if backed {
		out = append(out, info("shim", "Classic scoop trio is backed up as scoop.classic.* in '"+dir+"'."))
	}
	return out
}

// checkMetadata verifies install metadata parses for every installed
// version directory and flags version dirs without metadata as failed
// install markers.
func checkMetadata(opts Options) []Finding {
	var out []Finding
	for _, global := range []bool{false, true} {
		appsDir := opts.Roots.Apps(global)
		for _, app := range installedApps(appsDir) {
			for _, v := range versionDirs(filepath.Join(appsDir, app)) {
				dir := filepath.Join(appsDir, app, v)
				meta := ""
				for _, name := range []string{"scoop-install.json", "install.json"} {
					if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
						meta = filepath.Join(dir, name)
						break
					}
				}
				if meta == "" {
					out = append(out, issue("metadata", "App '"+app+"' has a failed install marker: version dir '"+v+"' without metadata."))
					continue
				}
				data, err := os.ReadFile(meta)
				if err != nil {
					out = append(out, issue("metadata", "Install metadata for '"+app+" ("+v+")' is unreadable."))
					continue
				}
				var doc map[string]any
				if err := json.Unmarshal(data, &doc); err != nil {
					out = append(out, issue("metadata", "Install metadata for '"+app+" ("+v+")' is not valid JSON."))
				}
			}
		}
	}
	return out
}

// bucketPack describes one bucket repository for the integrity report.
type bucketPack struct {
	packs int64
	bytes int64
}

// packStats sums .git/objects/pack/*.pack count and size.
func packStats(root string) bucketPack {
	var st bucketPack
	entries, err := os.ReadDir(filepath.Join(root, ".git", "objects", "pack"))
	if err != nil {
		return st
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".pack") {
			continue
		}
		st.packs++
		if fi, err := e.Info(); err == nil {
			st.bytes += fi.Size()
		}
	}
	return st
}

// checkBuckets verifies local bucket directories are readable and
// git-backed buckets open as repositories. Pack count and size report as
// informational findings; oversized pack stores suggest delete plus
// re-clone maintenance.
func checkBuckets(opts Options) []Finding {
	var out []Finding
	bucketsDir := filepath.Join(opts.Roots.Scoop, "buckets")
	entries, err := os.ReadDir(bucketsDir)
	if err != nil {
		return out
	}
	engine := gitengine.New(false)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		root := filepath.Join(bucketsDir, name)
		bucketDir := root
		if fi, err := os.Stat(filepath.Join(root, "bucket")); err == nil && fi.IsDir() {
			bucketDir = filepath.Join(root, "bucket")
		}
		if _, err := os.Stat(bucketDir); err != nil {
			out = append(out, issue("bucket", "Bucket '"+name+"' directory is unreadable."))
			continue
		}
		if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
			out = append(out, info("bucket", "Bucket '"+name+"' is a plain directory, not a git repository."))
			continue
		}
		head, err := engine.Head(root)
		if err != nil {
			out = append(out, issue("bucket", "Bucket '"+name+"' has an unreadable git repository."))
			continue
		}
		remote, _ := engine.ConfigGet(root, "remote.origin.url")
		st := packStats(root)
		msg := "Bucket '" + name + "' HEAD " + shortHash(head) + " with " +
			itoa(st.packs) + " packs (" + filesize(st.bytes) + ")"
		if remote != "" {
			msg += " from " + remote
		}
		msg += "."
		out = append(out, info("bucket", msg))
		if st.packs >= 50 || st.bytes >= 500<<20 {
			out = append(out, issue("bucket", "Bucket '"+name+"' pack store is large ("+itoa(st.packs)+" packs, "+filesize(st.bytes)+"); delete plus re-clone compacts it."))
		}
	}
	return out
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func filesize(n int64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	const kb = 1 << 10
	switch {
	case n > gb:
		return fixed(float64(n)/gb) + " GB"
	case n > mb:
		return fixed(float64(n)/mb) + " MB"
	case n > kb:
		return fixed(float64(n)/kb) + " KB"
	default:
		return itoa(n) + " B"
	}
}

func fixed(f float64) string {
	n := int64(f*10+0.5) / 10
	frac := int64(f*10+0.5) % 10
	return itoa(n) + "." + itoa(frac)
}

// checkPath verifies user and global shim directories are on PATH.
func checkPath(opts Options) []Finding {
	var out []Finding
	path := opts.PathEnv
	if path == "" {
		path = os.Getenv("PATH")
	}
	entries := strings.Split(path, string(os.PathListSeparator))
	onPath := func(dir string) bool {
		for _, e := range entries {
			e = strings.TrimSpace(e)
			if runtime.GOOS == "windows" {
				if strings.EqualFold(e, dir) {
					return true
				}
				continue
			}
			if e == dir {
				return true
			}
		}
		return false
	}
	for _, global := range []bool{false, true} {
		dir := opts.Roots.Shims(global)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if !onPath(dir) {
			out = append(out, issue("path", "Shim directory '"+dir+"' is not in PATH."))
		}
	}
	return out
}

// defenderHints reports Defender exclusion guidance. Queries need live
// Defender APIs through powershell, so doctor only prints the remediation
// command; elevation wording follows install.IsAdmin.
func defenderHints(opts Options) []Finding {
	if runtime.GOOS != "windows" {
		return []Finding{info("defender", "Defender checks apply on Windows only.")}
	}
	var out []Finding
	for _, global := range []bool{false, true} {
		base := opts.Roots.Base(global)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		line := "Defender exclusion hint: Add-MpPreference -ExclusionPath '" + base + "'"
		if !install.IsAdmin() {
			line += " (requires elevation)"
		}
		line += "."
		out = append(out, info("defender", line))
	}
	return out
}

// sweepReport flags stale lock files and orphan staged files from
// interrupted operations: *.tmp dirs under apps, *.tmp plus *.new files in
// version dirs and the cache.
func sweepReport(opts Options) []Finding {
	var out []Finding
	for _, global := range []bool{false, true} {
		base := opts.Roots.Base(global)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(base, "scoop.lock")); err == nil {
			out = append(out, info("sweep", "Lock file '"+filepath.Join(base, "scoop.lock")+"' exists; another gscoop process may hold it."))
		}
		appsDir := opts.Roots.Apps(global)
		for _, app := range installedApps(appsDir) {
			appDir := filepath.Join(appsDir, app)
			entries, err := os.ReadDir(appDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				name := e.Name()
				if name == "current" {
					continue
				}
				full := filepath.Join(appDir, name)
				if strings.HasSuffix(name, ".tmp") {
					out = append(out, issue("sweep", "Orphan staged directory '"+full+"' from an interrupted operation."))
					continue
				}
				fi, err := os.Stat(full)
				if err != nil || !fi.IsDir() {
					continue
				}
				staged, err := os.ReadDir(full)
				if err != nil {
					continue
				}
				for _, s := range staged {
					if s.IsDir() {
						continue
					}
					if strings.HasSuffix(s.Name(), ".tmp") || strings.HasSuffix(s.Name(), ".new") {
						out = append(out, issue("sweep", "Orphan staged file '"+filepath.Join(full, s.Name())+"' from an interrupted operation."))
					}
				}
			}
		}
	}
	cache, err := os.ReadDir(opts.Roots.Cache)
	if err == nil {
		for _, e := range cache {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".tmp") {
				out = append(out, issue("sweep", "Orphan staged file '"+filepath.Join(opts.Roots.Cache, e.Name())+"' from an interrupted operation."))
			}
		}
	}
	return out
}
