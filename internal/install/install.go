// Package install implements the mutation pipelines in classic order.
//
// Install order mirrors install_app (lib/install.ps1:46-82) and
// Appendix A: resolve, version checks, architecture negotiation,
// version dir, download, extraction, pre_install, installer,
// PATH guard, current junction, shims, shortcuts, psmodule,
// env_add_path, env_set, persist, persist ACL, post_install, then the
// atomic commit of scoop-manifest.json plus scoop-install.json.
// Uninstall mirrors scoop-uninstall.ps1:48-150 in reverse.
//
// Downloads run through the Downloader seam (parallel callers fan out;
// this package never duplicates download logic). Extraction runs
// through the Extractor seam. Only the hook runner spawns
// powershell.exe. No emojis.
package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gscoop/internal/deps"
	"gscoop/internal/hook"
	"gscoop/internal/junction"
	"gscoop/internal/lnk"
	"gscoop/internal/shim"
)

// Op is one package operation in a transaction.
type Op struct {
	App          string
	Version      string
	Architecture string
	Global       bool
	Bucket       string
	URL          string
	ManifestRaw  []byte
}

// Transaction is a dependency-ordered list of operations.
type Transaction struct {
	Ops []Op
}

// ManifestView is the subset of manifest fields the pipeline reads.
// It adapts both internal/manifest and command-local shapes without
// importing either, keeping the seam testable.
type ManifestView struct {
	Version       string
	URLs          []string
	ManifestRaw   []byte
	Installer     InstallerView
	Uninstaller   InstallerView
	PreInstall    string
	PostInstall   string
	PreUninstall  string
	PostUninstall string
	Bins          []shim.Entry
	Shortcuts     []ShortcutView
	EnvAddPath    []string
	EnvSet        map[string]string
	Persist       []PersistView
	PSModuleName  string
	Notes         string
	Depends       []string
	Suggest       map[string][]string
}

// InstallerView mirrors installer/uninstaller file plus args, script,
// and keep flag (lib/install.ps1:84-141).
type InstallerView struct {
	File   string
	Args   []string
	Script string
	Keep   bool
}

// ShortcutView mirrors one shortcuts entry.
type ShortcutView struct {
	Target string
	Name   string
	Args   string
	Icon   string
}

// PersistView mirrors one persist_def pair (lib/install.ps1:428-442).
type PersistView struct {
	Source string
	Target string
}

// Lookup loads ManifestView for app at arch, or an error when no
// manifest exists.
type Lookup func(app, arch string) (*ManifestView, error)

// Downloader fetches all arch URLs for one op into the staging dir
// and returns downloaded file names. The production downloader lives
// outside this package; the seam keeps download logic in one place.
type Downloader interface {
	Download(ctx context.Context, op Op, dir string) ([]string, error)
}

// Extractor expands one downloaded file into the staging dir.
type Extractor interface {
	Extract(file, destDir string) error
}

// HookScript runs one inline hook script.
type HookScript interface {
	RunScript(ctx context.Context, hook string, vars hook.Vars, script string) error
}

// Env carries roots and toggles the pipeline needs.
type Env struct {
	ScoopDir               string
	GlobalDir              string
	NoJunction             bool
	IgnoreRunningProcesses bool
	ShimVariant            string
}

// Logger receives user-visible lines. Out gets success and progress;
// Err gets warnings and errors. Nil writers fall back to std streams.
type Logger struct {
	Out io.Writer
	Err io.Writer
}

func (l Logger) out() io.Writer {
	if l.Out == nil {
		return os.Stdout
	}
	return l.Out
}

func (l Logger) errOut() io.Writer {
	if l.Err == nil {
		return os.Stderr
	}
	return l.Err
}

// versionChars mirrors the manifest version guard
// (lib/install.ps1:17-19).
var versionChars = regexp.MustCompile(`^[A-Za-z0-9._\-\+]+$`)

// NightlyVersion maps nightly to nightly-yyyyMMdd and disables hash
// checks at the call site (lib/install.ps1:1-6, :21-25).
func NightlyVersion(now time.Time) string {
	return "nightly-" + now.Format("20060102")
}

// Plan resolves manifests, versions, and architectures in dependency
// order. nightly maps to a dated version. Invalid versions and
// unknown architectures abort like classic.
func Plan(apps []string, arch string, lookup Lookup, depFetch deps.Fetcher) (Transaction, error) {
	ordered, err := deps.ResolveAll(depFetch, apps, arch)
	if err != nil {
		return Transaction{}, err
	}
	var tx Transaction
	for _, spec := range ordered {
		name := stripBucket(spec)
		mv, err := lookup(name, arch)
		if err != nil {
			return Transaction{}, err
		}
		if mv == nil {
			return Transaction{}, fmt.Errorf("Couldn't find manifest for '%s'.", name)
		}
		version := mv.Version
		if version == "" {
			return Transaction{}, fmt.Errorf("Manifest doesn't specify a version.")
		}
		if version == "nightly" {
			version = NightlyVersion(time.Now())
		}
		if !versionChars.MatchString(version) {
			m := version
			for _, r := range version {
				s := string(r)
				if !regexp.MustCompile(`[\w.\-+]`).MatchString(s) {
					m = s
					break
				}
			}
			return Transaction{}, fmt.Errorf("Manifest version has unsupported character '%s'.", m)
		}
		if arch == "" {
			return Transaction{}, fmt.Errorf("'%s' doesn't support current architecture!", name)
		}
		tx.Ops = append(tx.Ops, Op{App: name, Version: version, Architecture: arch})
	}
	return tx, nil
}

func stripBucket(spec string) string {
	if i := strings.LastIndexAny(spec, "/\\"); i >= 0 {
		return spec[i+1:]
	}
	return spec
}

// Base returns the scope root.
func (e Env) Base(global bool) string {
	if global {
		return e.GlobalDir
	}
	return e.ScoopDir
}

// AppDir returns apps/<app> for scope.
func (e Env) AppDir(app string, global bool) string {
	return filepath.Join(e.Base(global), "apps", app)
}

// VersionDir returns apps/<app>/<version> for scope.
func (e Env) VersionDir(app, version string, global bool) string {
	return filepath.Join(e.AppDir(app, global), version)
}

// StagingDir returns the crash-atomic extract target:
// <version>.tmp sibling, renamed to <version> on success.
func (e Env) StagingDir(app, version string, global bool) string {
	return e.VersionDir(app, version, global) + ".tmp"
}

// Executor wires pipeline stages to the filesystem.
type Executor struct {
	Env        Env
	Log        Logger
	Downloader Downloader
	Extractor  Extractor
	Hooks      HookScript
	// LookupManifest reloads ManifestView per op during Install.
	LookupManifest Lookup
	// Running lists running process executable paths for the guard.
	// Nil selects the OS implementation.
	Running func(appDir string) []string
	// Now supplies nightly dates. Nil means time.Now.
	Now func() time.Time
}

// Install runs the transaction strictly sequentially in dependency
// order. Downloads for the whole transaction may run beforehand
// through DownloadAll; Install itself is sequential.
func (x *Executor) Install(ctx context.Context, tx Transaction) error {
	for _, op := range tx.Ops {
		if err := x.installOne(ctx, op); err != nil {
			return err
		}
	}
	return nil
}

// DownloadAll fetches every op concurrently through Downloader.
// Sequential callers pass maxParallel 1; the pacman default fans out.
func (x *Executor) DownloadAll(ctx context.Context, tx Transaction, maxParallel int) map[string]error {
	if maxParallel < 1 {
		maxParallel = 1
	}
	type job struct {
		op Op
	}
	jobs := make(chan Op)
	errs := make(map[string]error)
	done := make(chan struct{})
	go func() {
		defer close(done)
	}()
	_ = jobs
	_ = done
	// Bounded worker pool over ops; sequential when maxParallel is 1.
	sem := make(chan struct{}, maxParallel)
	results := make(chan struct {
		key string
		err error
	}, len(tx.Ops))
	for _, op := range tx.Ops {
		op := op
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			dir := x.Env.StagingDir(op.App, op.Version, op.Global)
			var err error
			if x.Downloader != nil {
				_, err = x.Downloader.Download(ctx, op, dir)
			}
			results <- struct {
				key string
				err error
			}{key: op.App, err: err}
		}()
	}
	for range tx.Ops {
		r := <-results
		if r.err != nil {
			errs[r.key] = r.err
		}
	}
	return errs
}

func (x *Executor) installOne(ctx context.Context, op Op) error {
	env := x.Env
	log := x.Log
	mv := x.viewFor(op)
	if mv == nil {
		return fmt.Errorf("Couldn't find manifest for '%s'.", op.App)
	}
	fmt.Fprintf(log.out(), "Installing '%s' (%s) [%s]\n", op.App, op.Version, op.Architecture)
	staging := env.StagingDir(op.App, op.Version, op.Global)
	final := env.VersionDir(op.App, op.Version, op.Global)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	// Download into the staging dir.
	var files []string
	if x.Downloader != nil {
		var err error
		files, err = x.Downloader.Download(ctx, op, staging)
		if err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	// Extract each file.
	if x.Extractor != nil {
		for _, f := range files {
			if err := x.Extractor.Extract(filepath.Join(staging, f), staging); err != nil {
				_ = os.RemoveAll(staging)
				return err
			}
		}
	}
	vars := hook.Vars{
		Dir:          staging,
		OriginalDir:  staging,
		PersistDir:   filepath.Join(env.Base(op.Global), "persist", op.App),
		Version:      op.Version,
		Architecture: op.Architecture,
		Global:       op.Global,
		ScoopDir:     env.ScoopDir,
		ScoopGlobal:  env.GlobalDir,
	}
	if x.Hooks != nil && mv.PreInstall != "" {
		if err := x.Hooks.RunScript(ctx, hook.TypePreInstall, vars, mv.PreInstall); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	if err := x.runInstaller(ctx, op, mv, staging, files, vars, false); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	EnsureInstallDirNotInPath(staging, op.Global)
	// Atomic commit of the version dir: rename staging to final.
	// Failure leaves the classic failed() state (staging removed, no
	// current, no metadata) rather than a half-linked tree.
	if _, err := os.Stat(final); err == nil {
		_ = os.RemoveAll(staging)
	} else {
		if err := os.Rename(staging, final); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	vars.Dir = final
	current := final
	if !env.NoJunction {
		link, err := junction.LinkCurrent(env.AppDir(op.App, op.Global), final, func(s string) {
			fmt.Fprintln(log.out(), s)
		})
		if err != nil {
			return err
		}
		current = link
		vars.Dir = current
	}
	shimDir := filepath.Join(env.Base(op.Global), "shims")
	if err := CreateShims(shimDir, final, mv.Bins, staging, vars, env.ShimVariant, func(s string) {
		fmt.Fprintln(log.out(), s)
	}); err != nil {
		return err
	}
	if err := CreateShortcuts(filepath.Join(env.Base(op.Global), "scoop-apps-menu"), final, mv.Shortcuts, vars, func(s string) {
		fmt.Fprintln(log.out(), s)
	}); err != nil {
		return err
	}
	if err := InstallPSModule(env.Base(op.Global), final, mv.PSModuleName, func(s string) {
		fmt.Fprintln(log.out(), s)
	}); err != nil {
		return err
	}
	ApplyEnvAddPath(mv.EnvAddPath, current, op.Global)
	ApplyEnvSet(mv.EnvSet, op.Global)
	if err := PersistData(mv.Persist, staging, vars.PersistDir, func(s string) {
		fmt.Fprintln(log.out(), s)
	}); err != nil {
		return err
	}
	PersistPermission(env.Base(op.Global), mv.Persist, op.Global)
	if x.Hooks != nil && mv.PostInstall != "" {
		if err := x.Hooks.RunScript(ctx, hook.TypePostInstall, vars, mv.PostInstall); err != nil {
			return err
		}
	}
	if err := WriteInstallMetadata(final, op, mv.ManifestRaw); err != nil {
		return err
	}
	fmt.Fprintf(log.out(), "'%s' (%s) was installed successfully!\n", op.App, op.Version)
	ShowNotes(log.out(), mv.Notes, vars)
	return nil
}
func (x *Executor) viewFor(op Op) *ManifestView {
	if x.LookupManifest == nil {
		return &ManifestView{Version: op.Version}
	}
	mv, err := x.LookupManifest(op.App, op.Architecture)
	if err != nil || mv == nil {
		return nil
	}
	return mv
}

// runInstaller mirrors Invoke-Installer file plus args and script
// handling with the keep flag (lib/install.ps1:84-141).
func (x *Executor) runInstaller(ctx context.Context, op Op, mv *ManifestView, dir string, files []string, vars hook.Vars, uninstall bool) error {
	inst := mv.Installer
	hookName := hook.TypeInstaller
	if uninstall {
		inst = mv.Uninstaller
		hookName = hook.TypeUninstaller
	}
	if inst.File != "" || len(inst.Args) > 0 {
		name := inst.File
		if name == "" && len(files) > 0 {
			name = files[0]
		}
		prog := filepath.Join(dir, name)
		if !isInDir(dir, prog) {
			return fmt.Errorf("Error in manifest: installer %s is outside the app directory.", prog)
		}
		if _, err := os.Stat(prog); err != nil {
			return fmt.Errorf("Installer %s is missing.", prog)
		}
		args := hook.SubstituteAll(inst.Args, vars)
		if strings.HasSuffix(strings.ToLower(prog), ".ps1") {
			if x.Hooks == nil {
				return fmt.Errorf("hook runner unavailable for %s", prog)
			}
			runner, ok := x.Hooks.(*hook.Runner)
			if !ok {
				return fmt.Errorf("hook runner cannot run ps1 files")
			}
			if err := runner.RunPS1File(ctx, hookName, vars, prog, args); err != nil {
				return abortInstall(op.App, uninstall)
			}
		} else {
			if err := hook.RunExecutable(ctx, x.Log.out(), "Running "+hookName+" ...", prog, args); err != nil {
				return abortInstall(op.App, uninstall)
			}
			if !inst.Keep {
				_ = os.Remove(prog)
			}
		}
	}
	script := inst.Script
	if script == "" {
		return nil
	}
	if x.Hooks == nil {
		return nil
	}
	if err := x.Hooks.RunScript(ctx, hookName, vars, script); err != nil {
		return abortInstall(op.App, uninstall)
	}
	return nil
}

func abortInstall(app string, uninstall bool) error {
	if uninstall {
		return fmt.Errorf("Uninstallation aborted.")
	}
	return fmt.Errorf("Installation aborted. You might need to run 'scoop uninstall %s' before trying again.", app)
}

// CreateShims resolves each bin entry and writes the shim triple.
func CreateShims(shimDir, dir string, bins []shim.Entry, originalDir string, vars hook.Vars, variant string, emit func(string)) error {
	for _, b := range bins {
		name := b.Name
		if name == "" {
			name = shim.ShimName(b.Target)
		}
		emit(fmt.Sprintf("Creating shim for '%s'.", name))
		target := filepath.Join(dir, b.Target)
		if _, err := os.Stat(target); err != nil {
			if filepath.IsAbs(b.Target) {
				target = b.Target
			} else {
				return fmt.Errorf("Can't shim '%s': File doesn't exist.", b.Target)
			}
		}
		if _, err := os.Stat(target); err != nil {
			return fmt.Errorf("Can't shim '%s': File doesn't exist.", b.Target)
		}
		arg := shim.Substitute(b.Args, dir, originalDir, vars.PersistDir)
		if strings.HasSuffix(strings.ToLower(target), ".exe") || strings.HasSuffix(strings.ToLower(target), ".com") {
			rewritten, err := shim.WriteExe(shimDir, name, target, variant)
			if err != nil {
				return err
			}
			if err := shim.WriteTextShim(shimDir, name, rewritten, arg); err != nil {
				return err
			}
			continue
		}
		if err := shim.Wrappers(shimDir, name, target, arg); err != nil {
			return err
		}
	}
	return nil
}

// CreateShortcuts writes menu shortcuts for entries.
func CreateShortcuts(menuDir, dir string, entries []ShortcutView, vars hook.Vars, emit func(string)) error {
	for _, e := range entries {
		target := filepath.Join(dir, e.Target)
		args := hook.Substitute(e.Args, vars)
		icon := ""
		if e.Icon != "" {
			icon = filepath.Join(dir, e.Icon)
		}
		s := lnk.Shortcut{Target: target, Name: e.Name, Args: args, Icon: icon}
		path := lnk.PathFor(menuDir, e.Name)
		if err := lnk.Create(path, s); err != nil {
			fmt.Fprintf(os.Stderr, "Creating shortcut for %s (%s) failed\n", e.Name, e.Target)
			continue
		}
		emit(fmt.Sprintf("Creating shortcut for %s (%s)", e.Name, e.Target))
	}
	return nil
}
