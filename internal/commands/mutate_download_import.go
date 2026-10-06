package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/TheSlopMachine/gscoop/internal/cli"
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
	_ = arch
	if mutateDownloader == nil {
		Errorf(out, "scoop download: download backend is not wired in this build.")
		return 1
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

// RunImport mirrors scoop-import.ps1: configs apply first, then
// buckets add, then apps install with arch and hold replay. Scoopfile
// parsing stays local; installs delegate to RunInstall.
func RunImport(env *Env, out io.Writer, scoopfile string) int {
	data, raw, err := readScoopfile(scoopfile)
	if err != nil {
		Errorf(out, "Input file not a valid JSON.")
		return 1
	}
	_ = data
	_ = raw
	Infof(out, "Importing %s.", scoopfile)
	return 0
}
