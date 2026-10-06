# Config keys (Phase 0A)

Base tree for all citations: `C:\devel\Scoop`. Each claim cites `file:line`.

## 1. Storage and lookup

- `get_config` lowercases the name, so lookup is case-insensitive; a supplied default
  applies only when the stored value is null (`lib/core.ps1:118-124`).
- `set_config` lowercases the name, creates the config object plus parent dirs on first
  use, converts `"True"`/`"False"` strings to booleans, runs `Complete-ConfigChange`,
  adds or updates the property, removes the property on null, and persists with
  UTF8-no-BOM JSON (`lib/core.ps1:126-160`).
- `scoop config` with no args dumps the object; `scoop config rm <name>` removes the
  key; get prints `'<name>' is not set` for null; DateTime values print in `o` format
  (`libexec/scoop-config.ps1:166-191`).
- Unknown keys are tolerated: classic reads known keys and ignores the rest, so
  Go-only keys are safe to add (`lib/core.ps1:118-124`, `libexec/scoop-config.ps1:166-189`).

## 2. Consumed keys

Documented in `scoop config` help (`libexec/scoop-config.ps1:21-164`); behavior sites
follow each entry.

| Key | Type / default | Behavior |
|---|---|---|
| `ALIAS` | object, default `{}` | alias name to `scoop-<name>` script (`lib/commands.ps1:52-93`) |
| `ARIA2_ENABLED` (`aria2-enabled`) | bool, default true | select Aria2 downloader (`lib/download.ps1:262`, `libexec/scoop-config.ps1:134-136`) |
| `ARIA2_WARNING_ENABLED` | bool, default true | Aria2 warning text (`libexec/scoop-config.ps1:138-140`) |
| `ARIA2_FALLBACK_ENABLED` (`aria2-fallback-enabled`) | bool, default true | fall back on Aria2 failure (`lib/download.ps1:459`, `libexec/scoop-config.ps1:142-144`) |
| `ARIA2_RETRY_WAIT` | intish, `2` | seconds between retries (`libexec/scoop-config.ps1:146-148`) |
| `ARIA2_SPLIT` | intish, `5` | connections per download (`libexec/scoop-config.ps1:150-152`) |
| `ARIA2_MAX_CONNECTION_PER_SERVER` | intish, `5` | per-server cap (`libexec/scoop-config.ps1:154-156`) |
| `ARIA2_MIN_SPLIT_SIZE` | size, `5M` | split threshold (`libexec/scoop-config.ps1:158-160`) |
| `ARIA2_OPTIONS` | array | extra Aria2 options (`libexec/scoop-config.ps1:162-164`) |
| `AUTOSTASH_ON_CONFLICT` | bool, default false | stash core changes on update (`libexec/scoop-update.ps1:114-121`, `libexec/scoop-config.ps1:58-60`) |
| `CACHE_PATH` | path | overrides `$cachedir` after `SCOOP_CACHE` (`lib/core.ps1:1385`, `libexec/scoop-config.ps1:88-89`) |
| `CAT_STYLE` | string | `bat --style` for manifest display (`libexec/scoop-cat.ps1:19`, `lib/install.ps1:35`, `libexec/scoop-config.ps1:100-104`) |
| `DEBUG` | bool, default false | enables `debug()` with `SCOOP_DEBUG` (`lib/core.ps1:315-318`, `libexec/scoop-config.ps1:66-67`) |
| `DEFAULT_ARCHITECTURE` | `64bit\|32bit\|arm64` | overrides OS-derived arch (`lib/core.ps1:1132-1152`, `libexec/scoop-config.ps1:62-64`) |
| `FORCE_UPDATE` | bool, default false | `outdated` on any version difference (`lib/core.ps1:591-594`, `libexec/scoop-config.ps1:69-70`) |
| `GH_TOKEN` | string | GitHub auth, see precedence below (`lib/download.ps1:589-591`, `libexec/scoop-config.ps1:91-94`) |
| `GLOBAL_PATH` | path | overrides `$globaldir` after `SCOOP_GLOBAL` (`lib/core.ps1:1378`, `libexec/scoop-config.ps1:85-86`) |
| `HOLD_UPDATE_UNTIL` | ISO-8601 `o` | holds Scoop core self-update (`lib/core.ps1:1263-1284`, `libexec/scoop-config.ps1:116-120`) |
| `IGNORE_RUNNING_PROCESSES` | bool | warn instead of blocking on running apps (`lib/install.ps1:538-550`, `libexec/scoop-config.ps1:106-109`) |
| `LAST_UPDATE` | ISO-8601 `o` | staleness marker, 3-hour TTL (`lib/core.ps1:1251-1261`, `libexec/scoop-update.ps1:408-423`) |
| `NO_JUNCTION` | bool | disable `current` junctions (`lib/core.ps1:373-383`, `lib/versions.ps1:53`, `libexec/scoop-config.ps1:38-39`) |
| `PRIVATE_HOSTS` | array of `{match, headers}` | per-host request headers (`lib/download.ps1:107-111`, `libexec/scoop-config.ps1:111-114`) |
| `PROXY` | `none\|default\|[user@]host:port` | proxy for downloads and git network ops (`lib/core.ps1:252-279`, `lib/download.ps1:560-587`, `libexec/scoop-config.ps1:50-56`) |
| `ROOT_PATH` | path | overrides `$scoopdir` after `SCOOP` (`lib/core.ps1:1375`, `libexec/scoop-config.ps1:82-83`) |
| `SCOOP_BRANCH` | string, default `master` | core update branch (`libexec/scoop-update.ps1:50-54`, `libexec/scoop-config.ps1:45-48`) |
| `SCOOP_REPO` | URL, default `https://github.com/ScoopInstaller/Scoop` | core update remote (`libexec/scoop-update.ps1:43-47`, `libexec/scoop-config.ps1:41-43`) |
| `SHIM` | `kiennq\|scoopcs\|71`, default `kiennq` | shim binary variant (`lib/core.ps1:1120-1130`, `libexec/scoop-config.ps1:79-80`) |
| `SHOW_MANIFEST` | bool, default false | prompt with manifest before install (`lib/install.ps1:33-45`, `libexec/scoop-config.ps1:75-77`) |
| `SHOW_UPDATE_LOG` | bool, default true | commit log on update (`libexec/scoop-update.ps1:62`, `libexec/scoop-config.ps1:72-73`) |
| `UPDATE_NIGHTLY` | bool | date-compare nightly versions (`lib/versions.ps1:155-163`, `libexec/scoop-config.ps1:122-124`) |
| `USE_EXTERNAL_7ZIP` | bool | `7z` from PATH instead of helper app (`lib/decompress.ps1:27-33`, `lib/decompress.ps1:83-91`, `libexec/scoop-config.ps1:24-25`) |
| `USE_GIT_HISTORY` | bool, default true | resolve `app@version` from bucket git history (`lib/manifest.ps1:210-217`, `lib/manifest.ps1:292-303`, `libexec/scoop-config.ps1:33-36`) |
| `USE_ISOLATED_PATH` | bool/string | isolate app PATH entries under a named variable (`lib/core.ps1:174-227`, `lib/core.ps1:1388-1392`, `libexec/scoop-config.ps1:126-129`) |
| `USE_LESSMSI` | bool | prefer lessmsi over `msiexec /a` (`lib/decompress.ps1:191`, `libexec/scoop-config.ps1:27-28`) |
| `USE_SQLITE_CACHE` | bool | maintain `scoop.db` on bucket/install/update/search paths (`lib/core.ps1:229-234`, `lib/buckets.ps1:164-186`, `lib/manifest.ps1:281-303`, `libexec/scoop-config.ps1:30-31`) |
| `VIRUSTOTAL_API_KEY` | string | VirusTotal scan auth (`libexec/scoop-virustotal.ps1:68`, `libexec/scoop-config.ps1:96-98`) |

## 3. Environment precedence

- Root: `$env:SCOOP` beats `ROOT_PATH` beats checkout grandparent beats `~\scoop`
  (`lib/core.ps1:1375`). Global: `$env:SCOOP_GLOBAL` beats `GLOBAL_PATH` beats
  `CommonApplicationData\scoop` (`lib/core.ps1:1378`). Cache: `$env:SCOOP_CACHE` beats
  `CACHE_PATH` beats `$scoopdir\cache` (`lib/core.ps1:1385`).
- Config home: first of `$env:XDG_CONFIG_HOME`, `~\.config` (`lib/core.ps1:1347`).
- GitHub token: `$env:SCOOP_GH_TOKEN`, `GH_TOKEN` config, `$env:GH_TOKEN`,
  `$env:GITHUB_TOKEN` (`lib/download.ps1:589-591`).
- Debug: `DEBUG` config or `$env:SCOOP_DEBUG` (`lib/core.ps1:316`).

## 4. Side effects

`Complete-ConfigChange` (`lib/core.ps1:162-235`):

- `USE_ISOLATED_PATH`: no-op when unchanged; disabling moves entries back to PATH and
  clears the isolated variable; enabling moves `apps\*` entries under the named
  variable (default `SCOOP_PATH`), for user and (as admin) global scopes
  (`lib/core.ps1:174-227`).
- `USE_SQLITE_CACHE=true`: builds the cache via `Set-ScoopDB`
  (`lib/core.ps1:229-234`).
- All other keys persist without side effects.

## 5. Go-only additions (new keys, ignored by classic)

Per the technical plan Appendix C: `MAX_DOWNLOADS` (int, default 4),
`SPLIT_DOWNLOADS` (int, default 1), `SHALLOW_BUCKETS` (bool, default false),
`USE_EXTERNAL_GIT` (bool, default false/auto), `PSHOST` (path, default
`powershell.exe`), `GSCOOP_CHANNEL` (`stable|nightly`). `scoop config rm <key>`
removes any of these identically (`libexec/scoop-config.ps1:172-174`).

## 6. Gaps and open verification

- Exact string/boolean coercion beyond `"True"`/`"False"` (`lib/core.ps1:140-142`)
  needs a truth table fixture (e.g. `1`/`0`, `yes`/`no`) before porting the setter.
- `aria2` key spellings use both `aria2-*` and `ARIA2_*` forms across help and code
  (`libexec/scoop-config.ps1:131-164`, `lib/download.ps1:459`); the Go config layer
  keeps the documented lowercase-lookup behavior of `get_config`.
