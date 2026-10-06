# State contract (Phase 0A)

Base tree for all citations: `C:\devel\Scoop`. Each claim cites `file:line`.
Decisions: Go 1.22 LTS, `CGO_ENABLED=0`, local-only module `gscoop` (`go.mod`).

## 1. Root derivation

- `$scoopdir` selects the first non-empty value of `$env:SCOOP`, `ROOT_PATH` config,
  the grandparent directory of the core checkout, `~\scoop` (`lib/core.ps1:1375`).
- `$globaldir` selects the first non-empty value of `$env:SCOOP_GLOBAL`, `GLOBAL_PATH`
  config, `C:\ProgramData\scoop` (`lib/core.ps1:1378`).
- `$cachedir` selects the first non-empty value of `$env:SCOOP_CACHE`, `CACHE_PATH`
  config, `$scoopdir\cache` (`lib/core.ps1:1385`).
- `basedir($global)` returns `$globaldir` for global scope, `$scoopdir` otherwise
  (`lib/core.ps1:366`).
- Config file path is `$XDG_CONFIG_HOME\scoop\config.json`, falling back to
  `~\.config\scoop\config.json` (`lib/core.ps1:1347-1348`). When the core runs from
  `apps\scoop\current` and `<root>\config.json` exists, that portable path wins
  (`lib/core.ps1:1349-1372`).

## 2. Root layout

Derived directories (`lib/core.ps1:366-387`, `lib/buckets.ps1:1`):

```text
<root>\
  apps\<app>\<version>\        versiondir (lib/core.ps1:371)
  apps\<app>\current\          directory junction, absent under NO_JUNCTION
  persist\<app>\...            persistdir (lib/core.ps1:385)
  cache\<app>#<version>#<id>   cachedir file (lib/core.ps1:388-403)
  buckets\<name>\[.git]        bucketsdir (lib/buckets.ps1:1)
  shims\<name>.*               shimdir (lib/core.ps1:368)
  modules\                     modulesdir (lib/core.ps1:369)
  workspace\<app>.json         usermanifestsdir/usermanifest (lib/core.ps1:386-387)
  scoop.db                     Join-Path $scoopdir 'scoop.db' (lib/database.ps1:87)
```

- `appsdir` is `$(basedir)\apps`, `appdir` appends `\<app>` (`lib/core.ps1:367-370`).
- `versiondir` appends `\<version>` to `appdir` (`lib/core.ps1:371`).
- `usermanifestsdir` is `$(basedir)\workspace` (user scope only, no global variant)
  (`lib/core.ps1:386`).
- Known-bucket registry is `buckets.json` at the repo root (`buckets.json:1-12`,
  `lib/buckets.ps1:37-46`).

## 3. Buckets

- `Find-BucketDirectory -Name <name> [-Root]` returns `$bucketsdir\<name>`, or
  `$bucketsdir\<name>\bucket` when that subdirectory exists and `-Root` is absent
  (`lib/buckets.ps1:3-29`). Empty bucket name defaults to `main` (`lib/buckets.ps1:18-21`).
- Each bucket root carries `.git` for cloned buckets; `list_buckets` reports
  `remote.origin.url` plus `log --format=%aI -n 1` for git buckets, otherwise the local
  path and directory timestamp (`lib/buckets.ps1:104-121`).
- Manifest lookup scans bucket directories recursively for `<sanitary app>.json`
  (`lib/buckets.ps1:52-54`, `lib/manifest.ps1:1-3`).
- `Get-LocalBucket` lists subdirectories of `$bucketsdir`, ordered with known buckets
  first (`lib/buckets.ps1:56-75`).

## 4. Per-version metadata

- `save_installed_manifest` writes `$dir\scoop-manifest.json`: for URL installs it
  downloads the manifest bytes and decodes them with the response charset, otherwise it
  copies `manifest_path` (`lib/manifest.ps1:123-132`).
- `installed_manifest` returns `scoop-manifest.json` when present, else falls back to
  legacy `manifest.json` (`lib/manifest.ps1:134-139`).
- `save_install_info` removes null-valued keys, formats with `ConvertToPrettyJson`,
  and writes `$dir\scoop-install.json` with `WriteAllLines` (`lib/manifest.ps1:141-147`).
- `install_info` returns `scoop-install.json` when present, else falls back to legacy
  `install.json` (`lib/manifest.ps1:149-154`).
- `install_app` records `@{ architecture; url; bucket }` through `save_install_info`
  (`lib/install.ps1:71-73`). `hold:true` is added by `scoop hold` (section 7).
- `ConvertToPrettyJson` emits 4-space indent, CRLF line endings, `": "` after colons,
  and normalizes single-element arrays to scalars before serialization
  (`lib/json.ps1:6-93`, `lib/json.ps1:161-214`).

## 5. Cache naming

`cache_path($app, $version, $url)` (`lib/core.ps1:388-403`):

1. `underscoredUrl = url -replace '[^\w\.\-]+', '_'`.
2. Candidate is `Join-Path $cachedir "$app#$version#$underscoredUrl"`.
3. When that legacy file exists on disk, it wins and is returned unchanged.
4. Otherwise the identifier is `sha256(UTF8(url))`, lowercased, first 7 hex characters,
   plus `[System.IO.Path]::GetExtension($url)`; the legacy tail is replaced by that
   value. Go checks the legacy form first, permanently.
- Download flow copies the cache file to `$dir\<url_filename>` when caching is on,
  moves it when caching is off (`lib/download.ps1:54-70`). In-flight data uses
  `$cached.download` plus `Move-Item` (`lib/download.ps1:57-60`).
- `url_filename` is `(Split-Path $url -Leaf).split('?')[0]` (`lib/download.ps1:691-693`).

## 6. Installed-version resolution

- `Select-CurrentVersion` (`lib/versions.ps1:31-71`): with junctions enabled, read
  `$appdir\current\scoop-manifest.json`, falling back to `manifest.json`
  (`lib/versions.ps1:54-55`); when the manifest version is `nightly`, return the leaf
  name of the junction target (`lib/versions.ps1:57-59`). When no junction version is
  available, return the last entry of `Get-InstalledVersion`, else null
  (`lib/versions.ps1:61-68`). `NO_JUNCTION` skips the junction read (`lib/versions.ps1:53`).
- `Get-InstalledVersion` (`lib/versions.ps1:96-101`): collect
  `$appPath\*\scoop-install.json` and `$appPath\*\install.json`, sort by
  `LastWriteTimeUtc`, take unique parent directory names, exclude `current` and
  `_*.old*`. Output order is oldest to newest (`lib/versions.ps1:80-83`).
- `installed($app, $global)` (`lib/core.ps1:407-416`): null scope queries both scopes;
  strips a `bucket/` prefix; true when `Select-CurrentVersion` returns non-null.
- `installed_apps` lists container children of `appsdir`, excluding `scoop`
  (`lib/core.ps1:417-422`).
- `failed($app, $global)` (`lib/core.ps1:425-430`): `hasCurrent` is true under
  `NO_JUNCTION` or when `current` exists; failed when the app path exists without
  (`hasCurrent` and `installed`).
- `currentdir` (`lib/core.ps1:373-383`): under `NO_JUNCTION` (except app `scoop`)
  resolve via `Select-CurrentVersion`, otherwise use `current`.
- `app_status` (`lib/core.ps1:567-611`): `hold` is true when
  `install_info.hold -eq $true` (`lib/core.ps1:576`); `outdated` uses `Compare-Version`
  with `>` semantics, or `!= 0` under `FORCE_UPDATE` (`lib/core.ps1:589-596`);
  `missing_deps` lists `depends` entries that are not installed (`lib/core.ps1:598-609`).

## 7. Hold

- `scoop hold scoop` sets `HOLD_UPDATE_UNTIL` to `Now.AddDays(1)` in ISO-8601 `o`
  format (`libexec/scoop-hold.ps1:34-39`).
- `scoop hold <app>` uses version `current`, or `Select-CurrentVersion` under
  `NO_JUNCTION`; copies existing install-info properties into a hashtable, sets
  `hold = $true`, writes with `save_install_info` (`libexec/scoop-hold.ps1:49-68`).
- `scoop unhold scoop` removes `HOLD_UPDATE_UNTIL` via `set_config ... $null`
  (`libexec/scoop-unhold.ps1:35-39`). Per-app unhold sets `hold = $null`, which
  `save_install_info` strips (`libexec/scoop-unhold.ps1:60-67`, `lib/manifest.ps1:141-143`).
- `Test-ScoopCoreOnHold` (`lib/core.ps1:1263-1284`): null means not held; a future date
  skips self-update; an expired date re-enables updates; an unparsable value is removed
  with an error.
- `LAST_UPDATE` uses the same `o` format (`libexec/scoop-update.ps1:408-423`);
  `is_scoop_outdated` treats age >= 3 hours as stale (`lib/core.ps1:1251-1261`).

## 8. Shims

- `shim($path, $global, $name, $arg)` aborts when the target is missing, ensures the
  scope shim directory, adds it to PATH, defaults the name to the extensionless target
  file name, and lowercases it (`lib/core.ps1:949-955`).
- For `.exe`/`.com` targets: warn on overwrite, byte-copy the selected shim binary to
  `$name.exe`, write `path = "<resolved>"` plus optional `args = <arg>` into
  `$name.shim` (`lib/core.ps1:963-990`). When the shim PE machine is `0x014c` (I386)
  on a 64-bit OS, rewrite `System32` to `Sysnative` and `SysWOW64` to `System32`
  (`lib/core.ps1:970-985`). When the target subsystem is 2 (GUI), patch the shim copy
  to GUI as well (`lib/core.ps1:992-997`).
- `.bat`/`.cmd` targets produce `$name.cmd` plus a extensionless POSIX wrapper
  (`lib/core.ps1:998-1011`). `.ps1` targets produce `$name.ps1` (relative-path form when
  possible), a `pwsh`/`powershell` `.cmd` launcher, and a POSIX wrapper
  (`lib/core.ps1:1012-1054`). `.jar` and `.py` have dedicated `.cmd` plus POSIX
  wrappers (`lib/core.ps1:1055-1088`). All other targets get a `bash`/`wslpath`/`cygpath`
  `.cmd` pair (`lib/core.ps1:1089-1117`).
- `get_shim_path` selects the binary by `SHIM` config (default `kiennq`) from
  `apps\scoop\current\supporting\shims\{scoopcs,71,kiennq}\shim.exe`
  (`lib/core.ps1:1120-1130`). Bundled payloads on disk: `supporting/shims/71/shim.exe`,
  `supporting/shims/kiennq/shim.exe`, `supporting/shims/scoopcs/shim.exe`.
- PE helpers: `Get-PEMachine` reads the `0x3C` offset then a UInt16 at `peOffset + 4`
  (`lib/core.ps1:24-42`); `Get-PESubsystem` and `Set-PESubsystem` use the subsystem field
  (`lib/core.ps1:1-68`).
- `Get-ShimTarget` reads the first `path = ...` line from `.shim` files, else the
  `@rem`/`#` comment line from wrappers, and maps `\Sysnative\` back to `\System32\`
  on read (`lib/core.ps1:911-928`).
- Overwrite backup: `warn_on_overwrite` returns early for missing shims or same-app
  owners; otherwise renames the existing shim to `$shim.$shim_app`
  (`lib/core.ps1:930-947`). `rm_shim` iterates `''`, `.shim`, `.cmd`, `.ps1`, removes
  `$name<suffix>.$app` backups first, restores the newest backup when the primary is
  removed, and removes `$name.exe` only when no backups remain
  (`lib/install.ps1:195-216`).
- `create_shims` resolves each arch-specific `bin` entry via `shim_def` (arrays pass
  through as `(target, name, args)`; strings default to extensionless name and null args)
  (`lib/install.ps1:171-193`), prefers `$dir\$target`, then absolute path, then
  `Get-Command`, and substitutes `$dir`/`$original_dir`/`$persist_dir` in args
  (`lib/install.ps1:176-193`).
- Alias shims are `shims\scoop-<alias>.ps1` files registered in the `ALIAS` config
  (`lib/commands.ps1:43-75`); command discovery also scans `$scoopdir\shims` for
  `scoop-*.ps1` (`lib/commands.ps1:5-8`).

## 9. SQLite cache

- Database file: `Join-Path $scoopdir 'scoop.db'` (`lib/database.ps1:87`).
- DDL, created when absent (`lib/database.ps1:93-104`):

```sql
CREATE TABLE IF NOT EXISTS 'app' (
    name TEXT NOT NULL COLLATE NOCASE,
    description TEXT NOT NULL,
    version TEXT NOT NULL,
    bucket VARCHAR NOT NULL,
    manifest JSON NOT NULL,
    binary TEXT,
    shortcut TEXT,
    dependency TEXT,
    suggest TEXT,
    PRIMARY KEY (name, version, bucket)
)
```

- Writes use `INSERT OR REPLACE INTO app (<cols>) VALUES (@<cols>)` inside a
  transaction (`lib/database.ps1:124-162`).
- `Set-ScoopDB` derives `binary` from arch-specific `bin`, `shortcut` from
  arch-specific `shortcuts`, `dependency` from `depends`, `suggest` from flattened
  `suggest` values (`lib/database.ps1:180-242`).
- Reads: `Find-ScoopDBItem` matches `%pattern%` against caller-selected columns and
  keeps the latest row per `(name, bucket)` (`lib/database.ps1:262-295`);
  `Get-ScoopDBItem` selects by `name`/`bucket`, optionally `version`
  (`lib/database.ps1:317-364`); `Remove-ScoopDBItem` deletes by bucket, optionally name
  (`lib/database.ps1:468-507`). Latest-row selection compares with `Compare-Version`
  (`lib/database.ps1:381-450`).

## 10. Compare-Version

Port character-for-character from `lib/versions.ps1:110-254`:

1. Replace `+` with `-` in both inputs (`lib/versions.ps1:142`); equal strings return 0
   (`lib/versions.ps1:145-147`).
2. Split with `SplitVersion` (`lib/versions.ps1:150-151`): wrap each letter run with the
   delimiter, split on the literal delimiter, drop empties, convert digit runs to Long
   (`lib/versions.ps1:251-253`).
3. Dual `nightly` returns 0 unless `UPDATE_NIGHTLY` is set; then missing date parts
   default to today `yyyyMMdd` and the result is `Sign(difference - reference)`
   (`lib/versions.ps1:153-167`).
4. Loop to the longer length (`lib/versions.ps1:169-226`):
   - Missing reference part: `-1` when the difference part matches
     `alpha|beta|rc|pre`, else `1` (`lib/versions.ps1:171-177`).
   - Missing difference part: mirror image (`lib/versions.ps1:179-185`).
   - Parts containing `.` recurse with delimiter `.`; parts containing `_` recurse
     with delimiter `_`; equal recursion continues (`lib/versions.ps1:188-207`).
   - Long values compared against strings are stringified first
     (`lib/versions.ps1:210-217`); `-gt` returns 1, `-lt` returns -1
     (`lib/versions.ps1:220-225`). Equal parts fall through; fully equal inputs return
     null (treated as 0 by callers such as `lib/database.ps1:400` and
     `lib/core.ps1:592-594`).

## 11. Junctions, persist, environment

- `link_current` returns the version dir under `NO_JUNCTION`, rejects a version named
  `current`, replaces an existing junction, creates the junction, and sets read-only
  (`lib/install.ps1:234-254`). `unlink_current` clears read-only and removes the
  junction (`lib/install.ps1:261-276`).
- `persist_data` moves existing source content into `persist\<app>`, creates missing
  targets as directories, then links directories as junctions (read-only) and files as
  hard links (`lib/install.ps1:444-494`). `unlink_persist_data` removes those links
  before directory removal (`lib/install.ps1:496-518`). Global persist roots get a
  BuiltinUsers write rule when run as admin (`lib/install.ps1:521-530`).
- `env_add_path` joins manifest paths under the install dir and appends them through
  the isolated-path variable when configured (`lib/install.ps1:309-319`); `env_set`
  expands and sets variables (`lib/install.ps1:331-342`); removal mirrors both
  (`lib/install.ps1:321-353`).
- `ensure_install_dir_not_in_path` strips the install dir from PATH after installers
  run (`lib/install.ps1:279-294`).
- `test_running_process` blocks update/uninstall while app processes run, unless
  `IGNORE_RUNNING_PROCESSES` is set (`lib/install.ps1:533-550`).

## 12. Gaps and open verification

- Exact byte content of bundled `shim.exe` variants is payload data; Go embeds the
  same files without modification.
- Shortcut (COM `WScript.Shell`/`IShellLink`) and psmodule copy semantics live in
  `lib/shortcuts.ps1` and `lib/psmodules.ps1`; this spec defers their line-level
  mapping to the mutation-engine phase.
- `schema.json` remains the manifest contract (`schema.json:2` declares the
  `http://scoop.sh/draft/schema#` id); validator parity is a manifest-engine task.
