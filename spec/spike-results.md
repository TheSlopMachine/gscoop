# Phase 0B spike results

Date: 2026-10-06. Machine: Windows 10 10.0.19045, PowerShell 7.6.4,
Go toolchain 1.27.1 with `go 1.22` module directive, `CGO_ENABLED`
unset (default enabled; no cgo packages in the dependency graph),
git.exe 2.56.0.windows.1, 7z 26.02 used only for 7z fixture
generation.

## Dependencies

| Module | Version | Reason |
|---|---|---|
| `github.com/go-git/go-git/v5` | v5.13.2 | Newest release with a `go` directive at or below 1.22 (`go 1.21`). Later releases require go 1.23+. |
| `github.com/bodgit/sevenzip` | v1.6.1 | Newest release with a `go` directive at or below 1.22 (`go 1.21`). v1.6.2+ requires go 1.25. |
| `golang.org/x/sys` | v0.30.0 | Newest release with a `go` directive at or below 1.22 (`go 1.18`). v0.31.0+ requires go 1.23. |

All three are pure Go. `bodgit/sevenzip` pulls `bodgit/windows`,
`ulikunitz/xz`, `klauspost/compress`, `pierrec/lz4/v4`, and
`andybalholm/brotli` as format codecs; none use cgo.

## Spike verdicts

| Spike | Location | Verdict |
|---|---|---|
| go-git round-trip | `spikes/gogit/` | PASS (16/16 checks) |
| archive extraction | `spikes/sevenzip/` | PASS (6/6 executed checks) |
| NTFS junction | `spikes/junction/` | PASS (6/6 checks) |
| msiexec parity | `spikes/msiexec/NOTE.md` | Document only; no installer executed |

## go-git round-trip: PASS

The spike initialises an origin repository with go-git, clones it once
with go-git and once with git.exe, then alternates writers: a go-git
commit pulled by git.exe, followed by a git.exe commit pulled by
go-git. `git fsck` runs in all three repositories before and after.

```
PASS go-git-init-commit
PASS go-git-clone
PASS git-exe-clone
PASS fsck-initial:origin
PASS fsck-initial:clone-gogit
PASS fsck-initial:clone-exe
PASS git-exe-pull-after-gogit-commit
PASS heads-converge-after-gogit-commit
PASS git-exe-commit-origin
PASS git-exe-rev-parse
PASS go-git-pull-after-exe-commit
PASS git-exe-pull-after-exe-commit
PASS fsck-final:origin
PASS fsck-final:clone-gogit
PASS fsck-final:clone-exe
PASS heads-converge-final
```

Both directions converge on identical HEAD revisions and every `fsck`
exits 0. git.exe invocations pin `-c core.autocrlf=false
-c core.longpaths=true` for deterministic comparison.

## Archive extraction: PASS

Fixtures are generated locally (zip and tar.gz through the standard
library; 7z through 7z.exe from the Scoop-installed 7zip app) and
extracted through the plan section 8.3 option-B stack.

```
INFO sevenzip-dep version=v1.6.1
PASS zip-create
PASS zip-extract-verify entries=3 files=3
PASS tar.gz-create
PASS tar.gz-extract-verify entries=3 files=3
PASS 7z-create
PASS 7z-bodgit-extract-verify entries=3 files=3
```

Byte equality holds for all payload files in all three formats. zip
and tar.gz extraction uses `archive/zip`, `archive/tar`, and
`compress/gzip`; 7z extraction uses `bodgit/sevenzip`, proving the
library reads real-world 7z output including the LZMA2 default codec.
If 7z.exe is absent, the 7z case reports SKIP rather than FAIL.

## NTFS junction: PASS

`spikes/junction/` (Windows-only build) drives
`FSCTL_SET_REPARSE_POINT`, `FSCTL_GET_REPARSE_POINT`, and
`FSCTL_DELETE_REPARSE_POINT` through `golang.org/x/sys/windows`
with no shell-out. One layout correction was required during the
spike: the mount-point buffer must carry NUL terminators after both
names, with `PrintNameOffset` accounting for the substitute-name
terminator (verified against `fsutil reparsepoint query` on a
`mklink /J` reference).

```
PASS junction-create
PASS junction-attr-reparse
PASS junction-read
PASS junction-traverse
PASS junction-delete
PASS junction-target-preserved
```

Read-back target matches the absolute target path, content is
accessible through the link, and deletion removes only the link.

## msiexec: document only

`spikes/msiexec/NOTE.md` records the `Expand-MsiArchive` argument
vector (`msiexec.exe /a <msi> /qn TARGETDIR=<dest>\SourceDir`), the
`SourceDir`/`ExtractDir` move semantics, and the phase-3 native-reader
plan. No installer ran; no elevation was requested.

## Verification gate

```
go mod tidy   clean (downloads only; no source changes required after)
go build ./...  clean, no output
go vet ./...    clean, no output
go test ./...   ok gscoop/internal/cli; remaining packages report no test files
```

## Open risks

1. Fixture scale is small. go-git pack memory and wall-clock behavior
   on large buckets (e.g. Extras full history) was not measured; the
   `SHALLOW_BUCKETS` option and re-clone maintenance policy stay open
   until benchmarked.
2. `bodgit/sevenzip` does not support multi-volume `.7z.001`
   archives (7 URLs in the coverage scan) or NSIS-container
   executables. Both stay on the `USE_EXTERNAL_7ZIP` fallback.
3. The Inno Setup native port and the MSI native reader (CFB plus
   cabinet extraction) are unproven; phases 1-2 retain the innounp
   helper and the `msiexec /a` bridge.
4. Private-bucket authentication through Windows credential helpers is
   outside go-git's auth model; the `git.exe` fallback in the
   `gitengine` interface covers that path and was not exercised here.
5. Dependency ceiling: the pinned versions are the newest compatible
   with the `go 1.22` directive. Adopting newer releases requires
   raising the directive past 1.22.
