package commands

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/TheSlopMachine/gscoop/internal/config"
)

// Env carries the resolved Scoop roots for one invocation.
//
// TODO(state,config): derive roots, NO_JUNCTION, FORCE_UPDATE, and
// DEFAULT_ARCHITECTURE from internal/config and internal/state when those
// packages land. Current resolution covers env vars and OS defaults only.
type Env struct {
	ScoopDir    string
	GlobalDir   string
	CacheDir    string
	ConfigPath  string
	NoJunction  bool
	ForceUpdate bool
	Arch        string
}

// DefaultEnv resolves roots from the environment with OS defaults.
func DefaultEnv() *Env {
	scoop := os.Getenv("SCOOP")
	if scoop == "" {
		if home, err := os.UserHomeDir(); err == nil {
			scoop = filepath.Join(home, "scoop")
		} else {
			scoop = filepath.Join(".", "scoop")
		}
	}
	global := os.Getenv("SCOOP_GLOBAL")
	if global == "" {
		if runtime.GOOS == "windows" {
			global = `C:\ProgramData\scoop`
		} else {
			global = filepath.Join(scoop, "global")
		}
	}
	cache := os.Getenv("SCOOP_CACHE")
	if cache == "" {
		cache = filepath.Join(scoop, "cache")
	}
	arch, err := FormatArch(os.Getenv("SCOOP_ARCH"))
	if err != nil || arch == "" {
		arch = defaultArch()
	}
	return &Env{
		ScoopDir:   scoop,
		GlobalDir:  global,
		CacheDir:   cache,
		ConfigPath: config.ConfigFilePath(),
		Arch:       arch,
	}
}

func defaultArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	if runtime.GOARCH == "386" {
		return "32bit"
	}
	return "64bit"
}

// BaseDir returns the global root for global scope, else the user root.
// Mirrors basedir in lib/core.ps1.
func (e *Env) BaseDir(global bool) string {
	if global {
		return e.GlobalDir
	}
	return e.ScoopDir
}

// AppsDir mirrors appsdir in lib/core.ps1.
func (e *Env) AppsDir(global bool) string {
	return filepath.Join(e.BaseDir(global), "apps")
}

// AppDir mirrors appdir in lib/core.ps1.
func (e *Env) AppDir(app string, global bool) string {
	return filepath.Join(e.AppsDir(global), app)
}

// VersionDir mirrors versiondir in lib/core.ps1.
func (e *Env) VersionDir(app, version string, global bool) string {
	return filepath.Join(e.AppDir(app, global), version)
}

// CurrentDir mirrors currentdir in lib/core.ps1.
func (e *Env) CurrentDir(app string, global bool) string {
	if e.NoJunction && app != "scoop" {
		if v := e.CurrentVersion(app, global); v != "" {
			return e.VersionDir(app, v, global)
		}
	}
	return filepath.Join(e.AppDir(app, global), "current")
}

// PersistDir mirrors persistdir in lib/core.ps1.
func (e *Env) PersistDir(app string, global bool) string {
	return filepath.Join(e.BaseDir(global), "persist", app)
}

// BucketsDir mirrors $bucketsdir in lib/buckets.ps1.
func (e *Env) BucketsDir() string {
	return filepath.Join(e.ScoopDir, "buckets")
}

// ShimDir mirrors shimdir in lib/core.ps1.
func (e *Env) ShimDir(global bool) string {
	return filepath.Join(e.BaseDir(global), "shims")
}

// UserManifestsDir mirrors usermanifestsdir in lib/core.ps1.
func (e *Env) UserManifestsDir() string {
	return filepath.Join(e.ScoopDir, "workspace")
}
