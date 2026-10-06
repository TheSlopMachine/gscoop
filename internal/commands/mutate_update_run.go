// Update command wiring for Phase 3A (internal/commands/mutate_update_*.go).
//
// RunUpdate mirrors libexec/scoop-update.ps1: flag surface,
// Sync-Scoop plus Sync-Bucket through internal/update and the
// gitengine seam, HOLD_UPDATE_UNTIL handling, LAST_UPDATE stamping,
// outdated selection with hold skips, and per-app reinstall reusing
// the install transaction seam (parallel downloads, sequential
// installs). The gscoop binary self-update follows the release
// channel with a rename-swap; the classic scoop checkout
// fast-forwards best-effort. No emojis.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/gitengine"
	"github.com/TheSlopMachine/gscoop/internal/hook"
	"github.com/TheSlopMachine/gscoop/internal/install"
	"github.com/TheSlopMachine/gscoop/internal/junction"
	"github.com/TheSlopMachine/gscoop/internal/shim"
	"github.com/TheSlopMachine/gscoop/internal/update"
)

// OwnsPhase3A reports whether name is a Phase 3A command owned by the
// mutate_update/cleanup/misc files: update, cleanup, alias, home,
// virustotal, shim.
func OwnsPhase3A(name string) bool {
	switch name {
	case "update", "cleanup", "alias", "home", "virustotal", "shim":
		return true
	default:
		return false
	}
}

// RunPhase3A dispatches one Phase 3A command. It returns the exit code
// and true when name is owned here, or 0 and false otherwise.
func RunPhase3A(env *Env, out io.Writer, name string, args []string) (int, bool) {
	switch name {
	case "update":
		return RunUpdate(env, out, args), true
	case "cleanup":
		return RunCleanup(env, out, args), true
	case "alias":
		return RunAlias(env, out, args), true
	case "home":
		return RunHome(env, out, args), true
	case "virustotal":
		return RunVirusTotal(env, out, args), true
	case "shim":
		return RunShim(env, out, args), true
	default:
		return 0, false
	}
}

// newUpdateEngine selects the git engine for bucket and core sync.
// Tests override it with a stub; production uses gitengine.New.
var newUpdateEngine = gitengine.New

// updateSettings carries the config values scoop-update consumes.
type updateSettings struct {
	store          *config.Store
	showLog        bool
	repo           string
	branch         string
	autostash      bool
	useGitHistory  bool
	updateNightly  bool
	forceUpdate    bool
	ignoreRunning  bool
	maxDownloads   int
	channel        string
	gscoopRepo     string
	useExternalGit bool
	shallowBuckets bool
	noJunction     bool
	scoopDir       string
	globalDir      string
}

// loadUpdateSettings reads scoop-update keys with classic defaults.
// A malformed config file aborts the command.
func loadUpdateSettings(env *Env, out io.Writer) (*updateSettings, error) {
	store, err := config.Load(env.ConfigPath)
	if err != nil {
		Errorf(out, "Could not load config: %s", err.Error())
		return nil, err
	}
	boolOr := func(key string, def bool) bool {
		if v, ok := store.GetBool(key); ok {
			return v
		}
		return def
	}
	s := &updateSettings{store: store}
	s.showLog = boolOr("show_update_log", true)
	s.autostash = boolOr("autostash_on_conflict", false)
	s.useGitHistory = boolOr("use_git_history", true)
	s.updateNightly = boolOr("update_nightly", false)
	s.forceUpdate = boolOr("force_update", false)
	s.ignoreRunning = boolOr("ignore_running_processes", false)
	s.useExternalGit = boolOr("use_external_git", false)
	s.shallowBuckets = boolOr("shallow_buckets", false)
	s.noJunction = boolOr("no_junction", false)
	if v, ok := store.GetString("scoop_repo"); ok && v != "" {
		s.repo = v
	} else {
		s.repo = "https://github.com/ScoopInstaller/Scoop"
		_ = store.Set("scoop_repo", s.repo)
	}
	if v, ok := store.GetString("scoop_branch"); ok && v != "" {
		s.branch = v
	} else {
		s.branch = "master"
		_ = store.Set("scoop_branch", s.branch)
	}
	s.maxDownloads = 4
	if v, ok := store.Value("max_downloads"); ok {
		if n, ok := v.(float64); ok && n >= 1 {
			s.maxDownloads = int(n)
		}
	}
	s.channel = "stable"
	if v, ok := store.GetString("gscoop_channel"); ok && v != "" {
		s.channel = strings.ToLower(v)
	}
	s.gscoopRepo = "TheSlopMachine/gscoop"
	if v, ok := store.GetString("gscoop_repo"); ok && v != "" {
		s.gscoopRepo = strings.TrimPrefix(strings.TrimPrefix(v, "https://github.com/"), "http://github.com/")
		s.gscoopRepo = strings.TrimSuffix(s.gscoopRepo, ".git")
	}
	_ = store.Save()
	return s, nil
}

// RunUpdate mirrors libexec/scoop-update.ps1: -f/--force,
// -g/--global, -i/--independent, -k/--no-cache,
// -s/--skip-hash-check, -q/--quiet, -a/--all.
func RunUpdate(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "gfiksqa", []string{"global", "force", "independent", "no-cache", "skip-hash-check", "quiet", "all"})
	if r.Err != "" {
		Errorf(out, "scoop update: %s", r.Err)
		return 1
	}
	global := r.Has("g") || r.Has("global")
	force := r.Has("f") || r.Has("force")
	independent := r.Has("i") || r.Has("independent")
	useCache := !(r.Has("k") || r.Has("no-cache"))
	checkHash := !(r.Has("s") || r.Has("skip-hash-check"))
	quiet := r.Has("q") || r.Has("quiet")
	all := r.Has("a") || r.Has("all")
	_ = useCache
	_ = checkHash
	cfg, err := loadUpdateSettings(env, out)
	if err != nil {
		return 1
	}
	local := *env
	local.NoJunction = cfg.noJunction || env.NoJunction
	local.ForceUpdate = cfg.forceUpdate
	engine := newUpdateEngine(cfg.useExternalGit)
	now := time.Now()
	apps := r.Rest
	if len(apps) == 0 && !all {
		if global {
			Errorf(out, "scoop update: --global is invalid when <app> is not specified.")
			return 1
		}
		if !useCache {
			Errorf(out, "scoop update: --no-cache is invalid when <app> is not specified.")
			return 1
		}
		syncScoopCore(&local, out, engine, cfg, now)
		syncBuckets(&local, out, engine, cfg)
		update.StampUpdate(cfg.store, now)
		Successf(out, "Scoop was updated successfully!")
		return 0
	}
	if global && !install.IsAdmin() {
		Errorf(out, "You need admin rights to update global apps.")
		return 1
	}
	updateScoop := update.ScoopOutdated(cfg.store, now)
	for _, a := range apps {
		if update.ParseAppSpec(a).App == "scoop" {
			updateScoop = true
		}
	}
	apps = filterScoop(apps)
	pins := updatePins(apps)
	if updateScoop {
		syncScoopCore(&local, out, engine, cfg, now)
		syncBuckets(&local, out, engine, cfg)
		update.StampUpdate(cfg.store, now)
		Successf(out, "Scoop was updated successfully!")
	}
	var tuples [][2]any
	if (len(apps) == 1 && apps[0] == "*") || all {
		for _, app := range local.InstalledApps(false) {
			tuples = append(tuples, [2]any{app, false})
		}
		if global {
			for _, app := range local.InstalledApps(true) {
				tuples = append(tuples, [2]any{app, true})
			}
		}
	} else if len(apps) > 0 {
		var ok bool
		tuples, ok = confirmInstalled(&local, out, apps, global)
		if !ok {
			return 1
		}
	}
	if len(tuples) == 0 {
		return 0
	}
	explicit := !(len(apps) == 1 && apps[0] == "*") && !all
	var states []update.StatusView
	for _, t := range tuples {
		app := t[0].(string)
		g := t[1].(bool)
		st := local.AppStatusFor(app, g)
		info, _ := local.InstallInfoFor(app, st.Version, g)
		view := update.StatusView{
			App: app, Global: g, Installed: st.Installed, Failed: st.Failed,
			Hold: st.Hold, Version: st.Version, LatestVersion: st.LatestVersion,
		}
		if info != nil {
			view.PinURL = info.URL
			view.PinBucket = info.Bucket
		}
		states = append(states, view)
	}
	targets := update.SelectTargets(states, force, cfg.forceUpdate, cfg.updateNightly, !explicit,
		func(s update.StatusView) {
			Warnf(out, "'%s' is held to version %s", s.App, s.Version)
		},
		func(s update.StatusView) {
			if s.Failed {
				Errorf(out, "'%s' isn't installed correctly.", s.App)
				return
			}
			fmt.Fprintf(out, "%s: %s (latest version)\n", s.App, s.Version)
		},
		func(s update.StatusView) {
			Infof(out, "Please reinstall it or fix the manifest.")
		})
	if len(targets) > 1 {
		fmt.Fprintf(out, "Updating %d outdated apps:\n", len(targets))
	} else if len(targets) == 0 {
		fmt.Fprintf(out, "Latest versions for all apps are installed! For more information try 'scoop status'\n")
		return 0
	} else {
		fmt.Fprintf(out, "Updating one outdated app:\n")
	}
	targets = update.SortTargets(targets)
	targets = attachUpdatePins(&local, out, targets, tuples, pins)
	stamp := time.Now()
	suggested := map[string]bool{}
	var plans []*updatePlan
	var warm install.Transaction
	for _, t := range targets {
		plan, err := resolveUpdateTarget(&local, out, engine, cfg, t, force, quiet, stamp)
		if err != nil {
			if err == errAlreadyInstalled {
				continue
			}
			fmt.Fprintln(out, err.Error())
			continue
		}
		plans = append(plans, plan)
		plan.independent = independent
		warm.Ops = append(warm.Ops, install.Op{App: t.App, Version: plan.version, Architecture: plan.arch, Global: t.Global, Bucket: plan.bucket, URL: plan.url, ManifestRaw: plan.raw})
		for _, dep := range planDeps(&local, out, plan, t) {
			warm.Ops = append(warm.Ops, dep)
		}
	}
	// Phase 1: parallel downloads for the whole batch.
	if mutateDownloader != nil && len(warm.Ops) > 0 {
		warmEx := &install.Executor{Env: local.mutateEnv(), Log: install.Logger{Out: out, Err: out}, Downloader: mutateDownloader}
		_ = warmEx.DownloadAll(context.Background(), warm, cfg.maxDownloads)
	}
	// Phase 2: strictly sequential per-app updates.
	code := 0
	for _, plan := range plans {
		if err := updateOneApp(&local, out, cfg, plan, independent, force, quiet, suggested); err != nil {
			fmt.Fprintln(out, err.Error())
			code = 1
		}
	}
	return code
}

func filterScoop(apps []string) []string {
	var out []string
	for _, a := range apps {
		if update.ParseAppSpec(a).App == "scoop" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// updatePins maps bare app names to @version pins from CLI specs, so
// resolveUpdateTarget can resolve them from bucket git history.
func updatePins(apps []string) map[string]string {
	pins := map[string]string{}
	for _, a := range apps {
		spec := update.ParseAppSpec(a)
		if spec.Version != "" {
			pins[spec.App] = spec.Version
		}
	}
	return pins
}

// attachUpdatePins applies @version pins to selected targets and adds
// explicit targets for pinned apps that outdated selection skipped. Held
// pins report and skip; pins only take effect for installed apps, which
// confirmInstalled already verified.
func attachUpdatePins(env *Env, out io.Writer, targets []update.Target, tuples [][2]any, pins map[string]string) []update.Target {
	if len(pins) == 0 {
		return targets
	}
	selected := map[string]bool{}
	for i := range targets {
		if v, ok := pins[targets[i].App]; ok && targets[i].Pin == "" {
			targets[i].Pin = v
		}
		selected[targets[i].App] = true
	}
	for _, t := range tuples {
		app := t[0].(string)
		g := t[1].(bool)
		v, ok := pins[app]
		if !ok || selected[app] {
			continue
		}
		selected[app] = true
		st := env.AppStatusFor(app, g)
		if st.Hold {
			Warnf(out, "'%s' is held to version %s", app, st.Version)
			continue
		}
		targets = append(targets, update.Target{App: app, Global: g, Current: st.Version, Pin: v})
	}
	return update.SortTargets(targets)
}

// confirmInstalled mirrors Confirm-InstallationStatus
// (lib/core.ps1:1167-1204): unique apps without scoop, scope-checked
// with cross-scope hints, failed installs reported.
func confirmInstalled(env *Env, out io.Writer, apps []string, global bool) ([][2]any, bool) {
	var tuples [][2]any
	ok := true
	for _, spec := range uniqueArgs(apps) {
		parsed := update.ParseAppSpec(spec)
		if parsed.App == "scoop" {
			continue
		}
		app := parsed.App
		_, localErr := os.Stat(env.AppDir(app, false))
		_, globalErr := os.Stat(env.AppDir(app, true))
		if global {
			if localErr == nil && globalErr != nil {
				// Installed locally; fall through to the local scope
				// is wrong under --global, report like classic.
				Errorf(out, "'%s' isn't installed globally, but it may be installed locally.", app)
				Warnf(out, "Try again without the --global (or -g) flag instead.")
				ok = false
				continue
			}
			if globalErr != nil {
				Errorf(out, "'%s' isn't installed.", app)
				ok = false
				continue
			}
			tuples = append(tuples, [2]any{app, true})
		} else {
			if globalErr == nil && localErr != nil {
				Errorf(out, "'%s' isn't installed locally, but it may be installed globally.", app)
				Warnf(out, "Try again with the --global (or -g) flag instead.")
				ok = false
				continue
			}
			if localErr != nil {
				Errorf(out, "'%s' isn't installed.", app)
				ok = false
				continue
			}
			tuples = append(tuples, [2]any{app, false})
		}
		if env.Failed(app, global) {
			Errorf(out, "'%s' isn't installed correctly.", app)
			ok = false
		}
	}
	return tuples, ok
}

// syncScoopCore updates the gscoop binary by channel plus the classic
// scoop checkout best-effort. A held core skips both halves.
func syncScoopCore(env *Env, out io.Writer, engine gitengine.GitEngine, cfg *updateSettings, now time.Time) {
	if update.CheckCoreHold(cfg.store, out, now) {
		return
	}
	fmt.Fprintln(out, "Updating Scoop...")
	selfUpdateBinary(env, out, cfg)
	coreDir := filepath.Join(env.ScoopDir, "apps", "scoop", "current")
	if _, err := os.Stat(coreDir); os.IsNotExist(err) {
		return
	}
	err := update.FastForwardClassic(engine, coreDir, cfg.repo, cfg.branch, boolOr(cfg.store, "autostash_on_conflict", false), env.UserManifestsDir(), func(s string) {
		fmt.Fprintln(out, s)
	})
	if err != nil {
		Warnf(out, "classic scoop core not updated: %s", err.Error())
		return
	}
	if cfg.showLog {
		if head, err := engine.Head(coreDir); err == nil && head != "" {
			entries, err := engine.LogSince(coreDir, head, "", nil, 5)
			if err == nil {
				for _, e := range entries {
					fmt.Fprintf(out, "%s %s\n", shortHash(e.Hash), e.Message)
				}
			}
		}
	}
}

// syncBuckets ensures a git-backed main, then pulls every local bucket
// and prints the commit log when configured.
func syncBuckets(env *Env, out io.Writer, engine gitengine.GitEngine, cfg *updateSettings) {
	fmt.Fprintln(out, "Updating Buckets...")
	known := map[string]string{"main": "https://github.com/ScoopInstaller/Main.git"}
	repo := known["main"]
	if err := update.EnsureMainGit(engine, env.BucketsDir(), repo, func(s string) {
		Infof(out, "%s", s)
	}); err != nil {
		Errorf(out, "%s", err.Error())
		return
	}
	names := env.ListBucketNames()
	report := update.SyncBuckets(engine, env.BucketsDir(), names, cfg.showLog, func(s string) {
		fmt.Fprintln(out, s)
	})
	if cfg.showLog {
		for _, b := range report.Buckets {
			for _, e := range b.Log {
				fmt.Fprintf(out, "%s: %s %s\n", b.Name, shortHash(e.Hash), e.Message)
			}
		}
	}
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func boolOr(store *config.Store, key string, def bool) bool {
	if v, ok := store.GetBool(key); ok {
		return v
	}
	return def
}

// updatePlan is one resolved per-app update.
type updatePlan struct {
	target      update.Target
	arch        string
	raw         []byte
	version     string
	bucket      string
	url         string
	spec        string
	nightly     bool
	independent bool
}

// resolveUpdateTarget reuses the install architecture, bucket, and url,
// resolves the manifest (HEAD, history pin, or user manifest), and maps
// nightly versions, mirroring update() in scoop-update.ps1:261-302.
func resolveUpdateTarget(env *Env, out io.Writer, engine gitengine.GitEngine, cfg *updateSettings, t update.Target, force, quiet bool, now time.Time) (*updatePlan, error) {
	oldVersion := env.CurrentVersion(t.App, t.Global)
	oldRaw, _ := env.InstalledManifest(t.App, oldVersion, t.Global)
	_ = oldRaw
	info, _ := env.InstallInfoFor(t.App, oldVersion, t.Global)
	arch := env.Arch
	bucket, url := "", ""
	if info != nil {
		arch = info.Architecture
		if arch == "" {
			arch = env.Arch
		}
		bucket, url = info.Bucket, info.URL
	}
	if a, err := FormatArch(arch); err == nil {
		arch = a
	}
	plan := &updatePlan{target: t, arch: arch}
	// A -f pin against a user manifest re-resolves against HEAD.
	if t.Pin == "head" {
		bucket, url = "", ""
	}
	if bucket == "" && url == "" {
		bucket = "main"
	}
	var hit *ManifestHit
	version := ""
	if t.Pin != "" && t.Pin != "head" && bucket != "" {
		raw, ver, err := historyManifest(env, out, engine, cfg, t.App, bucket, t.Pin)
		if err != nil {
			return nil, err
		}
		plan.raw = raw
		plan.bucket = bucket
		plan.spec = bucket + "/" + t.App
		version = ver
	} else {
		if url != "" && bucket == "" {
			hit = env.FindManifest(url, out)
			plan.url = url
			plan.spec = url
		} else {
			hit = env.FindManifest(bucket+"/"+t.App, out)
			plan.bucket = bucket
			plan.spec = bucket + "/" + t.App
		}
		if hit.Manifest == nil || hit.Manifest.Version == "" {
			return nil, fmt.Errorf("No manifest available for '%s'.", t.App)
		}
		version = hit.Manifest.Version
		plan.raw = hit.Raw
	}
	nightly := false
	if version == "nightly" {
		if !quiet {
			Warnf(out, "This is a nightly version. Downloaded files won't be verified.")
		}
		version = update.NightlyDated(now)
		nightly = true
	}
	if !force && oldVersion == version {
		if !quiet {
			Warnf(out, "The latest version of '%s' (%s) is already installed.", t.App, version)
		}
		return nil, errAlreadyInstalled
	}
	plan.version = version
	plan.nightly = nightly
	return plan, nil
}

var errAlreadyInstalled = fmt.Errorf("already installed")

// historyManifest resolves an app@version pin from bucket git history,
// mirroring generate_user_manifest over Find-HistoricalManifest. Shallow
// clones keep the documented limit: pins beyond HEAD fail with
// ErrShallowHistory instead of resolving.
func historyManifest(env *Env, out io.Writer, engine gitengine.GitEngine, cfg *updateSettings, app, bucket, pin string) ([]byte, string, error) {
	root := env.bucketRoot(bucket)
	rel, err := filepath.Rel(root, env.FindBucketDirectory(bucket))
	if err != nil {
		return nil, "", err
	}
	if rel == "." {
		rel = ""
	}
	Infof(out, "Resolving historical manifest for '%s' (%s)", app, pin)
	raw, _, err := update.FindHistoricalManifest(engine, root, rel, app, pin, cfg.useGitHistory)
	if err != nil {
		return nil, "", fmt.Errorf("Could not find manifest for '%s@%s': %w", app, pin, err)
	}
	m := parseManifestBytes(raw)
	if m == nil || m.Version == "" {
		return nil, "", fmt.Errorf("No manifest available for '%s'.", app)
	}
	return raw, m.Version, nil
}

// planDeps expands missing dependencies into warm ops for one plan.
// The sequential install phase re-resolves them in order.
func planDeps(env *Env, out io.Writer, plan *updatePlan, t update.Target) []install.Op {
	if plan.independent {
		return nil
	}
	spec := plan.spec
	deps, err := env.ResolveDepends(spec, plan.arch, out)
	if err != nil {
		return nil
	}
	var ops []install.Op
	for _, dep := range deps {
		name := update.StripBucket(dep)
		if name == t.App || dep == spec {
			continue
		}
		if env.Installed(name, nil) {
			continue
		}
		hit := env.FindManifest(dep, out)
		if hit.Manifest == nil {
			continue
		}
		ver := hit.Manifest.Version
		if ver == "nightly" {
			ver = update.NightlyDated(time.Now())
		}
		ops = append(ops, install.Op{App: hit.Name, Version: ver, Architecture: plan.arch, Global: t.Global, Bucket: hit.Bucket, URL: hit.URL, ManifestRaw: hit.Raw})
	}
	return ops
}

// updateOneApp runs the per-app reinstall: running-process guard,
// old-manifest hooks plus uninstaller, shim and env teardown, the
// force-archive move, then the install pipeline for the new version
// with missing dependencies unless independent. Mirrors update() in
// scoop-update.ps1:261-395.
func updateOneApp(env *Env, out io.Writer, cfg *updateSettings, plan *updatePlan, independent, force, quiet bool, suggested map[string]bool) error {
	t := plan.target
	version := plan.version
	fmt.Fprintf(out, "Updating '%s' (%s -> %s)\n", t.App, t.Current, version)
	if err := install.CheckRunning(env.AppDir(t.App, t.Global), cfg.ignoreRunning, nil); err != nil {
		fmt.Fprintln(out, "Running process detected, skip updating.")
		return nil
	}
	fmt.Fprintln(out, "Downloading new version")
	oldVersion := env.CurrentVersion(t.App, t.Global)
	oldManifest, oldRaw := env.InstalledManifest(t.App, oldVersion, t.Global)
	oldHit := &ManifestHit{Name: t.App, Manifest: oldManifest, Raw: oldRaw}
	_ = oldHit
	dir := env.VersionDir(t.App, oldVersion, t.Global)
	persistDir := env.PersistDir(t.App, t.Global)
	vars := hook.Vars{
		Dir: dir, OriginalDir: dir, PersistDir: persistDir, Version: oldVersion,
		Architecture: plan.arch, Global: t.Global, ScoopDir: env.ScoopDir, ScoopGlobal: env.GlobalDir,
	}
	runner := &hook.Runner{Dir: dir, Out: out, Err: out}
	oldHooks := parseHookFields(oldRaw)
	if oldHooks.PreUninstall != "" {
		if err := runner.RunScript(context.Background(), hook.TypePreUninstall, vars, oldHooks.PreUninstall); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Uninstalling '%s' (%s)\n", t.App, oldVersion)
	if err := runUninstaller(context.Background(), out, runner, dir, vars, oldHooks.Uninstaller); err != nil {
		return err
	}
	removeAppShims(env, out, t.App, oldManifest, plan.arch, t.Global)
	refdir := dir
	if !env.NoJunction {
		refdir = junction.UnlinkCurrent(env.AppDir(t.App, t.Global), dir, func(s string) {
			fmt.Fprintln(out, s)
		})
	}
	install.UninstallPSModule(env.BaseDir(t.Global), oldHooks.PSModule, func(s string) {
		fmt.Fprintln(out, s)
	})
	removeShortcuts(menuDir(env, t.Global), oldManifest, plan.arch, func(s string) {
		fmt.Fprintln(out, s)
	})
	install.RemoveEnv(envAddPathFor(oldManifest, plan.arch), envSetFor(oldManifest, plan.arch), refdir, t.Global, nil)
	if force && oldVersion == version {
		if err := archiveVersionDir(env.AppDir(t.App, t.Global), oldVersion); err != nil {
			return err
		}
	}
	if oldHooks.PostUninstall != "" {
		if err := runner.RunScript(context.Background(), hook.TypePostUninstall, vars, oldHooks.PostUninstall); err != nil {
			return err
		}
	}
	if mutateDownloader == nil {
		return fmt.Errorf("scoop update: download backend is not available in this build.")
	}
	spec := plan.spec
	iex := &install.Executor{
		Env:        env.mutateEnv(),
		Log:        install.Logger{Out: out, Err: out},
		Downloader: mutateDownloader,
		Extractor:  mutateExtractor,
		Hooks:      &hook.Runner{Dir: "", Out: out, Err: out},
		LookupManifest: func(app, arch string) (*install.ManifestView, error) {
			if plan.raw != nil && t.Pin != "" && t.Pin != "head" {
				if m := parseManifestBytes(plan.raw); m != nil {
					return mutateView(&ManifestHit{Name: t.App, Manifest: m, Raw: plan.raw}, arch, version), nil
				}
			}
			hit := env.FindManifest(spec, out)
			if hit.Manifest == nil {
				return nil, fmt.Errorf("Couldn't find manifest for '%s'.", app)
			}
			return mutateView(hit, arch, version), nil
		},
	}
	if independent {
		tx := install.Transaction{Ops: []install.Op{{App: t.App, Version: version, Architecture: plan.arch, Global: t.Global, Bucket: plan.bucket, URL: plan.url, ManifestRaw: plan.raw}}}
		return iex.Install(context.Background(), tx)
	}
	deps, err := env.ResolveDepends(spec, plan.arch, out)
	if err != nil {
		return err
	}
	var tx install.Transaction
	for _, dep := range deps {
		name := update.StripBucket(dep)
		if name == t.App || update.StripBucket(spec) == dep {
			continue
		}
		if env.Installed(name, nil) {
			continue
		}
		hit := env.FindManifest(dep, out)
		if hit.Manifest == nil {
			continue
		}
		ver := hit.Manifest.Version
		if ver == "nightly" {
			ver = update.NightlyDated(time.Now())
		}
		tx.Ops = append(tx.Ops, install.Op{App: hit.Name, Version: ver, Architecture: plan.arch, Global: t.Global, Bucket: hit.Bucket, URL: hit.URL, ManifestRaw: hit.Raw})
	}
	tx.Ops = append(tx.Ops, install.Op{App: t.App, Version: version, Architecture: plan.arch, Global: t.Global, Bucket: plan.bucket, URL: plan.url, ManifestRaw: plan.raw})
	_ = suggested
	return iex.Install(context.Background(), tx)
}

// releaseAsset is one GitHub release artifact.
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// releaseInfo is the subset of the GitHub releases API used here.
type releaseInfo struct {
	Tag        string         `json:"tag_name"`
	PreRelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

// gscoopReleaseAPI names the releases endpoint. Tests point it at a
// local server; production uses the GitHub API for gscoopRepo.
var gscoopReleaseAPI = "https://api.github.com"

// errNoRelease reports an empty channel without warning the user.
var errNoRelease = fmt.Errorf("no release found")

// currentBinaryVersion marks this build for the semver check. The
// release pipeline stamps it at link time.
var currentBinaryVersion = "v0.0.0"

// selfUpdateBinary checks the release channel for a newer gscoop
// binary and swaps it into place. Network or selection failures warn
// and continue; the bucket and classic updates never depend on it.
func selfUpdateBinary(env *Env, out io.Writer, cfg *updateSettings) {
	latest, asset, err := latestRelease(cfg.gscoopRepo, cfg.channel, cfg.store.GitHubToken())
	if err != nil {
		if err == errNoRelease {
			return
		}
		Warnf(out, "gscoop release check failed: %s", err.Error())
		return
	}
	if !update.ShouldSelfUpdate(currentBinaryVersion, latest, cfg.channel) {
		return
	}
	fmt.Fprintf(out, "Updating gscoop (%s -> %s)\n", currentBinaryVersion, latest)
	if asset == "" {
		Warnf(out, "No binary asset for %s/%s in %s.", runtime.GOOS, runtime.GOARCH, latest)
		return
	}
	if err := fetchAndSwap(asset, cfg.store.GitHubToken()); err != nil {
		Warnf(out, "gscoop self-update failed: %s", err.Error())
		return
	}
	Successf(out, "gscoop was updated to %s.", latest)
}

// latestRelease returns the newest tag plus the asset matching this
// platform. Stable skips pre-releases; nightly takes the newest
// pre-release, mirroring GSCOOP_CHANNEL semantics.
func latestRelease(repo, channel, token string) (string, string, error) {
	req, err := http.NewRequest("GET", gscoopReleaseAPI+"/repos/"+repo+"/releases", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("releases API returned %d", resp.StatusCode)
	}
	var releases []releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", "", err
	}
	wantPre := strings.EqualFold(channel, "nightly")
	for _, rel := range releases {
		if rel.PreRelease != wantPre {
			continue
		}
		if rel.Tag == "" {
			continue
		}
		return rel.Tag, pickAsset(rel.Assets), nil
	}
	return "", "", errNoRelease
}

// pickAsset selects the first asset naming this OS and architecture.
func pickAsset(assets []releaseAsset) string {
	wantOS, wantArch := strings.ToLower(runtime.GOOS), strings.ToLower(runtime.GOARCH)
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, wantOS) && strings.Contains(name, wantArch) {
			return a.URL
		}
	}
	return ""
}

// fetchAndSwap downloads the asset next to the running binary (same
// volume for the rename) and swaps it into place.
func fetchAndSwap(assetURL, token string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", assetURL, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), "gscoop-new-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	_ = tmp.Close()
	if err := os.Chmod(tmpName, 0o755); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return update.SwapExecutable(exe, tmpName)
}

// archiveVersionDir moves a force-reinstalled version dir to
// _$version.old, with an index when taken (scoop-update.ps1:365-375).
func archiveVersionDir(appDir, version string) error {
	src := filepath.Join(appDir, version)
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	dst := filepath.Join(appDir, "_"+version+".old")
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return os.Rename(src, dst)
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s(%d)", dst, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return os.Rename(src, candidate)
		}
	}
}

// hookFields carries the raw manifest hook and installer sections used
// by the update teardown without touching the shared Manifest shape.
type hookFields struct {
	PreUninstall  string
	PostUninstall string
	Uninstaller   installerFields
	PSModule      string
}

type installerFields struct {
	File   string
	Args   []string
	Script string
	Keep   bool
}

func parseHookFields(raw []byte) hookFields {
	var doc struct {
		PreUninstall  any `json:"pre_uninstall"`
		PostUninstall any `json:"post_uninstall"`
		Uninstaller   *struct {
			File   string `json:"file"`
			Args   any    `json:"args"`
			Script any    `json:"script"`
			Keep   bool   `json:"keep"`
		} `json:"uninstaller"`
		PSModule any `json:"psmodule"`
	}
	var out hookFields
	if err := json.Unmarshal(raw, &doc); err != nil {
		return out
	}
	out.PreUninstall = hook.JoinScript(doc.PreUninstall)
	out.PostUninstall = hook.JoinScript(doc.PostUninstall)
	if doc.Uninstaller != nil {
		out.Uninstaller.File = doc.Uninstaller.File
		out.Uninstaller.Keep = doc.Uninstaller.Keep
		out.Uninstaller.Script = hook.JoinScript(doc.Uninstaller.Script)
		switch t := doc.Uninstaller.Args.(type) {
		case string:
			out.Uninstaller.Args = []string{t}
		case []any:
			for _, a := range t {
				if s, ok := a.(string); ok {
					out.Uninstaller.Args = append(out.Uninstaller.Args, s)
				}
			}
		}
	}
	switch t := doc.PSModule.(type) {
	case string:
		out.PSModule = t
	case map[string]any:
		if name, ok := t["name"].(string); ok {
			out.PSModule = name
		}
	}
	return out
}

// runUninstaller executes the old manifest uninstaller file plus args
// or script, mirroring Invoke-Installer uninstall handling.
func runUninstaller(ctx context.Context, out io.Writer, runner *hook.Runner, dir string, vars hook.Vars, inst installerFields) error {
	if inst.File == "" && len(inst.Args) == 0 && inst.Script == "" {
		return nil
	}
	if inst.File != "" || len(inst.Args) > 0 {
		name := inst.File
		prog := filepath.Join(dir, name)
		if _, err := os.Stat(prog); err != nil {
			return fmt.Errorf("Uninstaller %s is missing.", prog)
		}
		args := hook.SubstituteAll(inst.Args, vars)
		if strings.HasSuffix(strings.ToLower(prog), ".ps1") {
			if err := runner.RunPS1File(ctx, hook.TypeUninstaller, vars, prog, args); err != nil {
				return fmt.Errorf("Uninstallation aborted.")
			}
		} else {
			if err := hook.RunExecutable(ctx, out, "Running uninstaller ...", prog, args); err != nil {
				return fmt.Errorf("Uninstallation aborted.")
			}
			if !inst.Keep {
				_ = os.Remove(prog)
			}
		}
	}
	if inst.Script == "" {
		return nil
	}
	if err := runner.RunScript(ctx, hook.TypeUninstaller, vars, inst.Script); err != nil {
		return fmt.Errorf("Uninstallation aborted.")
	}
	return nil
}

// removeAppShims deletes shims owned by the old manifest bins.
func removeAppShims(env *Env, out io.Writer, app string, m *Manifest, arch string, global bool) {
	if m == nil {
		return
	}
	shimDir := env.ShimDir(global)
	for _, item := range m.BinList(arch) {
		entry, ok := binEntryOf(item)
		if !ok {
			continue
		}
		for _, line := range shim.RemoveShim(shimDir, entry.Name, app) {
			fmt.Fprintln(out, line)
		}
	}
}

func binEntryOf(item any) (shim.Entry, bool) {
	switch t := item.(type) {
	case string:
		return shim.Entry{Target: t, Name: shim.ShimName(stripExe(t))}, true
	case []any:
		return shim.ParseEntry(t)
	default:
		return shim.Entry{}, false
	}
}

func stripExe(p string) string {
	base := p
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[:i]
	}
	return base
}

// removeShortcuts deletes menu shortcuts for the old manifest.
func removeShortcuts(menuDir string, m *Manifest, arch string, emit func(string)) {
	if m == nil {
		return
	}
	for _, s := range shortcutEntries(m.ShortcutsList(arch)) {
		path := filepath.Join(menuDir, s.Name+".lnk")
		if _, err := os.Stat(path); err == nil {
			emit("Removing shortcut " + path)
			_ = os.Remove(path)
		}
	}
}

func menuDir(env *Env, global bool) string {
	return filepath.Join(env.BaseDir(global), "scoop-apps-menu")
}

// envAddPathFor picks arch-specific env_add_path with top-level fallback.
func envAddPathFor(m *Manifest, arch string) []string {
	if m == nil {
		return nil
	}
	if a, ok := m.Architecture[arch]; ok && a.EnvAddPath != nil {
		return anyToStrings(a.EnvAddPath)
	}
	return anyToStrings(m.EnvAddPath)
}

// envSetFor picks arch-specific env_set with top-level fallback.
func envSetFor(m *Manifest, arch string) map[string]string {
	if m == nil {
		return nil
	}
	if a, ok := m.Architecture[arch]; ok && a.EnvSet != nil {
		return stringMap(a.EnvSet)
	}
	return stringMap(m.EnvSet)
}

func stringMap(v map[string]any) map[string]string {
	if v == nil {
		return nil
	}
	out := make(map[string]string, len(v))
	for k, item := range v {
		if s, ok := item.(string); ok {
			out[k] = s
		}
	}
	return out
}
