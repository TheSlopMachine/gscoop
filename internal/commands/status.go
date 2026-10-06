package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// AppStatus mirrors app_status in lib/core.ps1 for one installed app.
type AppStatus struct {
	Name          string
	Global        bool
	Installed     bool
	Version       string
	LatestVersion string
	Failed        bool
	Hold          bool
	Deprecated    string
	Removed       bool
	Outdated      bool
	MissingDeps   []string
}

// AppStatusFor resolves the status of an installed app: hold state from
// install metadata, deprecation from the bucket deprecated directory,
// latest version from the install bucket manifest, outdated via
// Compare-Version (or inequality under FORCE_UPDATE), and missing
// dependencies against installed apps.
//
// TODO(state,version,manifest,bucket): replace version resolution,
// Compare-Version, manifest lookup, and bucket scans with internal/state,
// internal/version, internal/manifest, and internal/bucket when those seams
// land.
func (e *Env) AppStatusFor(app string, global bool) AppStatus {
	st := AppStatus{Name: app, Global: global}
	g := global
	st.Installed = e.Installed(app, &g)
	st.Version = e.CurrentVersion(app, global)
	info, _ := e.InstallInfoFor(app, st.Version, global)
	if info != nil {
		st.Hold = info.Hold
	}
	st.Failed = e.Failed(app, global)
	st.Deprecated = e.deprecatedManifestPath(app, installBucket(info))
	manifest, _ := e.statusManifest(app, info)
	if manifest == nil {
		st.Removed = true
		st.LatestVersion = st.Version
	} else {
		st.LatestVersion = manifest.Version
		if st.LatestVersion == "" {
			st.LatestVersion = st.Version
		}
	}
	if st.Version != "" && st.LatestVersion != "" {
		cmp := CompareVersions(st.Version, st.LatestVersion)
		if e.ForceUpdate {
			st.Outdated = cmp != 0
		} else {
			st.Outdated = cmp > 0
		}
	}
	if manifest != nil {
		for _, dep := range manifest.DependsList() {
			name := dep
			if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
				name = name[i+1:]
			}
			if name != "" && !e.Installed(name, nil) {
				st.MissingDeps = append(st.MissingDeps, dep)
			}
		}
	}
	return st
}

func installBucket(info *InstallInfo) string {
	if info == nil {
		return ""
	}
	return info.Bucket
}

// deprecatedManifestPath mirrors the deprecated lookup in app_status
// (lib/core.ps1:578-581): the deprecated manifest full path, or "".
func (e *Env) deprecatedManifestPath(app, bucket string) string {
	deprecated := filepath.Join(e.bucketRoot(bucket), "deprecated")
	matches, _ := filepath.Glob(filepath.Join(deprecated, sanitaryPath(app)+".json"))
	if len(matches) > 0 {
		return matches[0]
	}
	found := ""
	_ = filepath.WalkDir(deprecated, func(p string, d os.DirEntry, err error) error {
		if err == nil && found == "" && !d.IsDir() && strings.EqualFold(d.Name(), sanitaryPath(app)+".json") {
			found = p
		}
		return nil
	})
	return found
}

// statusManifest mirrors the manifest resolution in app_status
// (lib/core.ps1:583): the install bucket manifest, a local install path
// manifest, or the first bucket scan hit. Remote install URLs need the
// download layer and resolve to nil (latest stays local).
func (e *Env) statusManifest(app string, info *InstallInfo) (*Manifest, string) {
	if info != nil {
		if info.Bucket != "" {
			if m, _, p := e.bucketManifest(app, info.Bucket); m != nil {
				return m, p
			}
			return nil, ""
		}
		if info.URL != "" {
			if isURL(info.URL) {
				return nil, ""
			}
			if m, raw := e.pathManifest(info.URL); m != nil {
				_ = raw
				return m, info.URL
			}
			return nil, ""
		}
	}
	if hit, _, _ := e.scanBuckets(app); hit != nil {
		return hit.Manifest, hit.Path
	}
	return nil, ""
}

// RunStatus mirrors libexec/scoop-status.ps1: one row per installed app
// that is outdated, failed, deprecated, removed, or missing dependencies.
// Scoop-core and bucket freshness checks need the gitengine network seam
// and are skipped in Phase 1C, so -l/--local changes nothing yet. The
// command always exits 0.
func RunStatus(env *Env, out io.Writer, args []string) int {
	// Only the first argument selects local-only mode
	// (libexec/scoop-status.ps1:16).
	_ = len(args) > 0 && (args[0] == "-l" || args[0] == "--local")
	var rows [][]string
	for _, global := range []bool{true, false} {
		for _, app := range env.InstalledApps(global) {
			st := env.AppStatusFor(app, global)
			if !st.Outdated && !st.Failed && st.Deprecated == "" && !st.Removed && len(st.MissingDeps) == 0 {
				continue
			}
			latest := ""
			if st.Outdated {
				latest = st.LatestVersion
			}
			var info []string
			if st.Failed {
				info = append(info, "Install failed")
			}
			if st.Hold {
				info = append(info, "Held package")
			}
			if st.Deprecated != "" {
				info = append(info, "Deprecated")
			}
			if st.Removed {
				info = append(info, "Manifest removed")
			}
			rows = append(rows, []string{app, st.Version, latest, strings.Join(st.MissingDeps, " | "), strings.Join(info, ", ")})
		}
	}
	if len(rows) == 0 {
		Successf(out, "Everything is ok!")
		return 0
	}
	renderTable(out, []string{"Name", "Installed Version", "Latest Version", "Missing Dependencies", "Info"}, rows)
	return 0
}
