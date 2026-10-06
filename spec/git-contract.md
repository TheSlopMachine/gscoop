# Git contract (Phase 0A)

Base tree for all citations: `C:\devel\Scoop`. Each claim cites `file:line`.

## 1. Transport

- All git traffic goes through `Invoke-Git -Path <dir> -ArgumentList <args>`, which
  prepends `-C <dir>` and shells to the resolved git binary (`lib/core.ps1:237-260`).
- The binary resolves via `Get-HelperPath -Helper Git`: Scoop-installed
  `git\mingw64\bin\git.exe` or `mingw32\bin\git.exe` first, else `git` from PATH
  (`lib/core.ps1:490-497`).
- `add_bucket` refuses without git (`lib/buckets.ps1:124-127`); core self-update aborts
  with the same requirement (`libexec/scoop-update.ps1:75`).
- Proxy `none` runs git directly; otherwise clone/checkout/pull/fetch/ls-remote run in
  a job with `HTTPS_PROXY`/`HTTP_PROXY` set, mapping `currentuser@` to `:@`
  (`lib/core.ps1:259-279`). `PROXY` parsing details: `lib/download.ps1:560-587`.
- Read-only log rendering uses `Invoke-GitLog` with
  `--no-pager log --color --no-decorate --grep=^(chore) --invert-grep --abbrev=12`
  plus a fixed pretty format (`lib/core.ps1:281-299`).

## 2. Minimum GitEngine interface

From the technical plan Appendix B, grounded in the call sites below:

```text
LsRemote(url)
Clone(url, dir, {branch, singleBranch, depth})
Pull(repo, {force, tags})
Fetch(repo, refspec, {force})
CheckoutCreate(repo, branch, track)
ResetHard(repo, rev)
Head(repo)
ConfigGet/Set(repo, key, value)
LogSince(repo, rev, {pathFilter, invertGrep})
DiffNameStatus(repo, a, b)
ShowFile(repo, rev, path)
Status(repo)
StashLike(repo)
```

Implementations: `gogit` (default), `execgit` (fallback).

## 3. Call-site inventory

### Buckets

- `add_bucket`: `ls-remote <repo>` probe, then `clone <repo> <dir> -q`; failures remove
  the target dir; SQLite cache refresh follows when enabled
  (`lib/buckets.ps1:149-169`). Duplicate names return 2, invalid repos return 1,
  success returns 0 (`lib/buckets.ps1:123-170`).
- Duplicate-remote detection reads `config --get remote.origin.url` per local bucket
  and compares normalized URIs via `Convert-RepositoryUri`
  (`lib/buckets.ps1:139-147`, `lib/buckets.ps1:83-102`).
- `list_buckets`: `config remote.origin.url` plus `log --format=%aI -n 1` per git
  bucket (`lib/buckets.ps1:109-111`).
- `rm_bucket`: plain directory removal plus `Remove-ScoopDBItem -Bucket <name>` when
  cached (`lib/buckets.ps1:172-186`).
- Issue helper: `config --get remote.origin.url`, rewritten to an `https://` GitHub
  issue URL (`lib/buckets.ps1:188-217`).

### Core self-update (`Sync-Scoop`)

- Fresh core: `clone -q <repo> --branch <branch> --single-branch <newdir>`, then
  rename `current` to `old` and `new` to `current`
  (`libexec/scoop-update.ps1:83-99`). Defaults `SCOOP_REPO` and `SCOOP_BRANCH` are
  written back to config when unset (`libexec/scoop-update.ps1:43-54`).
- Existing core: `rev-parse HEAD`; `config remote.origin.url`; `branch`; dirty check
  via `diff HEAD --name-only`; stash via `stash push -m <WIP o-date> -u -q` only under
  `AUTOSTASH_ON_CONFLICT`, else abort (`libexec/scoop-update.ps1:105-121`).
- Remote/branch change path: `config remote.origin.url <repo>`,
  `config remote.origin.fetch +refs/heads/*:refs/remotes/origin/*`,
  `fetch --force origin refs/heads/<b>:refs/remotes/origin/<b> -q`,
  `checkout -B <b> -t origin/<b> -q`, `reset --hard origin/<b> -q`
  (`libexec/scoop-update.ps1:123-137`). Steady path: `pull --tags --force -q`
  (`libexec/scoop-update.ps1:138-140`). Release shim rewrite closes the update
  (`libexec/scoop-update.ps1:152`).

### Bucket sync (`Sync-Bucket`)

- Non-git `main` is converted via remove plus re-add (`libexec/scoop-update.ps1:161-171`).
- Per git bucket: `rev-parse HEAD`, `pull -q`, optional `Invoke-GitLog`, then
  `diff --name-status <previousCommit>` to collect updated vs removed manifests for
  the SQLite refresh (`libexec/scoop-update.ps1:197-218`, second loop
  `libexec/scoop-update.ps1:228-234`). PowerShell 7 parallelizes across buckets with
  throttle limit 5 (`libexec/scoop-update.ps1:187-189`).

### Version-pinned installs (`Find-HistoricalManifestInGit`)

- Gate: `USE_GIT_HISTORY` default true (`lib/manifest.ps1:210-217`); bucket dir must
  contain `.git` (`lib/manifest.ps1:219-223`).
- HEAD fast path: `show HEAD:<relativeManifestPath>`; return when parsed version
  matches (`lib/manifest.ps1:233-244`).
- Pickaxe: `log --follow -n 1 --format=%H -G 'version.: .<version>' -- <path>`, with a
  `-S 'version: <version>'` literal fallback (`lib/manifest.ps1:245-258`). Both commits
  `$h^` and `$h` are shown and version-checked (`lib/manifest.ps1:261-274`).
- Orchestrator tries SQLite cache first, then git, then autoupdate
  (`lib/manifest.ps1:277-335`).

### Status, info, version

- `scoop status`: `fetch -q origin`, `branch --show-current`,
  `log HEAD..origin/<branch> --oneline` per bucket (`libexec/scoop-status.ps1:25-28`).
- `scoop info`: `log -1 -s --format=%aI#%an <manifest_path>`
  (`libexec/scoop-info.ps1:125`).
- `scoop --version`: core `log HEAD -1 --oneline` (non-master branch only) and per-bucket
  `log HEAD -1 --oneline` (`bin/scoop.ps1:21-40`).

## 4. Interop constraints for go-git

- Repositories stay ordinary git repositories: clone/pull/fetch/reset/checkout produce
  standard `.git` layouts readable by `git.exe fsck` and classic `scoop update`.
- `stash push -u` has no go-git equivalent; the ladder is: copy
  modified/untracked files to `workspace\.autostash\<timestamp>\`, hard reset, print
  restore instructions; refuse with the classic text otherwise.
- `USE_GIT_HISTORY` needs full history; a `SHALLOW_BUCKETS` (depth 1) option trades
  version-pinned installs for clone cost.
- Private buckets behind Windows credential helpers fall back to `execgit` via
  `USE_EXTERNAL_GIT`; go-git covers https token and ssh key paths.

## 5. Gaps and open verification

- Phase-0 gating spike: round-trip repos cloned by go-git vs `git.exe` (`fsck` clean,
  classic pull works, go-git pull works), long paths, symlinked manifests, and
  `core.autocrlf` neutrality.
- `scoop status` fetch cadence and `new_issue_msg` URL rewriting need golden-output
  fixtures before the update engine is ported.
