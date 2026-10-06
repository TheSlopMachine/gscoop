// Swap, doctor, and Phase 2B dispatch for Phase 4
// (internal/commands/mutate_swap_*.go).
//
// RunUnswap restores the classic scoop shim trio from the
// scoop.classic.* backups written by the gscoop manifest post_install.
// RunDoctor runs the internal/doctor checks and prints checkup-style
// output. RunPhase2B wires the Phase 2B runners (install, uninstall,
// reset, download, import, bucket, hold, unhold) into the main dispatch.
// No emojis.
package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/doctor"
	"github.com/TheSlopMachine/gscoop/internal/state"
)

// knownBuckets mirrors C:\devel\Scoop\buckets.json, the known-bucket
// registry used for the short `scoop bucket add <name>` form.
var knownBuckets = map[string]string{
	"main":         "https://github.com/ScoopInstaller/Main",
	"extras":       "https://github.com/ScoopInstaller/Extras",
	"versions":     "https://github.com/ScoopInstaller/Versions",
	"nirsoft":      "https://github.com/ScoopInstaller/Nirsoft",
	"sysinternals": "https://github.com/ScoopInstaller/Sysinternals",
	"php":          "https://github.com/ScoopInstaller/PHP",
	"nerd-fonts":   "https://github.com/matthewjberger/scoop-nerd-fonts",
	"nonportable":  "https://github.com/ScoopInstaller/Nonportable",
	"java":         "https://github.com/ScoopInstaller/Java",
	"games":        "https://github.com/ScoopInstaller/Games",
}

// OwnsPhase2B reports whether name is a Phase 2B command wired here:
// install, uninstall, reset, download, import, bucket, hold, unhold.
func OwnsPhase2B(name string) bool {
	switch name {
	case "install", "uninstall", "reset", "download", "import", "bucket", "hold", "unhold":
		return true
	default:
		return false
	}
}

// RunPhase2B dispatches one Phase 2B command. It returns the exit code
// and true when name is owned here, or 0 and false otherwise.
func RunPhase2B(env *Env, out io.Writer, name string, args []string) (int, bool) {
	switch name {
	case "install":
		return RunInstall(env, out, args), true
	case "uninstall":
		return RunUninstall(env, out, args), true
	case "reset":
		return RunReset(env, out, args), true
	case "download":
		return RunDownload(env, out, args), true
	case "import":
		return runImportCmd(env, out, args), true
	case "bucket":
		return RunBucket(env, out, args), true
	case "hold":
		return runHoldCmd(env, out, args, true), true
	case "unhold":
		return runHoldCmd(env, out, args, false), true
	default:
		return 0, false
	}
}

// runImportCmd mirrors the libexec/scoop-import.ps1 argument surface: one
// positional scoopfile path or URL.
func runImportCmd(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 {
		Errorf(out, "<path/url to scoopfile.json> missing")
		printUsage(out, "import")
		return 1
	}
	return RunImport(env, out, args[0])
}

// runHoldCmd parses -g/--global around the shared hold and unhold
// runners, mirroring scoop-hold.ps1 and scoop-unhold.ps1.
func runHoldCmd(env *Env, out io.Writer, args []string, hold bool) int {
	r := cli.GetOpt(args, "g", []string{"global"})
	prefix := "scoop hold"
	if !hold {
		prefix = "scoop unhold"
	}
	if r.Err != "" {
		Errorf(out, "%s: %s", prefix, r.Err)
		return 1
	}
	global := r.Has("g") || r.Has("global")
	if hold {
		return RunHold(env, out, r.Rest, global)
	}
	return RunUnhold(env, out, r.Rest, global)
}

// RunBucket mirrors libexec/scoop-bucket.ps1: subcommands add, list,
// known, rm with the known-bucket short form for add.
func RunBucket(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 {
		Errorf(out, "scoop bucket: cmd '' not supported")
		printUsage(out, "bucket")
		return 1
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		if len(rest) == 0 {
			Errorf(out, "<name> missing")
			fmt.Fprintln(out, "usage: scoop bucket add <name> [<repo>]")
			return 1
		}
		name, repo := rest[0], ""
		if len(rest) > 1 {
			repo = rest[1]
		}
		if repo == "" {
			if known, ok := knownBuckets[strings.ToLower(name)]; ok {
				repo = known
			} else {
				Errorf(out, "Unknown bucket '%s'. Try specifying <repo>.", name)
				fmt.Fprintln(out, "usage: scoop bucket add <name> [<repo>]")
				return 1
			}
		}
		return RunBucketAdd(env, out, name, repo, knownBuckets)
	case "rm":
		if len(rest) == 0 {
			Errorf(out, "<name> missing")
			fmt.Fprintln(out, "usage: scoop bucket rm <name>")
			return 1
		}
		return RunBucketRm(env, out, rest[0])
	case "list":
		buckets := env.ListBuckets()
		if len(buckets) == 0 {
			Warnf(out, "No bucket found. Please run 'scoop bucket add main' to add the default 'main' bucket.")
			return 2
		}
		table := make([][]string, 0, len(buckets))
		for _, b := range buckets {
			table = append(table, []string{b.Name, b.Source, b.Updated, fmt.Sprint(b.Manifests)})
		}
		renderTable(out, []string{"Name", "Source", "Updated", "Manifests"}, table)
		return 0
	case "known":
		names := make([]string, 0, len(knownBuckets))
		for n := range knownBuckets {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintln(out, n)
		}
		return 0
	default:
		Errorf(out, "scoop bucket: cmd '%s' not supported", sub)
		printUsage(out, "bucket")
		return 1
	}
}

// RunDoctor runs the internal/doctor checks over the resolved roots and
// prints checkup-style output. Findings print as WARN lines,
// informational results as INFO lines; the command always exits 0.
func RunDoctor(env *Env, out io.Writer, _ []string) int {
	rep := doctor.Check(doctor.Options{
		Roots: state.Roots{
			Scoop:  env.ScoopDir,
			Global: env.GlobalDir,
			Cache:  env.CacheDir,
		},
		NoJunction: env.NoJunction,
	})
	for _, f := range rep.Findings {
		if f.Issue {
			Warnf(out, "%s", f.Message)
			continue
		}
		Infof(out, "%s", f.Message)
	}
	if n := rep.Issues(); n > 0 {
		Warnf(out, "Found %d potential %s.", n, pluralize(n, "problem", "problems"))
	} else {
		Successf(out, "No problems identified!")
	}
	return 0
}

// swapExtensions lists the shim triple extensions managed by the gscoop
// manifest post_install and restored by unswap.
var swapExtensions = []string{".exe", ".shim", ".cmd", ".ps1"}

// RunUnswap restores the classic scoop shim trio from the scoop.classic.*
// backups. With no backups present it fails closed instead of leaving
// the scoop command unowned.
func RunUnswap(env *Env, out io.Writer, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(out, "Usage: gscoop unswap")
		return 1
	}
	restored := 0
	for _, global := range []bool{false, true} {
		dir := env.ShimDir(global)
		for _, ext := range swapExtensions {
			cur := filepath.Join(dir, "scoop"+ext)
			bak := filepath.Join(dir, "scoop"+doctor.SwapBackupSuffix+ext)
			if _, err := os.Stat(bak); err != nil {
				continue
			}
			_ = os.Remove(cur)
			if err := os.Rename(bak, cur); err != nil {
				Errorf(out, "Failed to restore '%s'.", bak)
				return 1
			}
			Infof(out, "Restored '%s'.", cur)
			restored++
		}
	}
	if restored == 0 {
		Errorf(out, "No scoop.classic.* backup found; nothing to restore.")
		return 1
	}
	Successf(out, "Classic scoop shim restored. Run the switch-back drill in MIGRATION.md to verify.")
	return 0
}
