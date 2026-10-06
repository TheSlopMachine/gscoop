// Rendered help text for the 28 Scoop subcommands.
//
// Each body is the verbatim classic "Help:" content with the leading "#",
// one optional space, and the "Help:" prefix removed, mirroring the
// scoop_help function in lib/help.ps1. Help(name) renders the same output as
// "scoop help <cmd>": the Usage line, a blank line, then the body.
// Commands without a Help header (depends, help, home, prefix) render only
// the Usage line. Bodies match testdata/cli/<cmd>.txt byte for byte
// (modulo CRLF normalization). No emojis.
package cli

import (
	"sort"
	"strings"
)

// bodies holds per-command help bodies without trailing newline.
// Absent or empty means the command has no Help header.
var bodies = map[string]string{
	"alias": `Available subcommands: add, rm, list.

Aliases are custom Scoop subcommands that can be created to make common tasks easier.

To add an alias:

    scoop alias add <name> <command> [<description>]

e.g.,

    scoop alias add rm 'scoop uninstall $args[0]' 'Uninstall an app'
    scoop alias add upgrade 'scoop update *' 'Update all apps, just like "brew" or "apt"'

To remove an alias:

    scoop alias rm <name>

To list all aliases:

    scoop alias list [-v|--verbose]

Options:
  -v, --verbose  Show alias description and table headers (works only for "list")`,

	"bucket": `Add, list or remove buckets.

Buckets are repositories of apps available to install. Scoop comes with
a default bucket, but you can also add buckets that you or others have
published.

To add a bucket:
    scoop bucket add <name> [<repo>]

e.g.:
    scoop bucket add extras https://github.com/ScoopInstaller/Extras.git

Since the 'extras' bucket is known to Scoop, this can be shortened to:
    scoop bucket add extras

To list all known buckets, use:
    scoop bucket known`,

	"cache": `Scoop caches downloads so you don't need to download the same files
when you uninstall and re-install the same version of an app.

You can use
    scoop cache show
to see what's in the cache, and
    scoop cache rm <app> to remove downloads for a specific app.

To clear everything in your cache, use:
    scoop cache rm *
You can also use the ` + "`-a/--all`" + ` switch in place of ` + "`*`" + ` here`,

	"cat": `Show content of specified manifest.
If configured, ` + "`bat`" + ` will be used to pretty-print the JSON.
See ` + "`cat_style`" + ` in ` + "`scoop help config`" + ` for further information.`,

	"checkup": `Performs a series of diagnostic tests to try to identify things that may
cause problems with Scoop.`,

	"cleanup": `'scoop cleanup' cleans Scoop apps by removing old versions.
'scoop cleanup <app>' cleans up the old versions of that app if said versions exist.

You can use '*' in place of <app> or ` + "`-a`" + `/` + "`--all`" + ` switch to cleanup all apps.

Options:
  -a, --all          Cleanup all apps (alternative to '*')
  -g, --global       Cleanup a globally installed app
  -k, --cache        Remove outdated download cache`,

	"config": `The scoop configuration file is saved at ~/.config/scoop/config.json.

To get all configuration settings:

    scoop config

To get a configuration setting:

    scoop config <name>

To set a configuration setting:

    scoop config <name> <value>

To remove a configuration setting:

    scoop config rm <name>

Settings
--------

use_external_7zip: $true|$false
      External 7zip (from path) will be used for archives extraction.

use_lessmsi: $true|$false
      Prefer lessmsi utility over native msiexec.

use_sqlite_cache: $true|$false
      Use SQLite database for caching. This is useful for speeding up 'scoop search' and 'scoop shim' commands.

use_git_history: $true|$false
      Enable searching for specific versions in git history when installing apps with version specifiers.
      When enabled, Scoop will first search the bucket's git history for the exact version before falling back to autoupdate.
      (Default is $true)

no_junction: $true|$false
      The 'current' version alias will not be used. Shims and shortcuts will point to specific version instead.

scoop_repo: https://github.com/ScoopInstaller/Scoop
      Git repository containing scoop source code.
      This configuration is useful for custom forks.

scoop_branch: master|develop
      Allow to use different branch than master.
      Could be used for testing specific functionalities before released into all users.
      If you want to receive updates earlier to test new functionalities use develop (see: 'https://github.com/ScoopInstaller/Scoop/issues/2939')

proxy: [username:password@]host:port
      By default, Scoop will use the proxy settings from Internet Options, but with anonymous authentication.

      * To use the credentials for the current logged-in user, use 'currentuser' in place of username:password
      * To use the system proxy settings configured in Internet Options, use 'default' in place of host:port
      * An empty or unset value for proxy is equivalent to 'default' (with no username or password)
      * To bypass the system proxy and connect directly, use 'none' (with no username or password)

autostash_on_conflict: $true|$false
      When a conflict is detected during updating, Scoop will auto-stash the uncommitted changes.
      (Default is $false, which will abort the update)

default_architecture: 64bit|32bit|arm64
      Allow to configure preferred architecture for application installation.
      If not specified, architecture is determined by system.

debug: $true|$false
      Additional and detailed output will be shown.

force_update: $true|$false
      Force apps updating to bucket's version.

show_update_log: $true|$false
      Do not show changed commits on 'scoop update'

show_manifest: $true|$false
      Displays the manifest of every app that's about to
      be installed, then asks user if they wish to proceed.

shim: kiennq|scoopcs|71
      Choose scoop shim build.

root_path: $Env:UserProfile\scoop
      Path to Scoop root directory.

global_path: $Env:ProgramData\scoop
      Path to Scoop root directory for global apps.

cache_path:
      For downloads, defaults to 'cache' folder under Scoop root directory.

gh_token:
      GitHub API token used to make authenticated requests.
      This is essential for checkver and similar functions to run without
      incurring rate limits and download from private repositories.

virustotal_api_key:
      API key used for uploading/scanning files using virustotal.
      See: 'https://support.virustotal.com/hc/en-us/articles/115002088769-Please-give-me-an-API-key'

cat_style:
      When set to a non-empty string, Scoop will use 'bat' to display the manifest for
      the ` + "`scoop cat`" + ` command and while doing manifest review. This requires 'bat' to be
      installed (run ` + "`scoop install bat`" + ` to install it), otherwise errors will be thrown.
      The accepted values are the same as ones passed to the --style flag of 'bat'.

ignore_running_processes: $true|$false
      When set to $false (default), Scoop would stop its procedure immediately if it detects
      any target app process is running. Procedure here refers to reset/uninstall/update.
      When set to $true, Scoop only displays a warning message and continues procedure.

private_hosts:
      Array of private hosts that need additional authentication.
      For example, if you want to access a private GitHub repository,
      you need to add the host to this list with 'match' and 'headers' strings.

hold_update_until:
      Disable/Hold Scoop self-updates, until the specified date.
      ` + "`scoop hold scoop`" + ` will set the value to one day later.
      Should be in the format 'YYYY-MM-DD', 'YYYY/MM/DD' or any other forms that accepted by '[System.DateTime]::Parse()'.
      Ref: https://docs.microsoft.com/dotnet/api/system.datetime.parse?view=netframework-4.5#StringToParse

update_nightly: $true|$false
      Nightly version is formatted as 'nightly-yyyyMMdd' and will be updated after one day if this is set to $true.
      Otherwise, nightly version will not be updated unless ` + "`--force`" + ` is used.

use_isolated_path: $true|$false|[string]
      When set to $true, Scoop will use ` + "`SCOOP_PATH`" + ` environment variable to store apps' ` + "`PATH`" + `s.
      When set to arbitrary non-empty string, Scoop will use that string as the environment variable name instead.
      This is useful when you want to isolate Scoop from the system ` + "`PATH`" + `.

ARIA2 configuration
-------------------

aria2-enabled: $true|$false
      Aria2c will be used for downloading of artifacts.
      (Default is $true)

aria2-warning-enabled: $true|$false
      Disable Aria2c warning which is shown while downloading.
      (Default is $true)

aria2-fallback-enabled: $true|$false
      Automatically falls back to the default downloader when Aria2c download fails.
      (Default is $true)

aria2-retry-wait: 2
      Number of seconds to wait between retries.
      See: 'https://aria2.github.io/manual/en/html/aria2c.html#cmdoption-retry-wait'

aria2-split: 5
      Number of connections used for download.
      See: 'https://aria2.github.io/manual/en/html/aria2c.html#cmdoption-s'

aria2-max-connection-per-server: 5
      The maximum number of connections to one server for each download.
      See: 'https://aria2.github.io/manual/en/html/aria2c.html#cmdoption-x'

aria2-min-split-size: 5M
      Downloaded files will be split by this configured size and downloaded using multiple connections.
      See: 'https://aria2.github.io/manual/en/html/aria2c.html#cmdoption-k'

aria2-options:
      Array of additional aria2 options.
      See: 'https://aria2.github.io/manual/en/html/aria2c.html#options'`,

	"create": `Create your own custom app manifest`,

	"download": `e.g. The usual way to download an app, without installing it (uses your local 'buckets'):
     scoop download git

To download a different version of the app
(note that this will auto-generate the manifest using current version):
     scoop download gh@2.7.0

To download an app from a manifest at a URL:
     scoop download https://raw.githubusercontent.com/ScoopInstaller/Main/master/bucket/runat.json

To download an app from a manifest on your computer
     scoop download path\to\app.json

Options:
  -f, --force                     Force download (overwrite cache)
  -s, --skip-hash-check           Skip hash verification (use with caution!)
  -u, --no-update-scoop           Don't update Scoop before downloading if it's outdated
  -a, --arch <32bit|64bit|arm64>  Use the specified architecture, if the app supports it`,

	"export": `Options:
  -c, --config       Export the Scoop configuration file too`,

	"hold": `To hold a user-scoped app:
     scoop hold <app>

To hold a global app:
     scoop hold -g <app>

Options:
  -g, --global  Hold globally installed apps`,

	"import": `To replicate a Scoop installation from a file stored on Desktop, run
     scoop import Desktop\scoopfile.json`,

	"info": `Options:
  -v, --verbose   Show full paths and URLs`,

	"install": `e.g. The usual way to install an app (uses your local 'buckets'):
     scoop install git

To install a different version of the app
(will search sqlite cache if enabled, then git history (if use_git_history is $true), then auto-generate manifest as fallback):
     scoop install gh@2.7.0

To install an app from a manifest at a URL:
     scoop install https://raw.githubusercontent.com/ScoopInstaller/Main/master/bucket/runat.json

To install a different version of the app from a URL:
      scoop install https://raw.githubusercontent.com/ScoopInstaller/Main/master/bucket/neovim.json@0.9.0

To install an app from a manifest on your computer
     scoop install \path\to\app.json

To install an app from a manifest on your computer
     scoop install \path\to\app.json@version

Options:
  -g, --global                    Install the app globally
  -i, --independent               Don't install dependencies automatically
  -k, --no-cache                  Don't use the download cache
  -s, --skip-hash-check           Skip hash validation (use with caution!)
  -u, --no-update-scoop           Don't update Scoop before installing if it's outdated
  -a, --arch <32bit|64bit|arm64>  Use the specified architecture, if the app supports it`,

	"list": `Lists all installed apps, or the apps matching the supplied query.`,

	"reset": `Used to resolve conflicts in favor of a particular app. For example,
if you've installed 'python' and 'python27', you can use 'scoop reset' to switch between
using one or the other.

You can use '*' in place of <app> or ` + "`-a`" + `/` + "`--all`" + ` switch to reset all apps.`,

	"search": `Searches for apps that are available to install.

If used with [query], shows app names that match the query.
  - With 'use_sqlite_cache' enabled, [query] is partially matched against app names, binaries, and shortcuts.
  - Without 'use_sqlite_cache', [query] can be a regular expression to match against app names and binaries.
Without [query], shows all the available apps.`,

	"shim": `Available subcommands: add, rm, list, info, alter.

To add a custom shim, use the 'add' subcommand:

    scoop shim add <shim_name> <command_path> [[--|--%] <args>...]

To remove shims, use the 'rm' subcommand: (CAUTION: this could remove shims added by an app manifest)

    scoop shim rm <shim_name> [<shim_name>...]

To list all shims or matching shims, use the 'list' subcommand:

    scoop shim list [<regex_pattern>...]

To show a shim's information, use the 'info' subcommand:

    scoop shim info <shim_name>

To alternate a shim's target source, use the 'alter' subcommand:

    scoop shim alter <shim_name>

Options:
  -g, --global       Manipulate global shim(s)

HINT: The FIRST terminator token ('--' or PowerShell '--%'), if any, is treated as the option
terminator and will NOT be included; everything after it is passed to the shim.
So if you want to pass arguments like '-g' or '--global' to the shim, put them after a '--' or '--%'.
Examples:
    POSIX-style:    scoop shim add myapp 'D:\path\myapp.exe' '--' myapp_args --global
    PowerShell:     scoop shim add myapp D:\path\myapp.exe --% myapp_args --global
Notes:
  - In PowerShell, '--' should be quoted to avoid parsing.
  - '--%' disables PowerShell parsing of the remainder; pass literals as needed.`,

	"status": `Options:
  -l, --local         Checks the status for only the locally installed apps,
                      and disables remote fetching/checking for Scoop and buckets`,

	"uninstall": `e.g. scoop uninstall git

Options:
  -g, --global   Uninstall a globally installed app
  -p, --purge    Remove all persistent data`,

	"unhold": `To unhold a user-scoped app:
     scoop unhold <app>

To unhold a global app:
     scoop unhold -g <app>

Options:
  -g, --global  Unhold globally installed apps`,

	"update": `'scoop update' updates Scoop to the latest version.
'scoop update <app>' installs a new version of that app, if there is one.

You can use '*' in place of <app> to update all apps.

Options:
  -f, --force            Force update even when there isn't a newer version
  -g, --global           Update a globally installed app
  -i, --independent      Don't install dependencies automatically
  -k, --no-cache         Don't use the download cache
  -s, --skip-hash-check  Skip hash validation (use with caution!)
  -q, --quiet            Hide extraneous messages
  -a, --all              Update all apps (alternative to '*')`,

	"virustotal": `Look for app's hash or url on virustotal.com

Use a single '*' or the '-a/--all' switch to check all installed apps.

To use this command, you have to sign up to VirusTotal's community,
and get an API key. Then, tell scoop about your API key with:

  scoop config virustotal_api_key <your API key: 64 lower case hex digits>

Exit codes:
 0 -> success
 1 -> problem parsing arguments
 2 -> at least one package was marked unsafe by VirusTotal
 4 -> at least one exception was raised while looking for info
 8 -> at least one package couldn't be queried because the manifest couldn't be found
16 -> VirusTotal API key is not configured
Note: the exit codes (2, 4 & 8) may be combined, e.g. 6 -> exit codes
      2 & 4 combined

Options:
  -a, --all                 Check for all installed apps
  -s, --scan                For packages where VirusTotal has no information, send download URL
                            for analysis (and future retrieval). This requires you to configure
                            your virustotal_api_key.
  -n, --no-depends          By default, all dependencies are checked too. This flag avoids it.
  -u, --no-update-scoop     Don't update Scoop before checking if it's outdated
  -p, --passthru            Return reports as objects`,

	"which": `Locate the path to a shim/executable that was installed with Scoop (similar to 'which' on Linux)`,
}

// Help renders the "scoop help <cmd>" output: the Usage line, a blank line,
// then the body. It returns false for unknown commands.
func Help(name string) (string, bool) {
	cmd := Lookup(name)
	if cmd == nil {
		return "", false
	}
	out := "Usage: " + cmd.Usage + "\n"
	if body, ok := bodies[name]; ok && body != "" {
		out += "\n" + body + "\n"
	}
	return out, true
}

// RunHelp routes the "scoop help" subcommand (libexec/scoop-help.ps1:30-42):
// no args prints the top-level listing, a known command prints its help,
// and an unknown command reports "ERROR scoop help: no such command '<cmd>'"
// with exit 1.
func RunHelp(args []string) (string, int) {
	if len(args) == 0 {
		return TopHelp(), 0
	}
	text, ok := Help(args[0])
	if !ok {
		return "ERROR scoop help: no such command '" + args[0] + "'\n", 1
	}
	return text, 0
}

// TopHelp renders the "scoop help" listing: the fixed preamble plus one
// "<command> - <summary>" line per command in alphabetical order.
func TopHelp() string {
	var b strings.Builder
	b.WriteString("Usage: scoop <command> [<args>]\n")
	b.WriteString("\nAvailable commands are listed below.\n")
	b.WriteString("\nType 'scoop help <command>' to get more help for a specific command.\n")
	names := Names()
	sort.Strings(names)
	for _, n := range names {
		b.WriteString("\n  " + n + " - " + Lookup(n).Summary)
	}
	b.WriteString("\n")
	return b.String()
}
