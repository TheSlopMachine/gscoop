# Install pipeline (Phase 0A)

Base tree for all citations: `C:\devel\Scoop`. Each claim cites `file:line`.

## 1. Preconditions

- `install_app($app, $architecture, $global, $suggested, $use_cache, $check_hash)`
  resolves `Get-Manifest` first and aborts when no manifest is found
  (`lib/install.ps1:8-13`).
- Empty version aborts; version characters outside `[\w.\-+]` abort
  (`lib/install.ps1:15-19`).
- `nightly` maps to `nightly-yyyyMMdd` via `nightly_version` and disables hash checks
  (`lib/install.ps1:1-6`, `lib/install.ps1:21-25`).
- `Get-SupportedArchitecture` selects the requested architecture when the manifest has
  a URL for it, else null aborts the install (`lib/install.ps1:27-31`,
  `lib/manifest.ps1:165-181`). `arm64` without an `arm64` mention falls back to `64bit`
  on Windows 11 (build >= 22000) and `32bit` on Windows 10 (`lib/manifest.ps1:165-177`).
- `SHOW_MANIFEST` prints the manifest and prompts before continuing, except under
  `scoop-update` (`lib/install.ps1:33-45`).

## 2. Authoritative install order

From `install_app` (`lib/install.ps1:46-82`):

```text
Get-Manifest -> version checks (nightly -> yyyyMMdd) -> Get-SupportedArchitecture
-> SHOW_MANIFEST prompt -> ensure versiondir
-> Invoke-ScoopDownload (per-URL: cache lookup -> fetch -> hash check)
-> Invoke-Extraction (per-file dispatch; extract_dir/extract_to)
-> pre_install hook
-> Invoke-Installer (file+args | script; keep flag; then installer.script hook)
-> ensure_install_dir_not_in_path -> link_current (junction) -> create_shims
-> create_startmenu_shortcuts -> install_psmodule -> env_add_path -> env_set
-> persist_data -> persist_permission -> post_install hook
-> save scoop-manifest.json + scoop-install.json -> show notes
```

Line mapping: version/arch checks (`lib/install.ps1:15-31`), prompt
(`lib/install.ps1:33-45`), `versiondir` plus `$original_dir`/`$persist_dir`
(`lib/install.ps1:48-50`), download (`lib/install.ps1:52`), extraction
(`lib/install.ps1:53`), `pre_install` (`lib/install.ps1:54`), installer plus PATH
guard plus junction plus shims plus shortcuts plus psmodule plus env
(`lib/install.ps1:56-63`), persist (`lib/install.ps1:66-67`), `post_install`
(`lib/install.ps1:69`), metadata plus success plus notes (`lib/install.ps1:71-81`).

## 3. Download stage

- `Invoke-ScoopDownload` collects all arch-specific URLs; installer archives sort first
  for `installer.args` handling (`lib/download.ps1:5-13`).
- Aria2 path is taken when `Test-Aria2Enabled`; otherwise each URL goes through
  `Invoke-CachedDownload` with abort on invalid URL (`lib/download.ps1:16-29`,
  `lib/download.ps1:262` for the enable check).
- Hash verification pairs each URL with `hash_for_url` (positional match against the
  arch URL list; missing index aborts) and `check_hash`; failure removes the cached
  file and aborts, with a SourceForge-specific retry hint (`lib/download.ps1:30-46`,
  `lib/download.ps1:715-726`).
- `check_hash` without a manifest hash warns and prints the file SHA256
  (`lib/download.ps1:728-733`); otherwise it resolves `md5|sha1|sha256|sha512`
  (bare hash defaults to sha256) and compares lowercase digests
  (`lib/download.ps1:736-759`, `lib/download.ps1:761-773`).
- `Invoke-CachedDownload` reuses the cache file named by `cache_path`, downloads to
  `$cached.download` then renames, prints `Loading ... from cache` on hit, and copies
  (cache on) or moves (cache off) into the version dir (`lib/download.ps1:54-70`).
- Request details: fragment stripped before fetch (`lib/download.ps1:91`);
  `User-Agent` from `Get-UserAgent` (`lib/download.ps1:556-558`); GitHub API assets use
  bearer token plus API version header (`lib/download.ps1:98-102`); manifest `cookie`
  values join as `name=value;...` (`lib/download.ps1:538-546`); `PRIVATE_HOSTS` entries
  add per-host headers (`lib/download.ps1:107-111`); token precedence is
  `SCOOP_GH_TOKEN`, `GH_TOKEN` config, `GH_TOKEN`, `GITHUB_TOKEN`
  (`lib/download.ps1:589-591`).
- Special URLs: FossHub API resolution, SourceForge reshaping to
  `downloads.sourceforge.net`, private GitHub release asset resolution
  (`lib/download.ps1:604-641`).
- Proxy setup honors `none`, `default`, `currentuser`, and `user:password@host:port`
  forms (`lib/download.ps1:560-587`); `Invoke-Git` applies the same proxy to
  clone/checkout/pull/fetch/ls-remote (`lib/core.ps1:252-279`).

## 4. Extraction stage

- `Invoke-Extraction` pairs each downloaded file with arch-specific `extract_dir` /
  `extract_to`, dispatches by file name, and passes `-Removal`
  (`lib/decompress.ps1:3-61`).
- `.zip` uses 7z when the helper or external 7z is available, else the .NET zip path
  (`lib/decompress.ps1:26-33`). `.msi` uses the MSI path (`lib/decompress.ps1:34-37`).
  `.exe` extracts only when `innosetup` is set (`lib/decompress.ps1:38-43`). All other
  7z-detectable names use the 7z path (`lib/decompress.ps1:44-47`).
- 7z extraction excludes `*.nsis`, handles nested tar in a second pass, and logs to
  `7zip.log` (`lib/decompress.ps1:63-120`). `USE_EXTERNAL_7ZIP` resolves `7z` from PATH
  and aborts with a config hint when missing (`lib/decompress.ps1:83-91`).
- MSI uses lessmsi when `USE_LESSMSI` is set, else `msiexec /a`
  (`lib/decompress.ps1:169` plus helpers); Inno uses the innounp helper
  (`lib/decompress.ps1:227` plus helpers).

## 5. Hooks and installer execution

- `Invoke-HookScript` accepts `installer`, `pre_install`, `post_install`,
  `uninstaller`, `pre_uninstall`, `post_uninstall`; `installer`/`uninstaller` read the
  nested `.script` property; scripts run in-process via `scriptblock`
  (`lib/install.ps1:143-168`).
- `Invoke-Installer` requires `installer.file` to resolve inside the app dir, runs
  `.ps1` directly and other binaries through `Invoke-ExternalCommand`, aborts install
  (or uninstall) on nonzero exit, honors the `keep` flag, then runs the
  `installer.script` hook (`lib/install.ps1:84-141`).
- `$dir`, `$global`, `$version` tokens in `installer.args` are substituted before
  execution (`lib/install.ps1:117-122`).

## 6. Link, shim, persist, metadata

- `ensure_install_dir_not_in_path` runs before linking (`lib/install.ps1:56-58`,
  `lib/install.ps1:279-294`).
- `link_current` flips the `current` junction (`lib/install.ps1:58`,
  `lib/install.ps1:234-254`).
- `create_shims` writes the shim triple with substituted args (`lib/install.ps1:59`,
  `lib/install.ps1:176-193`). Start-menu shortcuts and psmodule install follow
  (`lib/install.ps1:60-61`).
- `env_add_path` and `env_set` apply manifest environment changes (`lib/install.ps1:62-63`,
  `lib/install.ps1:309-342`).
- `persist_data` links persisted files/dirs, `persist_permission` fixes global ACLs
  (`lib/install.ps1:66-67`, `lib/install.ps1:444-530`).
- `post_install` runs before metadata is saved (`lib/install.ps1:69-73`); the commit
  point is `save_installed_manifest` plus `save_install_info`
  (`lib/manifest.ps1:123-147`); success text and notes close the install
  (`lib/install.ps1:79-81`, `lib/install.ps1:355-362`).

## 7. Failed installs

- A version directory without `current` linkage and without install metadata satisfies
  `failed()` (`lib/core.ps1:425-430`).
- `ensure_none_failed` repairs versioned-but-unlinked apps via `scoop-reset` and purges
  unversioned leftovers via `scoop-uninstall` before new installs
  (`lib/install.ps1:380-400`).
- `Confirm-InstallationStatus` rejects missing apps and reports failed installs as
  errors before mutation (`lib/core.ps1:1167-1204`).

## 8. Uninstall order (reverse)

From `scoop-uninstall.ps1:48-150`:

```text
Confirm-InstallationStatus -> Select-CurrentVersion
-> pre_uninstall hook -> test_running_process
-> Invoke-Installer (uninstaller) -> rm_shims -> rm_startmenu_shortcuts
-> unlink_current -> uninstall_psmodule -> env_rm_path -> env_rm
-> unlink_persist_data -> Remove-Item versiondir -> post_uninstall hook
-> remove older versions -> remove current link -> remove empty appdir
-> purge persist dir only with -p
```

- Manifest and architecture come from installed metadata, not the bucket
  (`libexec/scoop-uninstall.ps1:59-62`).
- Running processes abort the app loop unless `IGNORE_RUNNING_PROCESSES`
  (`libexec/scoop-uninstall.ps1:67-70`, `lib/install.ps1:533-550`).
- Persisted data survives unless `-p/--purge` (`libexec/scoop-uninstall.ps1:134-147`).
- `scoop` itself delegates to `bin/uninstall.ps1` (`libexec/scoop-uninstall.ps1:40-43`).

## 9. Gaps and open verification

- Shortcut creation (`create_startmenu_shortcuts`) and psmodule install/remove
  (`install_psmodule`, `uninstall_psmodule`) are called in the orders above; their
  internal file operations need line-level fixtures in the mutation-engine phase.
- `aria2` segmented-download arguments and FTP size probes (`ftp_file_size`,
  `lib/download.ps1:685-689`) need per-handler fixtures before porting special-URL
  behavior.
