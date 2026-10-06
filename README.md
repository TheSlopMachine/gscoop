# gscoop

Native Go replacement for the Scoop command-line installer. gscoop reads
and writes the classic Scoop on-disk state bit-for-bit, so both
implementations alternate on the same machine. See
`Scoop-Go-Rewrite-Technical-Plan.md` for the full plan and `MIGRATION.md`
for the switch-back drill.

## Status

v1.0 is in progress through Phase 4 (hardening and distribution).
`go build ./...` and `go test ./...` are green. Commands not yet wired
report as unimplemented instead of failing silently.

## Install

Install the `gscoop` package from a bucket carrying `bucket/gscoop.json`:

```
scoop bucket add <bucket carrying gscoop>
scoop install gscoop
```

`post_install` backs the classic trio up as `scoop.classic.{exe,shim,cmd,ps1}`
in the shims directory, then points the active `scoop` shim at the Go
binary. `scoop uninstall gscoop` restores the classic trio.
`gscoop unswap` (hidden maintenance command) restores it without
uninstalling.

## Threat model

Bucket manifests are trusted code. Manifest hooks (`pre_install`,
`post_install`, `pre_uninstall`, `post_uninstall`, `installer.script`,
`uninstaller.script`, `.ps1` installer files) run with the caller
privileges, elevated for `-g` global installs. Install only from buckets
you trust; review manifests before installing unknown packages.

## Deliberate deviations from classic

| Change | Rationale | Reversal |
|---|---|---|
| Parallel downloads (`MAX_DOWNLOADS`, default 4) | Wall-time improvement; the cache contract is unchanged | `MAX_DOWNLOADS=1` restores serial behavior |
| Hash failure isolates one package instead of aborting the transaction | Parallel model makes global abort wasteful | Strict mode flag restores abort-all |
| RE2 regex in `scoop search` instead of .NET regex | Go standard library | Unsupported syntax falls back to literal match with a warning |
| aria2 path removed | Native segmented downloader replaces it | Config accepted with a notice |
| SQLite cache maintained by pure-Go code, identical schema | No .NET bootstrap | None needed |
| `scoopcs` shim variant maps to the maintained default | Upstream removed the CS shim codebase | None needed |
| Download resume via HTTP `Range` | Robustness | Cache contract unchanged |
| New keys: `MAX_DOWNLOADS`, `SPLIT_DOWNLOADS`, `SHALLOW_BUCKETS`, `USE_EXTERNAL_GIT`, `PSHOST`, `GSCOOP_CHANNEL` | New capabilities | Classic ignores unknown keys |

## Performance targets

- `search`: 50 ms or less on a warm cache.
- Bucket sync during `update`: 5x classic or better.
- Multi-package install wall time: 2x classic or better on transactions
  with parallelizable downloads.

## Antivirus and false positives

Releases ship signed binaries with published hashes. The bundled shim
payloads are the upstream `supporting/shims` binaries, already
whitelisted by reputation; gscoop installs byte-identical copies. If a
scanner flags the binary, verify the release hash, then report the
detection with the scanner name and version.

## Upstream drift

A nightly CI job runs the differential harness against
`ScoopInstaller/Scoop@master`. Contract changes open issues
automatically, so silent divergence from classic stays bounded in time.
