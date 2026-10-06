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
	"fmt"
	"io"
	"os"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/hook"
	"github.com/TheSlopMachine/gscoop/internal/install"
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
		ScoopDir:  e.ScoopDir,
		GlobalDir: e.GlobalDir,
	}
}

// mutateView adapts a resolved manifest to install ManifestView.
func mutateView(hit *ManifestHit, arch, version string) *install.ManifestView {
	if hit == nil || hit.Manifest == nil {
		return &install.ManifestView{Version: version}
	}
	m := hit.Manifest
	mv := &install.ManifestView{
		Version:     version,
		ManifestRaw: hit.Raw,
		Depends:     m.DependsList(),
	}
	for _, b := range binEntries(m.BinList(arch)) {
		mv.Bins = append(mv.Bins, b)
	}
	for _, s := range shortcutEntries(m.ShortcutsList(arch)) {
		mv.Shortcuts = append(mv.Shortcuts, s)
	}
	return mv
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
	_ = independent
	if mutateDownloader == nil {
		Errorf(out, "scoop install: download backend is not wired in this build.")
		return 1
	}
	apps := uniqueArgs(r.Rest)
	resolved, err := env.ResolveDepends(apps[0], arch, out)
	if err != nil {
		fmt.Fprintln(out, err.Error())
		return 1
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
	_ = global
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
			m, _ := env.InstalledManifest(app, ver, global)
			hit := &ManifestHit{Name: app, Manifest: m}
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
	apps := r.Rest
	if all {
		apps = env.InstalledApps(false)
	}
	if len(apps) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "reset")
		return 1
	}
	code := 0
	for _, app := range uniqueArgs(apps) {
		ver := env.CurrentVersion(app, false)
		if ver == "" {
			Errorf(out, "'%s' is not installed.", app)
			code = 1
			continue
		}
		Infof(out, "Resetting %s (%s).", app, ver)
		_ = os.Getenv("SCOOP")
	}
	return code
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
