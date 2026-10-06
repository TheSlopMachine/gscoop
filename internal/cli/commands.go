// Command metadata for the 28 Scoop subcommands. Usage and Summary strings
// are verbatim copies of the classic libexec headers. Lookup matches names
// case-insensitively. The listing covers the 28 classic subcommands only;
// doctor and unswap dispatch as extras in cmd/gscoop. No emojis.
package cli

import "strings"

// Command describes one subcommand's CLI surface.
type Command struct {
	// Name is the subcommand name, without the "scoop-" prefix.
	Name string
	// Usage is the header text after "# Usage: ".
	Usage string
	// Summary is the header text after "# Summary: ".
	Summary string
	// ShortOpts is the getopt shortopts string. Empty when the command does
	// not use getopt.
	ShortOpts string
	// LongOpts is the getopt longopts list. Nil when the command does not
	// use getopt.
	LongOpts []string
	// HasGetOpt reports whether the command parses options with getopt.
	HasGetOpt bool
	// ErrPrefix is the "scoop <cmd>" prefix used in getopt error reports.
	// Empty when the command has no getopt error path.
	ErrPrefix string
}

// Commands lists all 28 subcommands in alphabetical order.
var Commands = []Command{
	{
		Name:      "alias",
		Usage:     "scoop alias <subcommand> [options] [<args>]",
		Summary:   "Manage scoop aliases",
		ShortOpts: "v",
		LongOpts:  []string{"verbose"},
		HasGetOpt: true,
		ErrPrefix: "scoop alias",
	},
	{
		Name:    "bucket",
		Usage:   "scoop bucket add|list|known|rm [<args>]",
		Summary: "Manage Scoop buckets",
	},
	{
		Name:    "cache",
		Usage:   "scoop cache show|rm [app(s)]",
		Summary: "Show or clear the download cache",
	},
	{
		Name:    "cat",
		Usage:   "scoop cat <app>",
		Summary: "Show content of specified manifest.",
	},
	{
		Name:    "checkup",
		Usage:   "scoop checkup",
		Summary: "Check for potential problems",
	},
	{
		Name:      "cleanup",
		Usage:     "scoop cleanup <app> [options]",
		Summary:   "Cleanup apps by removing old versions",
		ShortOpts: "agk",
		LongOpts:  []string{"all", "global", "cache"},
		HasGetOpt: true,
		ErrPrefix: "scoop cleanup",
	},
	{
		Name:    "config",
		Usage:   "scoop config [rm] name [value]",
		Summary: "Get or set configuration values",
	},
	{
		Name:    "create",
		Usage:   "scoop create <url>",
		Summary: "Create a custom app manifest",
	},
	{
		Name:      "depends",
		Usage:     "scoop depends <app>",
		Summary:   "List dependencies for an app, in the order they'll be installed",
		ShortOpts: "a:",
		LongOpts:  []string{"arch="},
		HasGetOpt: true,
		ErrPrefix: "",
	},
	{
		Name:      "download",
		Usage:     "scoop download <app> [options]",
		Summary:   "Download apps in the cache folder and verify hashes",
		ShortOpts: "fsua:",
		LongOpts:  []string{"force", "skip-hash-check", "no-update-scoop", "arch="},
		HasGetOpt: true,
		ErrPrefix: "scoop download",
	},
	{
		Name:    "export",
		Usage:   "scoop export > scoopfile.json",
		Summary: "Exports installed apps, buckets (and optionally configs) in JSON format",
	},
	{
		Name:    "help",
		Usage:   "scoop help <command>",
		Summary: "Show help for a command",
	},
	{
		Name:      "hold",
		Usage:     "scoop hold <apps>",
		Summary:   "Hold an app to disable updates",
		ShortOpts: "g",
		LongOpts:  []string{"global"},
		HasGetOpt: true,
		ErrPrefix: "scoop hold",
	},
	{
		Name:    "home",
		Usage:   "scoop home <app>",
		Summary: "Opens the app homepage",
	},
	{
		Name:    "import",
		Usage:   "scoop import <path/url to scoopfile.json>",
		Summary: "Imports apps, buckets and configs from a Scoopfile in JSON format",
	},
	{
		Name:      "info",
		Usage:     "scoop info <app> [options]",
		Summary:   "Display information about an app",
		ShortOpts: "v",
		LongOpts:  []string{"verbose"},
		HasGetOpt: true,
		ErrPrefix: "scoop info",
	},
	{
		Name:      "install",
		Usage:     "scoop install <app> [options]",
		Summary:   "Install apps",
		ShortOpts: "giksua:",
		LongOpts:  []string{"global", "independent", "no-cache", "skip-hash-check", "no-update-scoop", "arch="},
		HasGetOpt: true,
		ErrPrefix: "scoop install",
	},
	{
		Name:    "list",
		Usage:   "scoop list [query]",
		Summary: "List installed apps",
	},
	{
		Name:    "prefix",
		Usage:   "scoop prefix <app>",
		Summary: "Returns the path to the specified app",
	},
	{
		Name:      "reset",
		Usage:     "scoop reset <app>",
		Summary:   "Reset an app to resolve conflicts",
		ShortOpts: "a",
		LongOpts:  []string{"all"},
		HasGetOpt: true,
		ErrPrefix: "scoop reset",
	},
	{
		Name:    "search",
		Usage:   "scoop search <query>",
		Summary: "Search available apps",
	},
	{
		Name:      "shim",
		Usage:     "scoop shim <subcommand> [<shim_name>...] [options] [other_args]",
		Summary:   "Manipulate Scoop shims",
		ShortOpts: "g",
		LongOpts:  []string{"global"},
		HasGetOpt: true,
		ErrPrefix: "scoop shim",
	},
	{
		Name:    "status",
		Usage:   "scoop status",
		Summary: "Show status and check for new app versions",
	},
	{
		Name:      "uninstall",
		Usage:     "scoop uninstall <app> [options]",
		Summary:   "Uninstall an app",
		ShortOpts: "gp",
		LongOpts:  []string{"global", "purge"},
		HasGetOpt: true,
		ErrPrefix: "scoop uninstall",
	},
	{
		Name:      "unhold",
		Usage:     "scoop unhold <app>",
		Summary:   "Unhold an app to enable updates",
		ShortOpts: "g",
		LongOpts:  []string{"global"},
		HasGetOpt: true,
		ErrPrefix: "scoop unhold",
	},
	{
		Name:      "update",
		Usage:     "scoop update <app> [options]",
		Summary:   "Update apps, or Scoop itself",
		ShortOpts: "gfiksqa",
		LongOpts:  []string{"global", "force", "independent", "no-cache", "skip-hash-check", "quiet", "all"},
		HasGetOpt: true,
		ErrPrefix: "scoop update",
	},
	{
		Name:      "virustotal",
		Usage:     "scoop virustotal [* | app1 app2 ...] [options]",
		Summary:   "Look for app's hash or url on virustotal.com",
		ShortOpts: "asnup",
		LongOpts:  []string{"all", "scan", "no-depends", "no-update-scoop", "passthru"},
		HasGetOpt: true,
		ErrPrefix: "scoop virustotal",
	},
	{
		Name:    "which",
		Usage:   "scoop which <command>",
		Summary: "Locate a shim/executable (similar to 'which' on Linux)",
	},
}

// Lookup returns the Command for name, or nil for unknown names.
// Comparison is case-insensitive.
func Lookup(name string) *Command {
	for i := range Commands {
		if strings.EqualFold(Commands[i].Name, name) {
			return &Commands[i]
		}
	}
	return nil
}

// Names returns all subcommand names in alphabetical order.
func Names() []string {
	names := make([]string, 0, len(Commands))
	for _, c := range Commands {
		names = append(names, c.Name)
	}
	return names
}

// IsHelpFlag reports whether arg is a dispatch-level help flag:
// "-h", "--help", or "/?" (bin/scoop.ps1:18, bin/scoop.ps1:43).
func IsHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "/?"
}
