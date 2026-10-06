// Command wiring for install, uninstall, and reset mutations.
//
// These are the only command files Phase 2B may create
// (internal/commands/mutate_*.go). Existing dispatch stays untouched.
// Each runner parses flags per spec/cli-surface.md, resolves
// dependencies in install order, and delegates filesystem mutation to
// internal/install through its Downloader and Extractor seams so no
// download or extraction logic duplicates here. No emojis.
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/hook"
	"github.com/TheSlopMachine/gscoop/internal/install"
	"github.com/TheSlopMachine/gscoop/internal/junction"
	"github.com/TheSlopMachine/gscoop/internal/shim"
)

// mutateDownloader is the seam to the download layer owned outside
// Phase 2B. It stays nil until wired; runners report the gap instead
// of duplicating fetch logic.
var mutateDownloader install.Downloader

// mutateExtractor is the seam to the extraction layer owned outside
// Phase 2B. It stays nil until wired.
var mutateExtractor install.Extractor

// SetMutationSeams wires download and extraction backends. Later
// phases call it once; tests inject fakes.
func SetMutationSeams(d install.Downloader, e install.Extractor) {
	mutateDownloader = d
	mutateExtractor = e
}

// mutateEnv adapts commands Env to install Env.
func (e *Env) mutateEnv() install.Env {
	return install.Env{
		ScoopDir:   e.ScoopDir,
		GlobalDir:  e.GlobalDir,
		NoJunction: e.NoJunction,
	}
}

// mutateView adapts a resolved manifest to install ManifestView.
// Architecture-specific sections override top-level values, matching
// arch_specific resolution in lib/manifest.ps1.
func mutateView(hit *ManifestHit, arch, version string) *install.ManifestView {
	if hit == nil || hit.Manifest == nil {
		return &install.ManifestView{Version: version}
	}
	m := hit.Manifest
	mv := &install.ManifestView{
		Version:       version,
		ManifestRaw:   hit.Raw,
		Depends:       m.DependsList(),
		PreInstall:    hook.JoinScript(mutateRawProp(hit.Raw, arch, "pre_install")),
		PostInstall:   hook.JoinScript(mutateRawProp(hit.Raw, arch, "post_install")),
		PreUninstall:  hook.JoinScript(mutateRawProp(hit.Raw, arch, "pre_uninstall")),
		PostUninstall: hook.JoinScript(mutateRawProp(hit.Raw, arch, "post_uninstall")),
		Installer:     mutateInstallerView(mutateRawProp(hit.Raw, arch, "installer")),
		Uninstaller:   mutateInstallerView(mutateRawProp(hit.Raw, arch, "uninstaller")),
		EnvAddPath:    anyToStrings(m.EnvAddPathFor(arch)),
		EnvSet:        stringMap(m.EnvSetFor(arch)),
		Persist:       mutatePersistViews(mutateRawProp(hit.Raw, arch, "persist")),
		PSModuleName:  mutatePSModuleName(mutateRawProp(hit.Raw, arch, "psmodule")),
		Notes:         strings.Join(anyToStrings(m.Notes), "\n"),
		Suggest:       mutateSuggest(m.Suggest),
	}
	for _, b := range binEntries(m.BinList(arch)) {
		mv.Bins = append(mv.Bins, b)
	}
	for _, s := range shortcutEntries(m.ShortcutsList(arch)) {
		mv.Shortcuts = append(mv.Shortcuts, s)
	}
	return mv
}

// mutateRawProp returns the arch-specific value for prop, falling back
// to the top-level value. Invalid input returns nil.
func mutateRawProp(raw []byte, arch, prop string) any {
	if len(raw) == 0 {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	if arch != "" {
		if archRoot, ok := doc["architecture"].(map[string]any); ok {
			if section, ok := archRoot[arch].(map[string]any); ok {
				if v, ok := section[prop]; ok && v != nil {
					return v
				}
			}
		}
	}
	return doc[prop]
}

// mutateInstallerView converts a raw installer/uninstaller section to
// an install view. Args accept string or string list; script accepts
// string or string list.
func mutateInstallerView(v any) install.InstallerView {
	obj, ok := v.(map[string]any)
	if !ok {
		return install.InstallerView{}
	}
	var iv install.InstallerView
	if file, ok := obj["file"].(string); ok {
		iv.File = file
	}
	switch args := obj["args"].(type) {
	case string:
		iv.Args = []string{args}
	case []any:
		for _, item := range args {
			if s, ok := item.(string); ok {
				iv.Args = append(iv.Args, s)
			}
		}
	}
	iv.Script = hook.JoinScript(obj["script"])
	if keep, ok := obj["keep"].(bool); ok {
		iv.Keep = keep
	}
	return iv
}

// mutatePersistViews converts a raw persist section to install views.
// Strings map to same-name pairs; pairs pass through.
func mutatePersistViews(v any) []install.PersistView {
	var items []any
	switch t := v.(type) {
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

// mutatePSModuleName reads the psmodule name from string or object form.
func mutatePSModuleName(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if name, ok := t["name"].(string); ok {
			return name
		}
	}
	return ""
}

// mutateSuggest normalizes suggest values to string lists.
func mutateSuggest(raw map[string]any) map[string][]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for k, v := range raw {
		if list := anyToStrings(v); len(list) > 0 {
			out[k] = list
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func binEntries(list []any) []shim.Entry {
	var out []shim.Entry
	for _, item := range list {
		switch t := item.(type) {
		case string:
			out = append(out, shim.Entry{Target: t})
		case []any:
			if e, ok := shim.ParseEntry(t); ok {
				out = append(out, e)
			}
		}
	}
	return out
}

func shortcutEntries(list []any) []install.ShortcutView {
	var out []install.ShortcutView
	for _, item := range list {
		parts, ok := item.([]any)
		if !ok || len(parts) < 2 {
			continue
		}
		texts := make([]string, 0, len(parts))
		for _, p := range parts {
			s, ok := p.(string)
			if !ok {
				texts = nil
				break
			}
			texts = append(texts, s)
		}
		if texts == nil || len(texts) < 2 {
			continue
		}
		s := install.ShortcutView{Target: texts[0], Name: texts[1]}
		if len(texts) > 2 {
			s.Args = texts[2]
		}
		if len(texts) > 3 {
			s.Icon = texts[3]
		}
		out = append(out, s)
	}
	return out
}

// RunInstall mirrors libexec/scoop-install.ps1 flag surface:
// -g/--global, -i/--independent, -k/--no-cache, -s/--skip-hash-check,
// -u/--no-update-scoop, -a/--arch.
func RunInstall(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "giksua:", []string{"global", "independent", "no-cache", "skip-hash-check", "no-update-scoop", "arch="})
	if r.Err != "" {
		Errorf(out, "scoop install: %s", r.Err)
		return 1
	}
	if len(r.Rest) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "install")
		return 1
	}
	global := r.Has("g") || r.Has("global")
	// -k/--no-cache, -s/--skip-hash-check, and -u/--no-update-scoop stay
	// accepted for CLI parity. install.Op and the Downloader seam carry no
	// per-op cache or hash toggles, so the backend keeps its defaults until
	// the seam gains per-op options.
	arch := env.Arch
	if req := r.Get("a") + r.Get("arch"); req != "" {
		a, err := FormatArch(req)
		if err != nil {
			fmt.Fprintf(out, "ERROR: %s\n", err.Error())
			return 1
		}
		arch = a
	}
	independent := r.Has("i") || r.Has("independent")
	if mutateDownloader == nil {
		Errorf(out, "scoop install: download backend is not wired in this build.")
		return 1
	}
	apps := uniqueArgs(r.Rest)
	var resolved []string
	if independent {
		resolved = apps
	} else {
		seen := map[string]bool{}
		for _, app := range apps {
			deps, err := env.ResolveDepends(app, arch, out)
			if err != nil {
				fmt.Fprintln(out, err.Error())
				return 1
			}
			for _, dep := range deps {
				if !seen[dep] {
					seen[dep] = true
					resolved = append(resolved, dep)
				}
			}
		}
	}
	iex := &install.Executor{
		Env:        env.mutateEnv(),
		Log:        install.Logger{Out: out, Err: out},
		Downloader: mutateDownloader,
		Extractor:  mutateExtractor,
		Hooks:      &hook.Runner{Dir: "", Out: out, Err: out},
		LookupManifest: func(app, arch string) (*install.ManifestView, error) {
			hit := env.FindManifest(app, out)
			if hit.Manifest == nil {
				return nil, fmt.Errorf("Couldn't find manifest for '%s'.", app)
			}
			version := hit.Manifest.Version
			if version == "" {
				version = "0.0.0"
			}
			return mutateView(hit, arch, version), nil
		},
	}
	var tx install.Transaction
	for _, spec := range resolved {
		hit := env.FindManifest(spec, out)
		if hit.Manifest == nil {
			fmt.Fprintf(out, "Couldn't find manifest for '%s'.\n", spec)
			return 1
		}
		tx.Ops = append(tx.Ops, install.Op{App: hit.Name, Version: hit.Manifest.Version, Architecture: arch, Global: global, ManifestRaw: hit.Raw})
	}
	if err := iex.Install(context.Background(), tx); err != nil {
		fmt.Fprintln(out, err.Error())
		return 1
	}
	return 0
}

// RunUninstall mirrors libexec/scoop-uninstall.ps1: -g/--global,
// -p/--purge, reverse pipeline per op.
func RunUninstall(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "gp", []string{"global", "purge"})
	if r.Err != "" {
		Errorf(out, "scoop uninstall: %s", r.Err)
		return 1
	}
	if len(r.Rest) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "uninstall")
		return 1
	}
	global := r.Has("g") || r.Has("global")
	purge := r.Has("p") || r.Has("purge")
	iex := &install.Executor{
		Env:   env.mutateEnv(),
		Log:   install.Logger{Out: out, Err: out},
		Hooks: &hook.Runner{Out: out, Err: out},
		LookupManifest: func(app, arch string) (*install.ManifestView, error) {
			ver := env.CurrentVersion(app, global)
			m, raw := env.InstalledManifest(app, ver, global)
			hit := &ManifestHit{Name: app, Manifest: m, Raw: raw}
			return mutateView(hit, env.Arch, ver), nil
		},
	}
	for _, spec := range uniqueArgs(r.Rest) {
		app := spec
		if !env.Installed(app, nil) {
			Errorf(out, "'%s' is not installed.", app)
			continue
		}
		ver := env.CurrentVersion(app, global)
		if ver == "" {
			Errorf(out, "'%s' is not installed.", app)
			continue
		}
		if err := install.CheckRunning(env.AppDir(app, global), false, nil); err != nil {
			fmt.Fprintln(out, err.Error())
			continue
		}
		if err := iex.Uninstall(context.Background(), app, ver, global, purge); err != nil {
			fmt.Fprintln(out, err.Error())
			return 1
		}
	}
	return 0
}

// RunReset mirrors scoop-reset.ps1: -a/--all resets every app by
// relinking current plus shims. Failures report per app.
func RunReset(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "a", []string{"all"})
	if r.Err != "" {
		Errorf(out, "scoop reset: %s", r.Err)
		return 1
	}
	all := r.Has("a") || r.Has("all")
	type target struct {
		app    string
		global bool
	}
	var targets []target
	if all || (len(r.Rest) == 1 && r.Rest[0] == "*") {
		for _, app := range env.InstalledApps(false) {
			targets = append(targets, target{app, false})
		}
		for _, app := range env.InstalledApps(true) {
			targets = append(targets, target{app, true})
		}
	} else {
		for _, app := range uniqueArgs(r.Rest) {
			if app == "scoop" {
				continue
			}
			if env.Installed(app, boolPtr(true)) {
				targets = append(targets, target{app, true})
			} else {
				targets = append(targets, target{app, false})
			}
		}
	}
	if len(targets) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "reset")
		return 1
	}
	code := 0
	for _, t := range targets {
		ver := env.CurrentVersion(t.app, t.global)
		if ver == "" {
			Errorf(out, "'%s' is not installed.", t.app)
			code = 1
			continue
		}
		versionDir := env.VersionDir(t.app, ver, t.global)
		if fi, err := os.Stat(versionDir); err != nil || !fi.IsDir() {
			Errorf(out, "'%s (%s)' isn't installed.", t.app, ver)
			code = 1
			continue
		}
		if t.global && !install.IsAdmin() {
			Warnf(out, "'%s' (%s) is a global app. You need admin rights to reset it. Skipping.", t.app, ver)
			continue
		}
		Infof(out, "Resetting %s (%s).", t.app, ver)
		if !env.NoJunction {
			if _, err := junction.LinkCurrent(env.AppDir(t.app, t.global), versionDir, func(s string) {
				fmt.Fprintln(out, s)
			}); err != nil {
				Errorf(out, "Could not reset '%s': %s", t.app, err.Error())
				code = 1
				continue
			}
		}
		if err := resetShims(env, out, t.app, ver, t.global); err != nil {
			Errorf(out, "Could not reset '%s': %s", t.app, err.Error())
			code = 1
			continue
		}
	}
	return code
}

// resetShims recreates shims for the installed version manifest.
func resetShims(env *Env, out io.Writer, app, ver string, global bool) error {
	m, _ := env.InstalledManifest(app, ver, global)
	if m == nil {
		return fmt.Errorf("'%s (%s)' isn't installed.", app, ver)
	}
	arch := env.Arch
	if info, _ := env.InstallInfoFor(app, ver, global); info != nil && info.Architecture != "" {
		arch = info.Architecture
	}
	versionDir := env.VersionDir(app, ver, global)
	vars := hook.Vars{
		Dir: versionDir, OriginalDir: versionDir, PersistDir: env.PersistDir(app, global),
		Version: ver, Architecture: arch, Global: global,
		ScoopDir: env.ScoopDir, ScoopGlobal: env.GlobalDir,
	}
	return install.CreateShims(env.ShimDir(global), versionDir, binEntries(m.BinList(arch)), versionDir, vars, "", func(s string) {
		fmt.Fprintln(out, s)
	})
}

func uniqueArgs(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
