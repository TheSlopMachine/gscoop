package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/install"
)

// RunDownload mirrors scoop-download.ps1 flag surface:
// -f/--force, -s/--skip-hash-check, -u/--no-update-scoop,
// -a/--arch. It resolves each app, delegates fetch to the download
// seam, and verifies through that layer. No fetch logic lives here.
func RunDownload(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "fsua:", []string{"force", "skip-hash-check", "no-update-scoop", "arch="})
	if r.Err != "" {
		Errorf(out, "scoop download: %s", r.Err)
		return 1
	}
	if len(r.Rest) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "download")
		return 1
	}
	arch := env.Arch
	if req := r.Get("a") + r.Get("arch"); req != "" {
		a, err := FormatArch(req)
		if err != nil {
			fmt.Fprintf(out, "ERROR: %s\n", err.Error())
			return 1
		}
		arch = a
	}
	force := r.Has("f") || r.Has("force")
	skipHash := r.Has("s") || r.Has("skip-hash-check")
	noUpdate := r.Has("u") || r.Has("no-update-scoop")
	if !noUpdate {
		// Auto-update before download stays unwired; proceed against local buckets.
	}
	if mutateDownloader == nil {
		Errorf(out, "scoop download: download backend is not wired in this build.")
		return 1
	}
	if force {
		Warnf(out, "Cache is being ignored.")
	}
	failed := false
	for _, spec := range uniqueArgs(r.Rest) {
		hit := env.FindManifest(spec, out)
		if hit.Manifest == nil {
			Errorf(out, "Couldn't find manifest for '%s'.", spec)
			failed = true
			continue
		}
		Infof(out, "Downloading '%s' [%s]", spec, arch)
		if force {
			removeCacheEntries(env.CacheDir, hit.Name)
		}
		if skipHash {
			Infof(out, "Skipping hash verification.")
		}
		op := install.Op{App: hit.Name, Version: hit.Manifest.Version, Architecture: arch, ManifestRaw: hit.Raw}
		if _, err := mutateDownloader.Download(context.Background(), op, env.CacheDir); err != nil {
			fmt.Fprintln(out, err.Error())
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// removeCacheEntries deletes `<app>#*` files for one app to force redownload.
func removeCacheEntries(cacheDir, app string) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	prefix := app + "#"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		_ = os.Remove(filepath.Join(cacheDir, entry.Name()))
	}
}

// RunImport mirrors scoop-import.ps1: configs apply first, then
// buckets add, then apps install with arch and hold replay. Scoopfile
// parsing stays local; installs delegate to RunInstall.
func RunImport(env *Env, out io.Writer, scoopfile string) int {
	data, raw, err := readScoopfile(scoopfile)
	if err != nil {
		Errorf(out, "Input file not a valid JSON.")
		return 1
	}
	if data == nil {
		Errorf(out, "Input file not a valid JSON.")
		return 1
	}
	var doc struct {
		Config  map[string]any    `json:"config"`
		Buckets []scoopfileBucket `json:"buckets"`
		Apps    []scoopfileApp    `json:"apps"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		Errorf(out, "Input file not a valid JSON.")
		return 1
	}
	Infof(out, "Importing %s.", scoopfile)
	failed := false
	if len(doc.Config) > 0 {
		store, err := config.Load(env.ConfigPath)
		if err != nil {
			Errorf(out, "Could not load config: %s", err.Error())
			return 1
		}
		for _, name := range sortedConfigKeys(doc.Config) {
			value := doc.Config[name]
			if err := store.Set(name, value); err != nil {
				Errorf(out, "Could not set config: %s", err.Error())
				failed = true
				continue
			}
			fmt.Fprintf(out, "'%s' has been set to '%s'\n", name, configDisplay(value))
		}
		if err := store.Save(); err != nil {
			Errorf(out, "Could not save config: %s", err.Error())
			return 1
		}
	}
	existing := map[string]bool{}
	for _, name := range env.ListBucketNames() {
		existing[strings.ToLower(name)] = true
	}
	var bucketNames []string
	for _, b := range doc.Buckets {
		if b.Name == "" {
			continue
		}
		bucketNames = append(bucketNames, b.Name)
		if existing[strings.ToLower(b.Name)] {
			continue
		}
		if code := RunBucketAdd(env, out, b.Name, b.Source, knownBuckets); code != 0 {
			Warnf(out, "Failed to add bucket '%s'.", b.Name)
			continue
		}
		existing[strings.ToLower(b.Name)] = true
	}
	defArch := env.Arch
	for _, item := range doc.Apps {
		if item.Name == "" && item.Source == "" {
			continue
		}
		spec := importAppSource(item, bucketNames)
		if spec == "" {
			continue
		}
		global := infoHas(item.Info, "Global install")
		archArg := ""
		for _, a := range []string{"64bit", "32bit", "arm64"} {
			if infoHas(item.Info, a) && a != defArch {
				archArg = a
				break
			}
		}
		installArgs := []string{}
		if global {
			installArgs = append(installArgs, "-g")
		}
		if archArg != "" {
			installArgs = append(installArgs, "--arch", archArg)
		}
		installArgs = append(installArgs, spec)
		if code := RunInstall(env, out, installArgs); code != 0 {
			failed = true
			continue
		}
		if infoHas(item.Info, "Held package") {
			if code := RunHold(env, out, []string{item.Name}, global); code != 0 {
				failed = true
			}
		}
	}
	if failed {
		return 1
	}
	return 0
}

// sortedConfigKeys returns config keys in stable order.
func sortedConfigKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// configDisplay renders a scoopfile config value for the set confirmation.
func configDisplay(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// infoHas reports whether the comma-separated Info field contains entry.
func infoHas(info, entry string) bool {
	for _, part := range strings.Split(info, ", ") {
		if strings.EqualFold(strings.TrimSpace(part), entry) {
			return true
		}
	}
	return false
}
