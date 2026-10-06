# Scoop → Go Rewrite: Comprehensive Technical Plan

**Working title:** `gscoop` — a native Go reimplementation of Scoop, designed as a fully interchangeable, drop-in replacement for the PowerShell implementation ("classic Scoop").

**Source basis:** This plan is grounded in the actual classic Scoop codebase (`Scoop.zip`, current `master`, post-v0.6.0). Every claim about classic behavior below was verified against `bin/`, `lib/`, `libexec/`, and `supporting/` in that tree.

---

## 1. Executive Summary

Classic Scoop is ~28 PowerShell subcommands (`libexec/scoop-*.ps1`) dispatched by `bin/scoop.ps1` over 19 shared libraries (`lib/*.ps1`). Its real product is not the scripts — it is **an on-disk state contract**: directory layout, junctions, shims, two JSON metadata files per installed version, a cache naming scheme, git-backed buckets, and a `config.json`. Any replacement that reads and writes that contract bit-for-bit can alternate freely with classic Scoop on the same machine.

The rewrite therefore decomposes into:

1. **A state engine** (pure Go) that reproduces the contract exactly.
2. **A pipeline engine** (resolve → download → extract → hook → link → shim → persist) with pacman-style concurrency: *parallel artifact downloads, strictly sequential installs*.
3. **A CLI layer** with identical commands/arguments and clean, minimal, emoji-free output.
4. **Three embedded subsystems** replacing today's external tools: native search (replaces the third-party `scoop-search.exe` accelerator and the PowerShell scan), `go-git` for bucket management (replaces the `git.exe` process), and a Go extraction stack (replaces the Scoop-installed `7z.exe` helper).
5. **One permitted external process:** `powershell.exe`, used exclusively to execute manifest lifecycle hooks (`pre_install`, `post_install`, `pre_uninstall`, `post_uninstall`, `installer.script`, `uninstaller.script`, and `.ps1` installer files).

---

## 2. Ground-Truth Analysis of Classic Scoop

### 2.1 Codebase map (verified)

| Area | Files | Role |
|---|---|---|
| Entry | `bin/scoop.ps1` | Dispatches subcommands; `--version` reads CHANGELOG or git log |
| Libraries | `lib/core.ps1` (~1200 lines), `buckets`, `download`, `decompress`, `install`, `manifest`, `versions`, `depends`, `database`, `json`, `getopt`, `shortcuts`, `psmodules`, `commands`, `help`, `autoupdate`, `description`, `diagnostic`, `system` | Shared logic |
| Subcommands | 28 × `libexec/scoop-*.ps1` | alias, bucket, cache, cat, checkup, cleanup, config, create, depends, download, export, help, hold, home, import, info, install, list, prefix, reset, search, shim, status, uninstall, unhold, update, virustotal, which |
| Assets | `supporting/shims/{71,kiennq,scoopcs}/shim.exe`, `supporting/validator/validator.exe` (C#/Newtonsoft.Json.Schema), `supporting/formats/*.ps1xml` | Prebuilt binaries + display formats |
| Contracts | `schema.json` (manifest schema), `buckets.json` (known-bucket registry) | Versioned contracts |

### 2.2 The on-disk state contract (the interoperability surface)

All paths derive from `$scoopdir` (`%SCOOP%`, default `~\scoop`) and `$globaldir` (`%SCOOP_GLOBAL%`, default `C:\ProgramData\scoop`):

```
<root>\
  apps\<app>\<version>\          # one dir per installed version
    scoop-manifest.json          # copy of the manifest used (legacy name: manifest.json — still read as fallback)
    scoop-install.json           # {"architecture","bucket","url","hold"?} (legacy name: install.json — still read as fallback)
  apps\<app>\current\            # directory junction → version dir (skipped when NO_JUNCTION config is set)
  persist\<app>\...              # persisted data, linked into version dirs
  cache\<app>#<version>#<id>     # downloaded artifacts (see naming below)
  buckets\<name>\.git            # each bucket is a git clone; manifests under bucket\ subdir when present
  shims\<name>.exe|.shim|.cmd|.ps1
  workspace\<app>.json           # user-generated manifests
  scoop.db                       # SQLite search cache (only when USE_SQLITE_CACHE is enabled)
~\.config\scoop\config.json      # configuration
```

Critical details that must be reproduced exactly:

- **Cache naming** (`lib/core.ps1::cache_path`): `<app>#<version>#<sha256(url) first 7 hex chars>.<ext>`, **with a live migration**: if a file exists under the legacy name (`url` with non-`[\w.\-]` replaced by `_`), that name wins. Go must check the legacy form first, forever.
- **Installed-version resolution** (`Select-CurrentVersion` in `lib/versions.ps1`): `current` junction target when junctions are on; otherwise the newest version directory by `Compare-Version`. A version dir without `current` linkage and without install metadata = "failed install" marker (`failed()`).
- **Hold state**: per-app `"hold": true` written into `scoop-install.json` (`scoop-hold.ps1`); holding `scoop` itself writes `HOLD_UPDATE_UNTIL` (ISO-8601) into `config.json`.
- **Shim triple**: `<name>.exe` is a byte-copy of a bundled shim binary; `<name>.shim` is a text file (`path = <target>` / `args = <args>`); plus generated `.cmd` and `.ps1` wrappers. Classic rewrites `System32→Sysnative` / `SysWOW64→System32` in the shim path when the shim PE is x86 on an x64 OS (`lib/core.ps1::shim`, `Get-PEMachine`). Alias shims are the same triple named `scoop-<alias>.*`.
- **SQLite cache schema** (`lib/database.ps1`): single table `app(name TEXT COLLATE NOCASE, description, version, bucket, manifest JSON, binary, shortcut, dependency, suggest, PRIMARY KEY(name,version,bucket))` at `$scoopdir\scoop.db`.
- **Version comparison** (`Compare-Version`): semver-ish, `-` delimiter with `+` normalized to `-`; `nightly` equal unless `UPDATE_NIGHTLY` (then date-compare `yyyyMMdd`); shorter-vs-longer rules special-case `alpha|beta|rc|pre`; recurses on `.` and `_`; numeric-vs-string compare. **This function is correctness-critical** for `status`, `update`, `cleanup`, and version-pinned installs — port it character-for-character and property-test it against the PowerShell original.
- **Install pipeline order** (`install_app`): resolve manifest → architecture negotiation (`Get-SupportedArchitecture`) → create version dir → download → extract → `pre_install` → `installer` (file+args or script) → PATH guard → `current` junction → shims → Start-Menu shortcuts → psmodule → `env_add_path` → `env_set` → persist → persist ACL → `post_install` → write `scoop-manifest.json` + `scoop-install.json` → notes. Uninstall is the precise reverse plus `uninstaller` hooks. Deviation in ordering = interop breakage.

### 2.3 External dependency inventory (verified)

| Dependency | How classic obtains it | Used for |
|---|---|---|
| PowerShell 5.1+ | the host itself | everything |
| `git.exe` | Scoop-installed `git` app (`apps\git\current\mingw64\bin\git.exe`) preferred, else `PATH` (`Get-HelperPath Git`) | bucket add/update, self-update, `app@version` history lookup, status/info/version output |
| `7z.exe` | Scoop-installed `7zip` app; `PATH` only if `USE_EXTERNAL_7ZIP=true` | all archive extraction except plain `.zip` (and even `.zip` when 7zip is present), MSI lessmsi path excluded |
| `lessmsi.exe` | Scoop-installed, only when `USE_LESSMSI=true`; else `msiexec /a` (OS component) | `.msi` extraction |
| `innounp.exe` | Scoop-installed `innounp`/`innounp-unicode` | manifests with `"innosetup": true` |
| `dark.exe`/`wix.exe` | Scoop-installed | WiX Burn bundles (via `Expand-DarkArchive` in hooks) |
| `aria2c.exe` | Scoop-installed, optional (config-gated) | segmented downloads |
| `validator.exe` | bundled C# binary | manifest validation (`scoop create`, maintainer tooling) |
| `System.Data.SQLite` + `sqlite3.dll` | **downloaded on demand** from NuGet/sqlite.org when `USE_SQLITE_CACHE` is first enabled | search cache |
| `powershell.exe` hooks | in-process today | manifest lifecycle scripts |

### 2.4 Config surface (verified keys)

`ALIAS, ARIA2, AUTOSTASH_ON_CONFLICT, CACHE_PATH, CAT_STYLE, DEBUG, DEFAULT_ARCHITECTURE, FORCE_UPDATE, GH_TOKEN, GLOBAL_PATH, HOLD_UPDATE_UNTIL, IGNORE_RUNNING_PROCESSES, LAST_UPDATE, NO_JUNCTION, PRIVATE_HOSTS, PROXY, ROOT_PATH, SCOOP_BRANCH, SCOOP_REPO, SHIM, SHOW_MANIFEST, SHOW_UPDATE_LOG, UPDATE_NIGHTLY, USE_EXTERNAL_7ZIP, USE_GIT_HISTORY, USE_ISOLATED_PATH, USE_LESSMSI, USE_SQLITE_CACHE, VIRUSTOTAL_API_KEY` — lookup is case-insensitive; `Complete-ConfigChange` attaches side effects (e.g., enabling `USE_SQLITE_CACHE` builds the DB; `ARIA2` toggling warns). The Go `config` command must honor the same keys (adding new Go-only keys is safe — classic ignores unknown keys).

---

## 3. Target Architecture

### 3.1 Principles

1. **Single static binary**, pure Go by default (`CGO_ENABLED=0`); Windows-only target for the engine, cross-compilable test harnesses elsewhere where feasible.
2. **The state contract is the API.** No Go-only sidecar state that classic Scoop would need to understand. The only additions are a lock file and log files, both ignored by classic.
3. **PowerShell is the only external process**, spawned solely by the hook runner (§8.4). Everything else — git, archives, search, HTTP, shims, junctions, shortcuts, env vars — is in-process Go.
4. **Dependency discipline:** `golang.org/x/sys`, `golang.org/x/text` (encoding), `go-git/go-git/v5`, `mholt/archives`, `ulikunitz/xz`, `klauspost/compress`, `nwaples/rardecode`, `bodgit/sevenzip`, `modernc.org/sqlite`, `santhosh-tekuri/jsonschema`, `go-ole/go-ole` (shortcuts), `richardlehane/mscfb` (MSI/CFB, phase 3). No Cobra/Viper — hand-rolled parsing/output for exact CLI parity and binary size.

### 3.2 Module layout

```
github.com/<org>/gscoop
├── cmd/gscoop/main.go           # dispatch, mirrors bin/scoop.ps1 switch
├── internal/
│   ├── cli/                     # arg parser (getopt.ps1 semantics), usage/help text
│   ├── commands/                # one file per subcommand, mirroring libexec/
│   ├── config/                  # config.json: case-insensitive keys, side effects
│   ├── state/                   # paths, installed apps, Select-CurrentVersion, failed()
│   ├── manifest/                # parse, arch_specific resolution, schema validation
│   ├── bucket/                  # local/known buckets, add/rm, manifest enumeration
│   ├── gitengine/               # GitEngine interface + go-git impl + git.exe fallback
│   ├── download/                # concurrent fetcher, cache naming, hashes, proxies
│   ├── extract/                 # extractor registry (zip/tar/7z/rar/xz/zstd/msi/inno/...)
│   ├── install/                 # install/uninstall/reset pipelines, persist, env, ACL
│   ├── hook/                    # PowerShell hook runner (the only os/exec user)
│   ├── shim/                    # embed shim.exe variants, .shim/.cmd/.ps1 writers, PE patching
│   ├── search/                  # regex scan + modernc.org/sqlite cache (scoop.db)
│   ├── version/                 # Compare-Version port + property tests
│   ├── deps/                    # dependency resolution + installation-helper inference
│   ├── update/                  # app updates, bucket sync, self-update
│   ├── lnk/                     # Start-Menu shortcuts via go-ole IShellLink
│   ├── junction/                # NTFS reparse points via FSCTL_SET_REPARSE_POINT
│   └── ui/                      # functional output, progress, NO_COLOR/non-TTY handling
└── testdata/                    # fixture manifests, golden outputs, Pester-derived cases
```

### 3.3 Component map: classic → Go

| Classic | Go replacement | Disposition |
|---|---|---|
| `bin/scoop.ps1` dispatch | `cmd/gscoop` switch | rewrite |
| `lib/getopt.ps1` | `internal/cli` | rewrite |
| `lib/core.ps1` (paths, config, shims, PE patching, external-command runner) | `state`, `config`, `shim`, `hook`, `ui` | rewrite |
| `lib/download.ps1` (+ aria2 path) | `download` (native HTTP, ranged/segmented, concurrent) | rewrite; **aria2 support removed** |
| `lib/decompress.ps1` (+ 7z/lessmsi/innounp/dark helpers) | `extract` (§8.3) | rewrite; **helper apps removed** |
| `lib/buckets.ps1`, all `Invoke-Git` call sites | `gitengine` (§8.2) | rewrite; **git.exe removed** |
| `lib/database.ps1` + on-demand System.Data.SQLite download | `search` with `modernc.org/sqlite` (pure Go) | rewrite; **.NET SQLite removed** |
| `libexec/scoop-search.ps1` | `search` in-process | rewrite; third-party `scoop-search.exe` superseded |
| `supporting/validator/validator.exe` (C#, Newtonsoft.Json.Schema) | `manifest` with `santhosh-tekuri/jsonschema` against the same `schema.json` | rewrite; **validator.exe removed** |
| `supporting/shims/*/shim.exe` | embedded via `embed.FS` | **kept as data assets** (they are payloads, not runtimes; classic installs byte-identical shims — keeping them is required for interop) |
| `lib/shortcuts.ps1` (WScript.Shell COM) | `lnk` via `go-ole` IShellLink | rewrite |
| `lib/psmodules.ps1` | `install` (copy module dir + PSModulePath env edit) | rewrite (semantics kept — it is a manifest feature, not a runtime dep) |
| `lib/autoupdate.ps1`, `bin/checkver.ps1` etc. | out of end-user scope; optional phase-4 `gscoop checkver/autoupdate` | deferred |
| PowerShell 5/7 dual code paths (e.g., `search_bucket_legacy`), deprecated wrappers (`Expand-ZstdArchive`, `bucketdir`, …) | — | **deleted, not ported** |

### 3.4 Legacy mechanisms to remove (not port)

- **aria2 download path** (`Invoke-CachedAria2Download`, `aria_exit_code`, all `ARIA2*` config) — superseded by native concurrent/segmented HTTP. Config keys are accepted and ignored with a one-line notice.
- **`scoop-search.exe` awareness** — never an upstream component (it is the third-party Zig accelerator `shilangyu/scoop-search`, installed via `scoop install scoop-search` and wired through a PowerShell-profile hook). Nothing to preserve; the Go binary *is* the fast search. Users' profile hooks that call `scoop-search.exe` keep working only if that app stays installed; document the removal path.
- **On-demand .NET/SQLite bootstrapping** (`Get-SQLite` NuGet + sqlite.org download dance) — embedded pure-Go SQLite instead.
- **`scoopcs` legacy C# shim variant** — the `SHIM` config accepts `scoopcs` but maps to the maintained default (`kiennq`/`71`), matching upstream's removal of the CS shim codebase.
- **PS1XML display formats** (`supporting/formats`) — PowerShell-host-only formatting; Go renders its own tables.
- **cmd.exe subshell remnants and `getopt` quirks** — Go parser implements the documented surface only.

---

## 4. CLI Design

### 4.1 Command and argument parity

All 28 subcommands keep identical names, positional arguments, and switches (`-g/--global`, `-s/--skip-hash-check`, `-k/--no-cache`, `-u/--no-update-scoop`, `-f/--force`, `-a/--arch`, `-p/--purge`, `-q/--quiet`, `-v/--verbose`, `-y/--yes`, `/?` …), exactly as parsed by `getopt.ps1` in each `libexec` file. Parsing rules:

- Short and long forms; `--` terminates options; unknown options → `ERROR scoop <cmd>: <err>` + exit 1 (mirrors today).
- `scoop` / `scoop help` / `-h`/`--help`/`/?` behavior identical; `scoop --version` prints core + per-bucket HEAD lines (via `gitengine`, not git).
- App specs keep all four forms: `app`, `bucket/app`, `app@version`, and URL/path-to-manifest (generating a `workspace\` manifest, including `generate_user_manifest` semantics).

### 4.2 Output style

- **Functional and minimal.** No emojis anywhere (classic already follows this; the Go version hard-codes it as a lint rule). Status lines keep the recognizable prefixes — `Installing 'git' (2.47.0) ...`, `Creating shim for 'git'.`, `'git' (2.47.0) was installed successfully!` — because muscle memory and log parsers depend on them.
- Diagnostics use the classic severity prefixes exactly: `ERROR `, `WARN  `, `INFO  `, `DEBUG ` (the last only with `DEBUG` config or `--verbose`).
- Colors only when stdout is a terminal; honor `NO_COLOR` and `--no-color`; identical text with or without color (color is decoration, never information).
- No redundant prose: drop multi-paragraph hints that classic prints on happy paths; keep actionable error tails (e.g., the aria2-style "try again with `-s`" hints only where recovery depends on them).
- Progress: single-line carriage-return progress per active download plus an aggregate line, e.g.
  `DL 3/12  git-2.47.0.zip  14.2/61.8 MB  8.4 MB/s` — rendered only on TTY; log-redirected output gets one line per completed artifact.
- Structured-machine mode is **out of scope** (classic has none; adding `--json` would be a new feature, not parity).

### 4.3 Exit codes

0 success; 1 generic/user error; preserve command-specific codes that scripts rely on (`scoop search` no-match → 1; `scoop bucket add` duplicate → 2 per `add_bucket`). Install/update abort semantics (partial failure leaves a "failed install" marker exactly as classic's `failed()` computes it).

---

## 5. Concurrency Model (pacman-style)

### 5.1 Transaction structure

Every mutating command builds a **transaction**: a fully resolved, dependency-ordered list of package operations (`deps.Get-Dependency` — the electricmonk DFS algorithm, plus `Get-InstallationHelper` inference which today can *prepend* `7zip`/`lessmsi`/`innounp`/`dark` to the dependency list; in Go these become no-ops because extraction is built in — the resolver keeps the code path but emits nothing, preserving manifest semantics for `depends` output).

Phases:

1. **Plan** — resolve manifests, versions, architectures; compute per-URL artifact list. Read-only, parallel.
2. **Download (parallel)** — all artifacts for the whole transaction fetched concurrently.
3. **Install (strictly sequential)** — extraction + hooks + linking in dependency order, one package at a time.
4. **Commit** — metadata write (`scoop-manifest.json`, `scoop-install.json`) is the atomic commit point per package, matching classic's `failed()` semantics.

### 5.2 Download scheduler

- Worker pool, size = new config `MAX_DOWNLOADS` (default 4; classic is implicitly 1; aria2 segmented *within* one file).
- Per artifact: resume via HTTP `Range` against the cache file (classic restarts from zero — resume is a strict improvement that does not change the cache contract); optional segmented multi-connection fetch per file (`SPLIT_DOWNLOADS`, default 1, aria2-like when 4–16).
- Hash verification (md5/sha1/sha256/sha512, multihash strings, `<url>#.hash` fragment sources, `hash` extraction modes from `lib/download.ps1::get_hash`) runs as each file completes, on a separate goroutine pool; a failed hash fails only that package, downloads for others continue (classic aborts the whole transaction — behavior change flagged in §10 as deliberate).
- Cookie/per-URL headers (`cookie_header`), proxy matrix (`PROXY` incl. `currentuser@` handling), `GH_TOKEN` for api.github.com, special-URL rewriting (`handle_special_urls`: SourceForge mirror redirect, etc.) — all ported.
- FTP: classic supports `ftp_file_size`/FTP downloads via .NET; Go: `jlaffaye/ftp` or documented drop (usage in buckets is effectively zero — verify with a bucket-wide URL scan during phase 0).

### 5.3 Install serialization & locking

- **Process-level mutex**: `$scoopdir\scoop.lock` (global: `$globaldir\scoop.lock`) via `LockFileEx` on a held handle. Classic has no lock; the file is invisible to it. Timeout + `WARN another scoop process holds the lock; waiting`.
- **Package-level**: in-process map keyed by `(app, global)` — belt-and-braces; the sequential executor makes this trivially satisfied.
- **Crash atomicity**: extract into `<version>.tmp` sibling, then rename → `<version>` (rename is atomic on the same volume); write metadata files via temp-file + rename. On any failure: remove `.tmp`, leave the classic-recognizable "failed" state (version dir present, no `current`, no metadata) rather than half-linked trees.
- **Junction flip**: unlink `current` → create junction to new version. Keep the classic window where `current` briefly does not exist (identical failure modes), or do staged junction swap — choose classic parity.
- Running-process guard (`test_running_process`) before update/uninstall, ported with `IGNORE_RUNNING_PROCESSES` escape hatch.

### 5.4 Bucket sync

Parallel fetch across buckets (recent classic parallelizes this too, #6735) with a small pool; per-bucket `pull` serialized on that repo. `scoop-update`'s app updates reuse the same transaction engine (downloads parallel, installs sequential).

---

## 6. Interoperability & Migration Strategy

### 6.1 Bidirectional interchangeability requirements

The acceptance test for the whole project:

```
classic:  scoop install git 7zip
gscoop:   scoop install ripgrep && scoop update git
classic:  scoop uninstall ripgrep && scoop status && scoop install fd
gscoop:   scoop cleanup * && scoop list
```

After every arrow, `scoop list`, `scoop status`, and `scoop checkup` run under **either** implementation must agree, and every installed app must still launch through its shim. This holds iff:

1. **Reads accept both generations**: `scoop-install.json`/`install.json`, `scoop-manifest.json`/`manifest.json`, legacy cache names, `scoopcs`-era shims.
2. **Writes use current-generation names and byte-compatible content** (same JSON field order as `ConvertToPrettyJson` — Go must use an ordered writer, not `map[string]any`; PowerShell's `ConvertFrom-Json` is order-tolerant, but diff-noise in `scoop-export`/git-diffs is an interop smell).
3. **Junctions, shims, shortcuts, env-var edits** are produced through the same Win32 primitives classic effectively uses.
4. **Buckets remain ordinary git repositories** regardless of which engine cloned/pulled them (go-git writes standard `.git` — verified as a phase-0 spike, see §8.2).
5. **The `scoop` core app directory is treated as data.** GScoop never writes into `apps\scoop\current` (the classic git checkout); it only reads it when asked to *update classic* (see §6.3).

### 6.2 Installation & switching mechanism

- GScoop ships as a normal Scoop package (`gscoop` manifest). Its `post_install` swaps the `scoop` shim triple in `~\scoop\shims\` (`scoop.exe` → gscoop binary copy, `scoop.shim` rewritten) after backing the classic trio up as `scoop.classic.{exe,shim,cmd,ps1}`. Uninstall restores them. Switching back is `scoop uninstall gscoop` or `gscoop unswap` (a hidden maintenance command, not a new user-facing verb).
- Because shims are dumb path launchers, **both implementations coexist**: `scoop` (active) and `~\scoop\apps\scoop\current\bin\scoop.ps1` (classic, always directly invocable) — this makes A/B verification trivial and is the safety net for the alternation torture test (§9).
- `gscoop doctor` (= `scoop checkup` parity, extended) validates the contract: junction targets, shim trio consistency, `scoop-install.json` readability by `ConvertFrom-Json`, bucket repo integrity, PATH presence of both shim dirs, Defender exclusion hints (same checks as `scoop-checkup.ps1`).

### 6.3 Self-update strategy

Classic `scoop update` first git-pulls `apps\scoop\current` (clone of `ScoopInstaller/Scoop`, branch from `SCOOP_BRANCH`/`SCOOP_REPO`, with stash-on-conflict via `AUTOSTASH_ON_CONFLICT`). A Go binary cannot be its own git working tree. Resolution:

- `gscoop update` self-updates the **gscoop binary** from signed GitHub releases (semver check, `UPDATE_NIGHTLY`-style channel config `GSCOOP_CHANNEL`), replacing the file on disk via rename-swap (Windows-safe: rename running exe to `.old`, write new).
- If `apps\scoop\current` is a git repo, `gscoop update` **also** fast-forwards it with `gitengine` (best-effort; failure → `WARN classic scoop core not updated: ...`), so switching back to classic never lands on a stale core. This preserves classic semantics ("scoop core updates via git") for the classic installation while GScoop itself follows binary release management.
- `HOLD_UPDATE_UNTIL` (from `scoop hold scoop`) is honored for both halves.

### 6.4 State-corruption guards

- Lock file (§5.3) prevents two concurrent GScoop writers; GScoop cannot detect a concurrent *classic* writer — documented limitation, same as two concurrent classic processes today.
- After any crash, the next GScoop command runs a cheap consistency sweep (orphan `.tmp` dirs, `current` pointing at missing dirs) and repairs or reports — strictly more robust than classic, never less compatible.

---

## 7. PowerShell Hook Runner (the sole external process)

### 7.1 Why it exists

Manifests embed PowerShell: `pre_install`, `post_install`, `pre_uninstall`, `post_uninstall`, `installer.script`, `uninstaller.script` (string or string-array), and `installer.file` may name a `.ps1` run with `installer.args`. Classic executes them **in-process** via `[scriptblock]::Create()` + `Invoke-Command`, which means manifest scripts see caller variables through dynamic scoping: `$dir`, `$persist_dir`, `$original_dir`, `$version`, `$global`, `$architecture`.

### 7.2 Design

- **Host selection:** `powershell.exe` (Windows PowerShell 5.1) by default — this is what classic hooks were written and tested against; config `PSHOST` allows `pwsh`. Resolved once per process; absent host → install fails with the classic-style actionable error.
- **Invocation:** `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File <tmp.ps1>`, working directory = app version dir, stdout/stderr streamed through `ui` with the `Running <hook> script...` framing classic prints. Exit code ≠ 0 → abort exactly as classic's scriptblock exceptions do.
- **Variable injection:** the temp file is `preamble + user script`. The preamble assigns, from values passed via environment variables (not string interpolation — no injection bugs):
  `$dir`, `$original_dir`, `$persist_dir`, `$version`, `$architecture`, `$global` (as `$true`/`$false` bool), plus `$SCOOP`, `$SCOOP_GLOBAL` in env. This reproduces the dynamic-scoping surface observably identical for all conforming manifests.
- **`.ps1` installer files:** same runner, args bound via `param(...)`-safe passing (file + argv to powershell `-File`, matching `& $progName @fnArgs` semantics incl. `substitute()` of `$dir`/`$version`/`$global` tokens in `installer.args`).
- **Non-`.ps1` `installer.file`/`uninstaller.file`** (exe/msi with args) runs via `os/exec` directly — that is *the payload*, not a runtime dependency (classic does the same via `Invoke-ExternalCommand`).
- **Timeout & cancellation:** none by default (parity), Ctrl-C propagates (console process group), and the install transaction rolls to the classic "failed" state.
- **Threat model:** unchanged from classic — bucket manifests are trusted code; hooks run with the caller's privileges (elevated for `-g`). Document this verbatim in the security section of the README.

---

## 8. Component Feasibility Assessment

### 8.1 Search: native, in-binary

**Premise validation.** `scoop-search.exe` is **not** a classic Scoop component. Upstream search lives entirely in `libexec/scoop-search.ps1`: case-insensitive .NET regex against manifest file names and raw file content, with JSON parsed only for candidates, matching on `name` and `bin` entries (including `[exe, alias, args]` triples), output columns `Name/Version/Source/Binaries`; unknown-but-known buckets are queried remotely through the GitHub git-trees API. `scoop-search.exe` is the third-party accelerator `shilangyu/scoop-search` (Zig), installed per-user via `scoop install scoop-search` and typically wired in through a PowerShell-profile hook. So there is nothing to "remove" from the contract — the Go binary simply embeds search, superseding both the PowerShell scan and the external accelerator in one move.

**Design.**

- Index-free scan: concurrent walker over `buckets\<name>\(bucket\)`, raw-content regex prefilter (`regex.Match` on bytes) → parse JSON only on hit → name/bin/shortcut matching with classic precedence (name match lists empty `Binaries`; bin match lists matched names joined by ` | `).
- Result selection: latest version **semantically** (Compare-Version), matching upstream #6643.
- `USE_SQLITE_CACHE` parity: pure-Go `modernc.org/sqlite` (no cgo) operating on the **same file and schema** (`$scoopdir\scoop.db`, table `app`). Cache maintenance hooks fire on bucket add/rm/update, exactly like `Set-ScoopDB` call sites. Mixed-implementation use is safe: schema is identical, both sides `INSERT OR REPLACE`/delete by `(name,version,bucket)`.
- Remote fallback search (GitHub trees API for known-but-not-added buckets) ported with `GH_TOKEN` and rate-limit guard (`github_ratelimit_reached`).
- **Regex dialect delta:** .NET regex → Go RE2. Lookarounds/backreferences are unsupported in RE2; on compile failure GScoop falls back to case-insensitive substring matching and prints `WARN unsupported regex syntax; using literal match` (classic hard-errors with `Invalid regular expression`). This is the single known semantic gap; documented.

**Feasibility: high.** Effort: small. Performance target: ≤50 ms full-bucket scan on warm cache (classic: seconds; `scoop-search.exe`: ~10–50 ms — Go lands in the same class via mmap-free buffered reads + goroutine fan-out).

### 8.2 Git handling: `go-git`

**Premise validation.** Classic Scoop **invokes `git.exe` directly and internally** for bucket management — it is not merely an external package dependency. `add_bucket` refuses to run without git ("Git is required for buckets. Run 'scoop install git'"); the binary is resolved through `Get-HelperPath Git` (Scoop's own `git` app first, then `PATH`) and shelled out via `Invoke-Git`. So git is an *internal operational dependency delivered as an external package* — replacing the process with `go-git` removes a bootstrap paradox (needing git to get buckets that provide git) and a multi-megabyte runtime dependency.

**Verified call-site inventory** (every one must be covered):

| Call site | Git operations | go-git support |
|---|---|---|
| `scoop bucket add` | `ls-remote`, `clone -q` | ✅ `remote.List`, `PlainClone` (depth configurable) |
| `scoop bucket list/known` | `config remote.origin.url`, `log --format=%aI -n 1` | ✅ `Config`, `Log` |
| `scoop update` (buckets) | `rev-parse HEAD`, `pull -q`, `log --grep=^(chore) --invert-grep --format=...`, `diff --name-status A..B` | ✅ `ResolveRevision`, `Pull`; grep/format done in Go over `Log`; tree diff via `object.DiffTree` |
| `scoop update` (scoop core / classic repos) | `clone --branch --single-branch`, `diff HEAD --name-only`, `stash push -u`, `config remote.origin.*`, `fetch --force <refspec>`, `checkout -B <b> -t origin/<b>`, `reset --hard origin/<b>`, `pull --tags --force` | ✅ all **except `stash push -u`** |
| `app@version` installs (`USE_GIT_HISTORY`, default on) | `log --format=%H -- <path>`, `show <commit>:bucket/<app>.json` | ✅ path-filtered `Log` + blob read — but **requires full history** |
| `scoop status`, `scoop info`, `new_issue_msg` | remote queries, `log`, `config --get` | ✅ |

**Trade-offs and mitigations.**

1. **`stash push -u` has no go-git equivalent.** Only the scoop-core self-update path stashes (bucket pulls don't). Mitigation ladder: (a) GScoop's own self-update is binary (§6.3) so the path is only exercised when maintaining the *classic* checkout; (b) detect dirty tree via `Status`; if dirty and `AUTOSTASH_ON_CONFLICT`, copy modified/untracked files to `workspace\.autostash\<ts>\`, `reset --hard`, and print restore instructions — observable outcome equivalent to classic; (c) refuse with the classic error text otherwise.
2. **History depth vs. clone cost.** Full clones of large buckets (Extras: years of history) are slow and memory-hungry in go-git (packfile delta resolution; expect 2–5× wall-clock vs. git.exe, high RAM on first clone), and `USE_GIT_HISTORY` needs that history for `app@version`. Strategy: full clone by default (parity); config `SHALLOW_BUCKETS` (depth 1, Go-only key) for users who accept losing version-pinned installs; go-git lacks partial-clone filters, so treeless/blobless clones are not an option — noted as an upstream limitation to revisit.
3. **Repo maintenance.** git.exe auto-gcs; go-git has no repack/gc. Over years of daily pulls, pack accumulation degrades performance. Mitigation: `gscoop checkup` reports pack count/size; maintenance = delete + re-clone (buckets are expendable mirrors — classic `bucket rm`/`add` does exactly this) or run external git when present.
4. **Auth.** Public buckets need none. Private buckets using Git Credential Manager or Pageant (Windows ssh-agent) are **not** reachable through go-git's auth model (https basic/token and ssh key/agent-via-`SSH_AUTH_SOCK` work; Windows-native credential helpers don't). Mitigation: `gitengine` interface ships two implementations — `go-git` (default) and `git.exe` (selected automatically when a credential helper is configured, or via config `USE_EXTERNAL_GIT`, mirroring the spirit of `USE_EXTERNAL_7ZIP`). The fallback keeps full functionality for private-bucket users at the cost of keeping git installed.
5. **Interop verification (phase-0 spike, gating).** Prove round-trips: repo cloned by go-git → `git.exe fsck` clean → classic `scoop update` pulls fine; and vice versa (repo cloned by git.exe → go-git pull). Also long paths (`core.longpaths`), symlinks in repos, and CRLF/`core.autocrlf` neutrality (manifests are JSON — tolerant, but filemode bits must not dirty the tree on Windows).

**Feasibility: high with a contained fallback.** The interface seam (`gitengine`) confines the risk; the fallback guarantees no user-visible regression.

### 8.3 Archive extraction: `sevenzip-go` vs. pure Go

**Premise validation.** Classic Scoop **does not bundle 7-Zip** and does invoke `7z.exe` directly for internal manifest extractions: `Expand-7zipArchive` resolves `apps\7zip\current\7z.exe` (an ordinary Scoop-installed package — `Get-InstallationHelper` even injects `7zip` into the dependency graph when a URL matches `Test-7zipRequirement`'s extension list), falling back to `PATH` `7z` only under `USE_EXTERNAL_7ZIP`. Plain `.zip` goes through .NET (`Expand-Archive` semantics) *unless* 7zip is installed, in which case 7z handles zip too; `.tar*` inside compressed wrappers is a two-pass 7z invocation; MSI uses lessmsi (if `USE_LESSMSI`) or `msiexec /a`; Inno uses innounp; Burn uses dark. So 7z is *"internal extraction performed by an external process delivered as a package"* — exactly what an embedded library eliminates.

**Option A — `github.com/itchio/sevenzip-go`.** cgo bindings over the 7-Zip C sources.

- ✅ Format fidelity identical to real 7z: solid archives, BCJ/BCJ2/Delta/PPMd filters, multi-volume `.7z.001`, many installer containers (incl. some NSIS), `tar`-inside-anything two-pass replaced by streaming.
- ❌ **cgo**: breaks `CGO_ENABLED=0`; Windows builds need a C toolchain (or zig-cc); cross-compilation and reproducible-build pipelines get heavier; binary grows (~1.5–2 MB); the bundled C code carries CVE exposure that Go tooling (`govulncheck`) cannot see; upstream maintenance cadence is thin.
- Verdict: viable, but it trades a *runtime* dependency for a *build-time* one — against this project's "pure Go" spirit.

**Option B — pure-Go composite stack (recommended).**

| Classic extraction target | Go implementation | Coverage |
|---|---|---|
| `.zip` | `archive/zip` (stdlib) | full parity incl. `extract_dir`/`extract_to` |
| `.tar`, `.tar.gz/.tgz`, `.tar.bz2`, `.tar.xz`, `.tar.zst`, `.gz/.bz2/.xz/.zst` | `archive/tar` + stdlib gzip/bzip2 + `ulikunitz/xz` + `klauspost/compress/zstd` | full |
| `.7z` | `bodgit/sevenzip` (LZMA/LZMA2/BCJ/Delta/PPMd/Deflate/BZip2/COPY) | ~all real-world manifests; **multi-volume `.001` unsupported** |
| `.rar` (incl. split, per `decompress.ps1` split-RAR handling) | `nwaples/rardecode` (RAR4/RAR5 read) | good; split-RAR assembly ported from classic logic |
| `.nupkg`, `.lzma`, `.lzh`, `.img`, `.iso` | nupkg=zip; lzma via xz pkg; lzh via `saintfish/chardet`-era `golzh` fork or drop-with-warning; iso/img rare — explicit unsupported error | partial (long tail, measured in phase 0 by bucket scan) |
| `.msi` | phase 1: `msiexec /a` (Windows OS component — not an installed dependency); phase 3: Go reader = `richardlehane/mscfb` (CFB) + cabinet (LZX/MSZIP) extractor → true `lessmsi` parity | staged |
| Inno Setup (`"innosetup": true`) | phase 1–2: **the one transitional external helper** (innounp, exactly as classic resolves it) *or* error-with-guidance; phase 3: Go port of the documented Inno data format (innounp/innoextract sources as reference) | staged — see below |
| WiX Burn (`dark`) | rare (hooks-only); phase 3 container extractor or documented exception | low priority |
| NSIS-ish `.exe` that 7z could open | **gap** — neither pure-Go 7z nor archives handles NSIS containers; manifests relying on silent 7z-into-exe extraction need Option A or external 7z | known gap |
| Unified detection/dispatch | `mholt/archives` as the sniffing frontend where applicable | — |

**Integration strategy.** `extract` exposes `Extract(path, dest, opts{ExtractDir, ExtractTo, Removal})` with a registry mirroring `Invoke-Extraction`'s dispatch (including the "prefer 7z engine for zip" nuance — in Go, the zip path *is* the built-in path, so the nuance evaporates). Three escape hatches preserve behavior for edge manifests:

1. `USE_EXTERNAL_7ZIP=true` honored verbatim (shell out to `7z.exe`) — the sanctioned fallback for NSIS/multi-volume/exotic filters, at the user's explicit choice.
2. Optional `cgo7z` build tag swapping the 7z extractor for `sevenzip-go` (for distributions that accept cgo).
3. Inno/MSI transitional behavior documented above, with a hard roadmap commitment and per-format test fixtures.

**Feasibility: high for the 95% path (zip/tar/7z/rar + compression wrappers); medium for MSI (msiexec bridge is trivial, native reader is a quarter-scale project); medium-high effort for Inno (format is documented but versioned 2→6).** The decisive enabler: bucket-wide telemetry from a phase-0 scan classifying every `url` in all known buckets by extension and `innosetup` flag, so the long tail is measured, not guessed.

### 8.4 PowerShell hooks — summary

Covered in §7. Feasibility: high; the only subtlety is faithful variable injection and host-version parity (5.1 default). This is the *one* place `os/exec` appears in the codebase for scripts, enforced by CI lint.

---

## 9. Testing & Validation Strategy

1. **Golden differential harness (the core gate).** A Windows CI matrix that, per fixture: spins a clean `%SCOOP%`, runs an operation script under classic Scoop, snapshots the tree (file list + hashes + junction targets + shim bytes + `scoop-*.json` + config + PATH/env deltas); resets; runs the same script under GScoop; snapshots; **diffs the snapshots** and asserts semantic equality (with a normalization layer for timestamps and ordering-insensitive sets). Fixture matrix covers: zip/7z/rar/msi/inno installers, `installer.script` vs `installer.file` (.ps1 and .exe), persist (files/dirs, pre-existing data), `env_add_path`/`env_set`, shortcuts, psmodule, bin with alias/args, `depends` chains, `suggest`, arch-specific sections (64bit/32bit/arm64), `app@version` (nightly, pinned, `USE_GIT_HISTORY` on/off), hold/unhold, purge uninstall, `NO_JUNCTION` mode, global scope.
2. **Alternation torture test.** Scripted round-robin: classic installs A → GScoop updates A, installs B → classic uninstalls A, updates B → GScoop cleanup… — 50+ operations, asserting `list`/`status`/launchability after each hand-off. This directly tests the bidirectional-interchange requirement.
3. **Property tests for `Compare-Version`.** Fuzz pairs of version strings; compare Go result vs. PowerShell `Compare-Version` (driven headless in CI) — must match 100%, including `nightly`/`UPDATE_NIGHTLY`, `+`→`-`, alpha/beta/rc/pre, mixed numeric/string segments.
4. **Manifest corpus test.** Every manifest in `main`, `extras`, `versions`, `java`, `games` parsed + arch-resolved + schema-validated by the Go engine; zero unexplained errors. Same corpus feeds the format-coverage scan (§8.3) and search benchmarks.
5. **Upstream drift watch.** Nightly CI job runs the differential harness against `ScoopInstaller/Scoop@master` and current release; contract changes (like the v0.6.0 `scoop-*.json` rename) open issues automatically.
6. **Reuse classic's Pester suite where possible** (fixtures in `test/fixtures` are implementation-neutral) and port behavioral cases to Go tests; run PSScriptAnalyzer-era edge fixtures (malformed manifests, BOMs, CRLF) against the Go parsers.
7. **Benchmarks** vs. classic and vs. `scoop-search.exe`: cold/warm `search`, `list`, `update` (bucket sync), full install of a 20-package graph (wall time, with parallel downloads on). Publish targets: search ≤50 ms warm; update bucket-sync ≥5× classic; install wall-time ≥2× classic on multi-package transactions.

---

## 10. Deliberate Behavior Changes (explicit, documented)

Parity is the default; these are the only intentional deviations, each gated by config or clearly announced:

| Change | Why | Guard |
|---|---|---|
| Parallel downloads | key enhancement (pacman model) | `MAX_DOWNLOADS=1` restores serial behavior |
| Hash-failure isolates one package instead of aborting the whole transaction | parallel model makes global abort wasteful | `FORCE_UPDATE`/strict mode flag restores abort-all |
| RE2 regex instead of .NET regex in `scoop search` | Go stdlib | fallback-to-literal + WARN on unsupported syntax |
| aria2 ignored | native segmented downloader replaces it | config accepted w/ notice |
| `USE_SQLITE_CACHE` DB is maintained by Go code, schema-identical | pure-Go SQLite | none needed |
| `gscoop`-only keys: `MAX_DOWNLOADS`, `SPLIT_DOWNLOADS`, `SHALLOW_BUCKETS`, `USE_EXTERNAL_GIT`, `PSHOST`, `GSCOOP_CHANNEL` | new capabilities | classic ignores unknown keys — safe |
| Download resume (Range) | robustness | cache contract unchanged |

---

## 11. Phased Roadmap

**Phase 0 — Contract freeze & recon (2–3 wks).** Extract the state-format spec from `lib/` into `/spec` docs (this document's §2.2 is the seed); snapshot CLI help/usage text for all 28 commands; run the bucket-wide format-coverage scan; **gating spikes**: go-git↔git.exe repo round-trip, `bodgit/sevenzip` against a 7z fixture corpus, NTFS junction via Go, `msiexec /a` parity check. Exit: spikes green, spec reviewed.

**Phase 1 — Read-only core (3–4 wks).** `config`, paths/state engine, manifest engine + schema validation, `Compare-Version` + property tests, `bucket list/known`, `list`, `info`, `cat`, `which`, `prefix`, `depends`, `status`, `search` (scan + SQLite), `export`, `cache show`, `checkup`. Exit: differential harness green for all read-only commands.

**Phase 2 — Mutation engine (6–8 wks).** Downloader (sequential first, then pool + segments + hashes), pure-Go extraction stack (+ msiexec bridge), hook runner, install/uninstall/reset pipelines, shims, junctions, shortcuts, env vars, persist + ACL, `bucket add/rm` via go-git, `import`, `hold/unhold`, `download` command. Exit: differential + alternation harnesses green for install lifecycles; parallel-download benchmarks published.

**Phase 3 — Update & long tail (4–6 wks).** `update` (buckets parallel sync, app updates, `app@version` via git history, nightly semantics), self-update + classic-core co-update, `cleanup`, `alias`, `home`, `virustotal`, `shim` command; MSI native reader; Inno extractor (port) or finalized innounp-exception policy; `USE_EXTERNAL_GIT` fallback polish. Exit: full 28-command parity; alternation torture test at scale.

**Phase 4 — Hardening & distribution (3–4 wks).** `gscoop` manifest + shim-swap installer, `doctor` extension, AV-false-positive mitigation (signed binaries, shim provenance — shim.exe has AV history), docs, migration guide, upstream-drift CI in production. Optional: `checkver`/`autoupdate` maintainer tooling. Exit: v1.0 tagged; switch-back-to-classic drill documented and rehearsed.

Total: ~16–25 engineering weeks for one senior engineer; compressible with the fixture-first harness doing the heavy lifting.

---

## 12. Risk Register

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| R1 | go-git corner cases (auth helpers, gc, memory on giant clones) corrupt or degrade buckets | interop breakage | phase-0 round-trip gate; `USE_EXTERNAL_GIT` fallback; shallow-clone option; checkup pack-maintenance report |
| R2 | Inno/MSI/native-7z long tail blocks installs that classic handles | functional gap | phase-0 bucket scan quantifies; `USE_EXTERNAL_7ZIP` + msiexec bridges; staged native ports |
| R3 | Hook behavioral drift (PS 5.1 vs 7, variable visibility) breaks manifests | install failures | default to `powershell.exe`; differential hook fixtures; `PSHOST` escape |
| R4 | `Compare-Version` mismatch → wrong update/reset decisions | state corruption | property tests vs. PowerShell oracle, 100% match required |
| R5 | Mid-transaction crash leaves hybrid state classic misreads | corruption | staged rename atomicity; failed-state parity with classic `failed()`; startup consistency sweep |
| R6 | AV flags Go binary or bundled shim.exe | distribution failure | codesigning; reuse upstream shim binaries (already whitelisted by reputation); publish hashes |
| R7 | Upstream contract drift (e.g., another `install.json` rename) | silent divergence | nightly differential CI vs. upstream master (§9.5) |
| R8 | Two writers (one classic, one Go) interleave | corruption | documented limitation (same as classic-today); lock honored by GScoop side |
| R9 | Junction/shortcut/env edits diverge from PowerShell semantics (elevation, WM_SETTINGCHANGE broadcast, long paths) | subtle interop bugs | Win32-direct implementation (no shell-out), differential env snapshot tests |
| R10 | Download special-URL handlers (SourceForge mirrors etc.) drift | download failures | port `handle_special_urls` with per-handler fixtures; bucket-URL regression test |

---

## Appendix A. Classic install pipeline (authoritative order, from `lib/install.ps1`)

```
Get-Manifest → version checks (nightly → yyyyMMdd-HHmmss) → Get-SupportedArchitecture
→ SHOW_MANIFEST prompt (if configured) → ensure versiondir
→ Invoke-ScoopDownload (per-URL: cache lookup → fetch → hash check)
→ Invoke-Extraction (per-file dispatch; extract_dir/extract_to)
→ pre_install hook
→ Invoke-Installer (file+args | script; keep flag; then installer.script hook)
→ ensure_install_dir_not_in_path → link_current (junction) → create_shims
→ create_startmenu_shortcuts → install_psmodule → env_add_path → env_set
→ persist_data → persist_permission → post_install hook
→ save scoop-manifest.json + scoop-install.json → show notes
```

## Appendix B. Git operations contract (from §8.2 inventory)

Minimum `GitEngine` interface: `LsRemote(url)`, `Clone(url, dir, {branch, singleBranch, depth})`, `Pull(repo, {force, tags})`, `Fetch(repo, refspec, {force})`, `CheckoutCreate(repo, branch, track)`, `ResetHard(repo, rev)`, `Head(repo)`, `ConfigGet/Set(repo, key, value)`, `LogSince(repo, rev, {pathFilter, invertGrep})`, `DiffNameStatus(repo, a, b)`, `ShowFile(repo, rev, path)`, `Status(repo)`, `StashLike(repo)` (emulated). Implementations: `gogit` (default), `execgit` (fallback).

## Appendix C. Config keys consumed by GScoop

All keys in §2.4 plus: `MAX_DOWNLOADS` (int, default 4), `SPLIT_DOWNLOADS` (int, default 1), `SHALLOW_BUCKETS` (bool, default false), `USE_EXTERNAL_GIT` (bool, default false/auto), `PSHOST` (path, default `powershell.exe`), `GSCOOP_CHANNEL` (stable|nightly). Case-insensitive lookup; `scoop config rm <key>` and unknown-key tolerance identical to classic.
