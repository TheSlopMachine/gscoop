package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/gitengine"
	"github.com/TheSlopMachine/gscoop/internal/install"
)

// RunBucketAdd mirrors add_bucket (lib/buckets.ps1:123-170): exit 2
// for duplicates, 1 for git or repo failures, 0 on success.
func RunBucketAdd(env *Env, out io.Writer, name, repo string, known map[string]string) int {
	useExternal := false
	if cfg, err := config.Load(config.ConfigFilePath()); err == nil {
		if flag, ok := cfg.GetBool("use_external_git"); ok {
			useExternal = flag
		}
	}
	engine := gitengine.New(useExternal)
	var local []gitengine.BucketRef
	for _, b := range env.ListBucketNames() {
		remote := ""
		if e, ok := engine.(interface {
			ConfigGet(string, string) (string, error)
		}); ok {
			remote, _ = e.ConfigGet(filepath.Join(env.BucketsDir(), b), "remote.origin.url")
		}
		local = append(local, gitengine.BucketRef{Name: b, Remote: remote})
	}
	switch gitengine.AddBucket(engine, env.BucketsDir(), name, repo, known, local) {
	case 0:
		Successf(out, "The %s bucket was added successfully.", name)
		return 0
	case 2:
		Warnf(out, "The '%s' bucket already exists.", name)
		return 2
	default:
		Errorf(out, "Failed to add bucket '%s'.", name)
		return 1
	}
}

// RunBucketRm mirrors rm_bucket (lib/buckets.ps1:172-186).
func RunBucketRm(env *Env, out io.Writer, name string) int {
	if gitengine.RemoveBucket(env.BucketsDir(), name) != 0 {
		Errorf(out, "'%s' bucket not found.", name)
		return 1
	}
	Successf(out, "The %s bucket was removed successfully.", name)
	return 0
}

// RunHold mirrors scoop-hold.ps1: scoop itself writes
// HOLD_UPDATE_UNTIL one day out; apps write hold:true through the
// ordered install-info writer.
//
// Strictness: missing apps exit 1; global scope without admin rights
// exits 1 before any mutation; per-app failures accumulate and the
// exit is 1 when any item fails. Already-held items report info and
// skip the rewrite.
func RunHold(env *Env, out io.Writer, args []string, global bool) int {
	if len(args) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "hold")
		return 1
	}
	if global && !install.IsAdmin() {
		Errorf(out, "You need admin rights to hold global apps.")
		return 1
	}
	code := 0
	for _, app := range args {
		if app == "scoop" {
			path := config.ConfigFilePath()
			store, err := config.Load(path)
			if err != nil {
				fmt.Fprintln(out, err.Error())
				code = 1
				continue
			}
			if v, ok := store.Value("hold_update_until"); ok && v != nil && fmt.Sprint(v) != "" {
				Infof(out, "'%s' is already held.", app)
				continue
			}
			hold := holdUntilTomorrow()
			if err := store.Set("hold_update_until", hold); err != nil {
				fmt.Fprintln(out, err.Error())
				code = 1
				continue
			}
			if err := store.Save(); err != nil {
				fmt.Fprintln(out, err.Error())
				code = 1
				continue
			}
			Successf(out, "%s is now held and might not be updated until %s.", app, hold)
			continue
		}
		if !env.Installed(app, nil) {
			if global {
				Errorf(out, "'%s' is not installed globally.", app)
			} else {
				Errorf(out, "'%s' is not installed.", app)
			}
			code = 1
			continue
		}
		ver := env.CurrentVersion(app, global)
		dir := env.VersionDir(app, ver, global)
		if dirHoldSet(dir) {
			Infof(out, "'%s' is already held.", app)
			continue
		}
		if err := setHoldFlag(dir, true); err != nil {
			Errorf(out, "Failed to hold '%s'.", app)
			code = 1
			continue
		}
		Successf(out, "%s is now held and can not be updated anymore.", app)
	}
	return code
}

// RunUnhold mirrors scoop-unhold.ps1: scoop clears
// HOLD_UPDATE_UNTIL; apps clear hold through the ordered writer.
//
// Strictness: missing apps exit 1; global scope without admin rights
// exits 1 before any mutation; per-app failures accumulate and the
// exit is 1 when any item fails. Items that are not held report info
// and skip the rewrite.
func RunUnhold(env *Env, out io.Writer, args []string, global bool) int {
	if len(args) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "unhold")
		return 1
	}
	if global && !install.IsAdmin() {
		Errorf(out, "You need admin rights to unhold global apps.")
		return 1
	}
	code := 0
	for _, app := range args {
		if app == "scoop" {
			path := config.ConfigFilePath()
			store, err := config.Load(path)
			if err == nil {
				if v, ok := store.Value("hold_update_until"); !ok || v == nil || fmt.Sprint(v) == "" {
					Infof(out, "'%s' is not held.", app)
					continue
				}
				store.Remove("hold_update_until")
				_ = store.Save()
			}
			Successf(out, "%s is no longer held and can be updated again.", app)
			continue
		}
		if !env.Installed(app, nil) {
			if global {
				Errorf(out, "'%s' is not installed globally.", app)
			} else {
				Errorf(out, "'%s' is not installed.", app)
			}
			code = 1
			continue
		}
		ver := env.CurrentVersion(app, global)
		dir := env.VersionDir(app, ver, global)
		if !dirHoldSet(dir) {
			Infof(out, "'%s' is not held.", app)
			continue
		}
		if err := setHoldFlag(dir, false); err != nil {
			Errorf(out, "Failed to unhold '%s'.", app)
			code = 1
			continue
		}
		Successf(out, "%s is no longer held and can be updated again.", app)
	}
	return code
}

func setHoldFlag(dir string, hold bool) error {
	for _, name := range []string{"scoop-install.json", "install.json"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return writeHoldFile(path, hold)
		}
	}
	// No metadata yet: create a minimal scoop-install.json so hold
	// state survives like classic save_install_info.
	return writeHoldFile(filepath.Join(dir, "scoop-install.json"), hold)
}
