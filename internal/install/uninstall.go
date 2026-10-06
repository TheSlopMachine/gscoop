package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gscoop/internal/hook"
	"gscoop/internal/junction"
	"gscoop/internal/shim"
)

// Uninstall removes one version in precise reverse order, mirroring
// scoop-uninstall.ps1:48-150: pre_uninstall, running guard,
// uninstaller, shims, shortcuts, unlink current, psmodule, env,
// version dir removal, post_uninstall, older versions, empty appdir,
// optional purge.
func (x *Executor) Uninstall(ctx context.Context, app, version string, global, purge bool) error {
	env := x.Env
	log := x.Log
	mv := x.viewFor(Op{App: app, Version: version, Architecture: ""})
	if mv == nil {
		// Fall back to installed metadata when the bucket is gone.
		mv = &ManifestView{Version: version}
		if raw, err := os.ReadFile(filepath.Join(env.VersionDir(app, version, global), "scoop-manifest.json")); err == nil {
			mv.ManifestRaw = raw
		}
	}
	fmt.Fprintf(log.out(), "Uninstalling '%s' (%s).\n", app, version)
	dir := env.VersionDir(app, version, global)
	vars := hook.Vars{
		Dir:          dir,
		OriginalDir:  dir,
		PersistDir:   filepath.Join(env.Base(global), "persist", app),
		Version:      version,
		Architecture: archOf(dir),
		Global:       global,
		ScoopDir:     env.ScoopDir,
		ScoopGlobal:  env.GlobalDir,
	}
	if x.Hooks != nil && mv.PreUninstall != "" {
		if err := x.Hooks.RunScript(ctx, hook.TypePreUninstall, vars, mv.PreUninstall); err != nil {
			return err
		}
	}
	if err := CheckRunning(env.AppDir(app, global), env.IgnoreRunningProcesses, x.Running); err != nil {
		return err
	}
	if err := x.runInstaller(ctx, Op{App: app, Version: version, Global: global}, mv, dir, nil, vars, true); err != nil {
		return err
	}
	shimDir := filepath.Join(env.Base(global), "shims")
	for _, b := range mv.Bins {
		name := b.Name
		if name == "" {
			name = shim.ShimName(b.Target)
		}
		for _, line := range shim.RemoveShim(shimDir, name, app) {
			fmt.Fprintln(log.out(), line)
		}
	}
	removeShortcuts(filepath.Join(env.Base(global), "scoop-apps-menu"), mv.Shortcuts, func(s string) {
		fmt.Fprintln(log.out(), s)
	})
	refdir := dir
	if !env.NoJunction {
		refdir = junction.UnlinkCurrent(env.AppDir(app, global), dir, func(s string) {
			fmt.Fprintln(log.out(), s)
		})
	}
	UninstallPSModule(env.Base(global), mv.PSModuleName, func(s string) {
		fmt.Fprintln(log.out(), s)
	})
	RemoveEnv(mv.EnvAddPath, mv.EnvSet, refdir, global, nil)
	UnlinkPersistData(mv.Persist, dir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("Couldn't remove '%s'; it may be in use.", dir)
	}
	if x.Hooks != nil && mv.PostUninstall != "" {
		if err := x.Hooks.RunScript(ctx, hook.TypePostUninstall, vars, mv.PostUninstall); err != nil {
			return err
		}
	}
	removeOlderVersions(env.AppDir(app, global), func(s string) {
		fmt.Fprintln(log.out(), s)
	})
	if _, err := os.Lstat(filepath.Join(env.AppDir(app, global), "current")); err == nil {
		_ = junction.Delete(filepath.Join(env.AppDir(app, global), "current"))
	}
	if empty, _ := isEmptyDir(env.AppDir(app, global)); empty {
		_ = os.Remove(env.AppDir(app, global))
	}
	if purge {
		fmt.Fprintln(log.out(), "Removing persisted data.")
		_ = os.RemoveAll(filepath.Join(env.Base(global), "persist", app))
	}
	fmt.Fprintf(log.out(), "'%s' was uninstalled.\n", app)
	return nil
}

func archOf(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "scoop-install.json"))
	if err != nil {
		return "64bit"
	}
	// Minimal arch probe without a JSON dependency cycle risk.
	for _, arch := range []string{"64bit", "32bit", "arm64"} {
		if len(data) > 0 && containsArch(data, arch) {
			return arch
		}
	}
	return "64bit"
}

func containsArch(data []byte, arch string) bool {
	return len(data) > 0 && stringContains(string(data), arch)
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func removeShortcuts(menuDir string, entries []ShortcutView, emit func(string)) {
	for _, e := range entries {
		path := filepath.Join(menuDir, e.Name+".lnk")
		if _, err := os.Stat(path); err == nil {
			emit("Removing shortcut " + path)
			_ = os.Remove(path)
		}
	}
}

func removeOlderVersions(appDir string, emit func(string)) {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == "current" || !e.IsDir() {
			continue
		}
		emit("Removing older version (" + e.Name() + ").")
		_ = os.RemoveAll(filepath.Join(appDir, e.Name()))
	}
}

func isEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
