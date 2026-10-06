# CLI surface snapshot (Phase 0C)

Base tree for all citations: `C:\devel\Scoop`. Each claim cites `file:line`.
Decisions: Go 1.22 LTS, `CGO_ENABLED=0`, local-only module `gscoop` (`go.mod`).
Scope: dispatch rules, getopt semantics, all 28 command surfaces, exit codes.
Help bodies are verbatim: rendered `scoop help <cmd>` output equals the golden files
under `testdata/cli/<cmd>.txt` after CRLF-to-LF normalization. No emojis in code or output.

## 1. Dispatch (`bin/scoop.ps1:9-52`)

- `$subCommand` is `$Args[0]` (`bin/scoop.ps1:9`).
- `$subCommand` in `$null, '-h', '--help', '/?'` runs `help` with no arguments,
  which prints the top-level listing (`bin/scoop.ps1:17-20`,
  `libexec/scoop-help.ps1:30-36`).
- `$subCommand` in `'-v', '--version'` prints `Current Scoop version:`, then the newest
  `## [vX]` line from `CHANGELOG.md` (or `git log HEAD -1 --oneline` when git is
  available, a `.git` checkout exists, and `SCOOP_BRANCH` is not `master`), then one
  `'<bucket>' bucket:` + `git log HEAD -1 --oneline` block per git-backed local bucket
  (`bin/scoop.ps1:21-40`).
- `$subCommand` in `commands` runs `exec <cmd> <rest>`; when the first remaining argument
  is in `'-h', '--help', '/?'` it runs `help <cmd>` instead (`bin/scoop.ps1:41-48`).
- Any other `$subCommand` prints `WARN  scoop: '<sub>' isn't a scoop command. See 'scoop help'.`
  and exits 1 (`bin/scoop.ps1:49-52`, `lib/core.ps1:313`).
- `commands` is the union of `libexec/scoop-*.ps1` basenames and `shims/scoop-*.ps1`
  alias scripts (`lib/commands.ps1:5-16`). Help routing only inspects the first argument
  after the command name; `-h`/`--help`/`/?` elsewhere are ordinary arguments.
- `scoop help` with no command prints `Usage: scoop <command> [<args>]`, the
  `Available commands` preamble, then one `Command`/`Summary` row per command
  (`libexec/scoop-help.ps1:30-36`, `libexec/scoop-help.ps1:15-26`).
- `scoop help <cmd>` prints `usage` output, a blank line, then the `Help:` body
  (`libexec/scoop-help.ps1:5-13`). `usage` renders `Usage: <header text>`
  (`lib/help.ps1:1-3`). `scoop_help` strips the leading `#`, one optional space, and the
  `Help:` prefix from each header line (`lib/help.ps1:9-12`).
- `scoop help <unknown>` prints `ERROR scoop help: no such command '<cmd>'` and exits 1
  (`libexec/scoop-help.ps1:40-41`, `lib/core.ps1:312`).

## 2. getopt semantics (`lib/getopt.ps1:16-80`)

Signature: `getopt($argv, $shortopts, $longopts)` returns option hash, remaining-args
array, error string (`lib/getopt.ps1:10-16`).

- `$shortopts` is a string of single-letter options; a letter followed by `:` takes
  a separate argument (`lib/getopt.ps1:4-6`).
- `$longopts` is an array of names; a name ending with `=` takes a separate argument
  (`lib/getopt.ps1:7-9`).
- The first exact `--` or `--%` token terminates option parsing. Later arguments pass
  through verbatim, even when hyphen-led. The terminator itself is dropped
  (`lib/getopt.ps1:12-15`, `lib/getopt.ps1:35-39`).
- `--<name>` looks up `<name>` in `$longopts`. A trailing-`=` entry consumes the next
  argument as its value; otherwise the option records `$true` (`lib/getopt.ps1:40-54`).
- `-<letters>` scans each letter left to right (bundling). A letter that takes an
  argument must be last in the bundle and consumes the next argument as its value;
  other letters record `$true` (`lib/getopt.ps1:58-75`).
- A lone `-` is a positional argument, never an option (`lib/getopt.ps1:58`).
- Positional arguments accumulate in order; parsing continues after them (no
  permutation stop) until a terminator or the end of input (`lib/getopt.ps1:76-78`).
- Repeated options overwrite; the last occurrence wins (hashtable assignment,
  `lib/getopt.ps1:51-53`, `lib/getopt.ps1:68-70`).
- Parsing stops at the first error; callers discard partial results and exit 1,
  except `scoop depends`, which ignores the error value (edge E1).

Exact error strings (`lib/getopt.ps1:49-56`, `lib/getopt.ps1:65-73`):

```text
Option -<letter> requires an argument.
Option --<name> requires an argument.
Option -<letter> not recognized.
Option --<name> not recognized.
```

Each getopt-based command reports errors as `ERROR scoop <cmd>: <err>` and exits 1,
for example `ERROR scoop install: <err>` (`libexec/scoop-install.ps1:47`,
`lib/core.ps1:312`).

Getopt edge cases (verified against classic source):

- E1: `scoop depends` never checks `$err` (`libexec/scoop-depends.ps1:10-13`).
  Unknown or argument-less options are silently dropped there.
- E2: `--opt=value` is not a value form. `<name>` keeps the `=value` suffix, lookup
  fails, and the parser reports `Option --<name>=<value> not recognized.`
  (`lib/getopt.ps1:41-57`).
- E3: attached short values are rejected. `-a32bit` reports
  `Option -a requires an argument.` because an argument-taking letter must be last in
  its bundle and the value must be the next argument (`lib/getopt.ps1:65-68`).
- E4: matching is case-insensitive. Both `-match` filters run under PowerShell default
  case-insensitive semantics, so `--GLOBAL` matches `global` and `-G` matches `g`
  (`lib/getopt.ps1:43`, `lib/getopt.ps1:62`). The Go port reproduces this with
  case-folding comparison and stores the option under the typed spelling.
- E5: the long-option pattern is built from raw user input (`"^$name=?$"`,
  `lib/getopt.ps1:43`). Input containing regex syntax can match unrelated entries.
  The Go port uses exact case-folded equality instead; no legitimate invocation differs.
- E6: `/?` never reaches getopt as a help flag. It does not start with `-`, so inside
  commands it is positional (typically an app name). `/?` help works only at dispatch
  level and as the first argument after the command (`bin/scoop.ps1:18`,
  `bin/scoop.ps1:43`, `lib/getopt.ps1:58-77`).
- E7: `-h`/`--help` inside a command are ordinary getopt input. No command declares an
  `h`/`help` option, so `scoop install --help` reports
  `Option --help not recognized.` and exits 1. Command help works only through the
  dispatch-level first-argument rule (`bin/scoop.ps1:43-44`).
- E8: `scoop help <cmd>` output is `Usage: ...`, one blank line, then the stripped
  `Help:` body; commands without a `Help:` header (`depends`, `help`, `home`, `prefix`)
  print only the `Usage:` line (`libexec/scoop-help.ps1:5-13`).
- E9: PowerShell-only input kinds (`$null` skipped; array/int/decimal appended to
  remaining args, `lib/getopt.ps1:29-33`) have no Go equivalent. The Go port takes
  `[]string`; empty strings are positional.

## 3. Exit codes

- 0 is success. 1 is the generic and user-error code: `abort` defaults to 1
  (`lib/core.ps1:311`), and every getopt failure, missing-argument failure, unknown
  command, and failed guard uses 1 unless listed below.
- `scoop search` with no matches exits 1 after `WARN  No matches found.`
  (`libexec/scoop-search.ps1:218-223`). Plan reference: no-match is 1 (plan 4.3).
- `scoop bucket add` of an existing name or an already-cloned repo returns 2; git
  missing, invalid repo, and clone failure return 1 (`lib/buckets.ps1:123-170`).
  `scoop bucket list` with no buckets warns and exits 2 (`libexec/scoop-bucket.ps1:57-65`).
- `scoop list` with no installed apps warns and exits 1 (`libexec/scoop-list.ps1:19-22`).
- `scoop which` with no match warns and exits 2 (`libexec/scoop-which.ps1:15-16`).
- `scoop shim` uses 1 (usage/parse failures), 2 (scope mismatch or missing
  alternatives), and 3 (missing command path or shim) (`libexec/scoop-shim.ps1:122-220`).
- `scoop virustotal` combines 0 success, 1 argument parsing, 2 unsafe, 4 exception,
  8 manifest missing, 16 API key missing; 2, 4, and 8 can combine
  (`libexec/scoop-virustotal.ps1:12-20`, `libexec/scoop-virustotal.ps1:60-73`).
- `scoop checkup` and `scoop status` always exit 0; findings are printed, not coded
  (`libexec/scoop-checkup.ps1:58`, `libexec/scoop-status.ps1:85`).
- `scoop create` without a URL prints the `create` help and exits 0
  (`libexec/scoop-create.ps1:61-67`).

## 4. Command table

Columns: getopt `shortopts`, `longopts`, and the `ERROR scoop <cmd>:` prefix.
`manual` means the command parses `$args` directly without `lib/getopt.ps1`;
`positional` means fixed `param()` input.

| Command | Usage | Summary | shortopts | longopts | Error prefix |
|---|---|---|---|---|---|
| alias | scoop alias \<subcommand> [options] [\<args>] | Manage scoop aliases | `v` | `verbose` | scoop alias |
| bucket | scoop bucket add\|list\|known\|rm [\<args>] | Manage Scoop buckets | manual | manual | scoop bucket |
| cache | scoop cache show\|rm [app(s)] | Show or clear the download cache | manual (`-a`/`--all` = `*`) | manual | none |
| cat | scoop cat \<app> | Show content of specified manifest. | positional | positional | none |
| checkup | scoop checkup | Check for potential problems | none | none | none |
| cleanup | scoop cleanup \<app> [options] | Cleanup apps by removing old versions | `agk` | `all`, `global`, `cache` | scoop cleanup |
| config | scoop config [rm] name [value] | Get or set configuration values | manual | manual | none |
| create | scoop create \<url> | Create a custom app manifest | positional | positional | none |
| depends | scoop depends \<app> | List dependencies for an app, in the order they'll be installed | `a:` (\$err ignored) | `arch=` (\$err ignored) | none |
| download | scoop download \<app> [options] | Download apps in the cache folder and verify hashes | `fsua:` | `force`, `skip-hash-check`, `no-update-scoop`, `arch=` | scoop download |
| export | scoop export > scoopfile.json | Exports installed apps, buckets (and optionally configs) in JSON format | manual (`-c`/`--config` first arg) | manual | none |
| help | scoop help \<command> | Show help for a command | positional | positional | scoop help |
| hold | scoop hold \<apps> | Hold an app to disable updates | `g` | `global` | scoop hold |
| home | scoop home \<app> | Opens the app homepage | positional | positional | none |
| import | scoop import \<path/url to scoopfile.json> | Imports apps, buckets and configs from a Scoopfile in JSON format | positional (mandatory) | positional | none |
| info | scoop info \<app> [options] | Display information about an app | `v` | `verbose` | scoop info |
| install | scoop install \<app> [options] | Install apps | `giksua:` | `global`, `independent`, `no-cache`, `skip-hash-check`, `no-update-scoop`, `arch=` | scoop install |
| list | scoop list [query] | List installed apps | positional query (regex) | positional | none |
| prefix | scoop prefix \<app> | Returns the path to the specified app | positional | positional | none |
| reset | scoop reset \<app> | Reset an app to resolve conflicts | `a` | `all` | scoop reset |
| search | scoop search \<query> | Search available apps | positional query (regex) | positional | none |
| shim | scoop shim \<subcommand> [\<shim_name>...] [options] [other_args] | Manipulate Scoop shims | `g` (after subcommand; `--`/`--%` split) | `global` | scoop shim |
| status | scoop status | Show status and check for new app versions | manual (`-l`/`--local` first arg) | manual | none |
| uninstall | scoop uninstall \<app> [options] | Uninstall an app | `gp` | `global`, `purge` | scoop uninstall |
| unhold | scoop unhold \<app> | Unhold an app to enable updates | `g` | `global` | scoop unhold |
| update | scoop update \<app> [options] | Update apps, or Scoop itself | `gfiksqa` | `global`, `force`, `independent`, `no-cache`, `skip-hash-check`, `quiet`, `all` | scoop update |
| virustotal | scoop virustotal [* \| app1 app2 ...] [options] | Look for app's hash or url on virustotal.com | `asnup` | `all`, `scan`, `no-depends`, `no-update-scoop`, `passthru` | scoop virustotal |
| which | scoop which \<command> | Locate a shim/executable (similar to 'which' on Linux) | positional | positional | none |

Getopt call sites: alias (`libexec/scoop-alias.ps1:42`), cleanup
(`libexec/scoop-cleanup.ps1:18`), depends (`libexec/scoop-depends.ps1:10`), download
(`libexec/scoop-download.ps1:33`), hold (`libexec/scoop-hold.ps1:17`), info
(`libexec/scoop-info.ps1:11`), install (`libexec/scoop-install.ps1:46`), reset
(`libexec/scoop-reset.ps1:16`), shim (`libexec/scoop-shim.ps1:54`), uninstall
(`libexec/scoop-uninstall.ps1:19`), unhold (`libexec/scoop-unhold.ps1:17`), update
(`libexec/scoop-update.ps1:32`), virustotal (`libexec/scoop-virustotal.ps1:38`).

## 5. Verbatim Help bodies

Each block is the exact `scoop help <cmd>` body (the `Usage:` line is the golden
file's first line; see `testdata/cli/<cmd>.txt`). `depends`, `help`, `home`, and
`prefix` have no `Help:` header and print only their `Usage:` line (edge E8).

### alias (`libexec/scoop-alias.ps1:1-25`)

```text
Available subcommands: add, rm, list.

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
  -v, --verbose  Show alias description and table headers (works only for "list")
```

Subcommands are `add`, `rm`, `list`; anything else prints an error plus usage and
exits 1 (`libexec/scoop-alias.ps1:31-43`). `add` requires name and command, `rm`
requires name, each with exit 1 otherwise (`libexec/scoop-alias.ps1:48-62`).

### bucket (`libexec/scoop-bucket.ps1:1-19`)

```text
Add, list or remove buckets.

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
    scoop bucket known
```

Subcommand parsing is positional (`param($cmd, $name, $repo)`). `add` without a name
or with an unknown bucket and no repo exits 1; `rm` without a name exits 1;
unsupported subcommands print usage and exit 1 (`libexec/scoop-bucket.ps1:30-75`).

### cache (`libexec/scoop-cache.ps1:1-13`)

```text
Scoop caches downloads so you don't need to download the same files
when you uninstall and re-install the same version of an app.

You can use
    scoop cache show
to see what's in the cache, and
    scoop cache rm <app> to remove downloads for a specific app.

To clear everything in your cache, use:
    scoop cache rm *
You can also use the `-a/--all` switch in place of `*` here
```

Subcommand parsing is positional (`param($cmd)`); `rm` without apps exits 1 and
`-a`/`--all` select the whole cache (`libexec/scoop-cache.ps1:15-44`,
`libexec/scoop-cache.ps1:61-71`).

### cat (`libexec/scoop-cat.ps1:1-5`)

```text
Show content of specified manifest.
If configured, `bat` will be used to pretty-print the JSON.
See `cat_style` in `scoop help config` for further information.
```

Missing `<app>` prints `<app> missing` plus usage and exits 1
(`libexec/scoop-cat.ps1:14`).

### checkup (`libexec/scoop-checkup.ps1:1-4`)

```text
Performs a series of diagnostic tests to try to identify things that may
cause problems with Scoop.
```

### cleanup (`libexec/scoop-cleanup.ps1:1-11`)

```text
'scoop cleanup' cleans Scoop apps by removing old versions.
'scoop cleanup <app>' cleans up the old versions of that app if said versions exist.

You can use '*' in place of <app> or `-a`/`--all` switch to cleanup all apps.

Options:
  -a, --all          Cleanup all apps (alternative to '*')
  -g, --global       Cleanup a globally installed app
  -k, --cache        Remove outdated download cache
```

Missing apps without `--all` exits 1; global cleanup without admin rights exits 1
(`libexec/scoop-cleanup.ps1:18-28`).

### config (`libexec/scoop-config.ps1:1-164`)

```text
The scoop configuration file is saved at ~/.config/scoop/config.json.

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
      the `scoop cat` command and while doing manifest review. This requires 'bat' to be
      installed (run `scoop install bat` to install it), otherwise errors will be thrown.
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
      `scoop hold scoop` will set the value to one day later.
      Should be in the format 'YYYY-MM-DD', 'YYYY/MM/DD' or any other forms that accepted by '[System.DateTime]::Parse()'.
      Ref: https://docs.microsoft.com/dotnet/api/system.datetime.parse?view=netframework-4.5#StringToParse

update_nightly: $true|$false
      Nightly version is formatted as 'nightly-yyyyMMdd' and will be updated after one day if this is set to $true.
      Otherwise, nightly version will not be updated unless `--force` is used.

use_isolated_path: $true|$false|[string]
      When set to $true, Scoop will use `SCOOP_PATH` environment variable to store apps' `PATH`s.
      When set to arbitrary non-empty string, Scoop will use that string as the environment variable name instead.
      This is useful when you want to isolate Scoop from the system `PATH`.

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
      See: 'https://aria2.github.io/manual/en/html/aria2c.html#options'
```

`config` parses `name`/`value` positionally with a `rm` branch and always exits 0
(`libexec/scoop-config.ps1:166-191`).

### create (`libexec/scoop-create.ps1:1-4`)

```text
Create your own custom app manifest
```

### depends (`libexec/scoop-depends.ps1:1-2`)

No `Help:` header; prints only `Usage: scoop depends <app>`.
Missing `<app>` exits 1; getopt errors are ignored (edge E1).

### download (`libexec/scoop-download.ps1:1-20`)

```text
e.g. The usual way to download an app, without installing it (uses your local 'buckets'):
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
  -a, --arch <32bit|64bit|arm64>  Use the specified architecture, if the app supports it
```

Missing apps exit 1 (`libexec/scoop-download.ps1:47`).

### export (`libexec/scoop-export.ps1:1-4`)

```text
Options:
  -c, --config       Export the Scoop configuration file too
```

Only the first argument is inspected for `-c`/`--config`
(`libexec/scoop-export.ps1:10-16`).

### help (`libexec/scoop-help.ps1:1-2`)

No `Help:` header; prints only `Usage: scoop help <command>`.

### hold (`libexec/scoop-hold.ps1:1-10`)

```text
To hold a user-scoped app:
     scoop hold <app>

To hold a global app:
     scoop hold -g <app>

Options:
  -g, --global  Hold globally installed apps
```

Missing apps print usage and exit 1; global hold without admin rights exits 1
(`libexec/scoop-hold.ps1:22-30`).

### home (`libexec/scoop-home.ps1:1-3`)

No `Help:` header; prints only `Usage: scoop home <app>`.
Missing `<app>` prints usage and exits 1 (`libexec/scoop-home.ps1:20-23`).

### import (`libexec/scoop-import.ps1:1-4`)

```text
To replicate a Scoop installation from a file stored on Desktop, run
     scoop import Desktop\scoopfile.json
```

The scoopfile argument is mandatory (`libexec/scoop-import.ps1:6-10`).

### info (`libexec/scoop-info.ps1:1-4`)

```text
Options:
  -v, --verbose   Show full paths and URLs
```

Missing `<app>` prints usage and exits 1 (`libexec/scoop-info.ps1:16`).

### install (`libexec/scoop-install.ps1:1-28`)

```text
e.g. The usual way to install an app (uses your local 'buckets'):
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
  -a, --arch <32bit|64bit|arm64>  Use the specified architecture, if the app supports it
```

Missing apps exit 1 (`libexec/scoop-install.ps1:60`).

### list (`libexec/scoop-list.ps1:1-3`)

```text
Lists all installed apps, or the apps matching the supplied query.
```

### prefix (`libexec/scoop-prefix.ps1:1-2`)

No `Help:` header; prints only `Usage: scoop prefix <app>`.
Missing `<app>` prints usage and exits 1 (`libexec/scoop-prefix.ps1:5-8`).

### reset (`libexec/scoop-reset.ps1:1-7`)

```text
Used to resolve conflicts in favor of a particular app. For example,
if you've installed 'python' and 'python27', you can use 'scoop reset' to switch between
using one or the other.

You can use '*' in place of <app> or `-a`/`--all` switch to reset all apps.
```

Missing apps without `--all` exit 1 (`libexec/scoop-reset.ps1:20`).

### search (`libexec/scoop-search.ps1:1-8`)

```text
Searches for apps that are available to install.

If used with [query], shows app names that match the query.
  - With 'use_sqlite_cache' enabled, [query] is partially matched against app names, binaries, and shortcuts.
  - Without 'use_sqlite_cache', [query] can be a regular expression to match against app names and binaries.
Without [query], shows all the available apps.
```

### shim (`libexec/scoop-shim.ps1:1-36`)

```text
Available subcommands: add, rm, list, info, alter.

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
  - '--%' disables PowerShell parsing of the remainder; pass literals as needed.
```

Subcommands are `add`, `rm`, `list`, `info`, `alter`; anything else prints an error
plus usage and exits 1 (`libexec/scoop-shim.ps1:44-55`). Non-`list` subcommands
without a shim name exit 1; `add` without a command path exits 1; missing paths and
shims exit 2 or 3 by case (`libexec/scoop-shim.ps1:59-62`,
`libexec/scoop-shim.ps1:96-220`).

### status (`libexec/scoop-status.ps1:1-5`)

```text
Options:
  -l, --local         Checks the status for only the locally installed apps,
                      and disables remote fetching/checking for Scoop and buckets
```

Only the first argument is inspected for `-l`/`--local`
(`libexec/scoop-status.ps1:16`).

### uninstall (`libexec/scoop-uninstall.ps1:1-7`)

```text
e.g. scoop uninstall git

Options:
  -g, --global   Uninstall a globally installed app
  -p, --purge    Remove all persistent data
```

Missing apps exit 1; global uninstall without admin rights exits 1
(`libexec/scoop-uninstall.ps1:29-38`).

### unhold (`libexec/scoop-unhold.ps1:1-10`)

```text
To unhold a user-scoped app:
     scoop unhold <app>

To unhold a global app:
     scoop unhold -g <app>

Options:
  -g, --global  Unhold globally installed apps
```

Missing apps print usage and exit 1; global unhold without admin rights exits 1
(`libexec/scoop-unhold.ps1:22-30`).

### update (`libexec/scoop-update.ps1:1-15`)

```text
'scoop update' updates Scoop to the latest version.
'scoop update <app>' installs a new version of that app, if there is one.

You can use '*' in place of <app> to update all apps.

Options:
  -f, --force            Force update even when there isn't a newer version
  -g, --global           Update a globally installed app
  -i, --independent      Don't install dependencies automatically
  -k, --no-cache         Don't use the download cache
  -s, --skip-hash-check  Skip hash validation (use with caution!)
  -q, --quiet            Hide extraneous messages
  -a, --all              Update all apps (alternative to '*')
```

### virustotal (`libexec/scoop-virustotal.ps1:1-29`)

```text
Look for app's hash or url on virustotal.com

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
  -p, --passthru            Return reports as objects
```

Missing apps without `--all` print usage and exit 1; a missing API key aborts with
code 16 (`libexec/scoop-virustotal.ps1:38-42`,
`libexec/scoop-virustotal.ps1:60-73`).

### which (`libexec/scoop-which.ps1:1-3`)

```text
Locate the path to a shim/executable that was installed with Scoop (similar to 'which' on Linux)
```

Missing `<command>` prints `<command> missing` plus usage and exits 1
(`libexec/scoop-which.ps1:6-10`).
