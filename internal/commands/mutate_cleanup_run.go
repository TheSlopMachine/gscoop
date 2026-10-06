// Cleanup command wiring for Phase 3A (internal/commands/mutate_cleanup_*.go).
//
// RunCleanup mirrors libexec/scoop-cleanup.ps1: old versions are
// removed keeping current, the dangling current link and empty app
// dir are removed next, and cache pruning drops stale artifacts plus
// in-flight downloads. Persist links unlink before recursive removal.
// No emojis.
package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/install"
	"github.com/TheSlopMachine/gscoop/internal/update"
)

// RunCleanup mirrors libexec/scoop-cleanup.ps1: -a/--all,
// -g/--global, -k/--cache.
func RunCleanup(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "agk", []string{"all", "global", "cache"})
	if r.Err != "" {
		Errorf(out, "scoop cleanup: %s", r.Err)
		return 1
	}
	global := r.Has("g") || r.Has("global")
	pruneCache := r.Has("k") || r.Has("cache")
	all := r.Has("a") || r.Has("all")
	apps := r.Rest
	if len(apps) == 0 && !all {
		Errorf(out, "<app> missing")
		printUsage(out, "cleanup")
		return 1
	}
	if global && !install.IsAdmin() {
		Errorf(out, "you need admin rights to cleanup global apps")
		return 1
	}
	var tuples [][2]any
	verbose := true
	if (len(apps) == 1 && apps[0] == "*") || all {
		verbose = false
		for _, app := range env.InstalledApps(false) {
			tuples = append(tuples, [2]any{app, false})
		}
		if global {
			for _, app := range env.InstalledApps(true) {
				tuples = append(tuples, [2]any{app, true})
			}
		}
	} else {
		var ok bool
		tuples, ok = confirmInstalled(env, out, apps, global)
		if !ok {
			return 1
		}
	}
	for _, t := range tuples {
		cleanupOne(env, out, t[0].(string), t[1].(bool), verbose, pruneCache)
	}
	if pruneCache {
		for _, p := range update.PruneDownloads(env.CacheDir) {
			_ = p
		}
	}
	if !verbose {
		Successf(out, "Everything is shiny now!")
	}
	return 0
}

// cleanupOne removes old versions of one app, keeping current.
// Cache pruning keeps current-version artifacts only.
func cleanupOne(env *Env, out io.Writer, app string, global, verbose, pruneCache bool) {
	current := env.CurrentVersion(app, global)
	if pruneCache {
		for _, p := range update.PruneCache(env.CacheDir, app, current) {
			_ = p
		}
	}
	appDir := env.AppDir(app, global)
	versions := update.StaleVersions(appDir, current)
	if len(versions) == 0 {
		if verbose {
			Successf(out, "%s is already clean", app)
		}
		return
	}
	fmt.Fprintf(out, "Removing %s:", app)
	for _, v := range versions {
		fmt.Fprintf(out, " %s", v)
		dir := env.VersionDir(app, v, global)
		_, raw := env.InstalledManifest(app, v, global)
		install.UnlinkPersistData(persistViewsFromRaw(raw), dir)
		_ = os.RemoveAll(dir)
	}
	fmt.Fprintln(out, "")
	update.PruneEmptyCurrent(appDir)
}

// persistViewsFromRaw converts the raw manifest persist section to
// install views without touching the shared Manifest shape. Strings
// map to same-name pairs; pairs pass through.
func persistViewsFromRaw(raw []byte) []install.PersistView {
	var doc struct {
		Persist any `json:"persist"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	var items []any
	switch t := doc.Persist.(type) {
	case nil:
		return nil
	case string:
		items = []any{t}
	case []any:
		items = t
	default:
		return nil
	}
	var out []install.PersistView
	for _, item := range items {
		switch t := item.(type) {
		case string:
			out = append(out, install.PersistView{Source: t, Target: t})
		case []any:
			if len(t) == 2 {
				src, ok1 := t[0].(string)
				dst, ok2 := t[1].(string)
				if ok1 && ok2 {
					out = append(out, install.PersistView{Source: src, Target: dst})
				}
			} else if len(t) == 1 {
				if s, ok := t[0].(string); ok {
					out = append(out, install.PersistView{Source: s, Target: s})
				}
			}
		}
	}
	return out
}
