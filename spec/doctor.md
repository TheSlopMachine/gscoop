# Doctor: extended checkup contract

`gscoop doctor` extends `scoop checkup` (parity with
`libexec/scoop-checkup.ps1` plus `lib/diagnostic.ps1`) with checks over the
on-disk state contract. Implementation lives in `internal/doctor`; the CLI
wiring is `RunDoctor` in `internal/commands/mutate_swap_doctor.go`. Doctor
only reads; it never repairs.

## Check inventory

| Check | Source | Rule |
|---|---|---|
| `junction` | `apps/<app>/current` per scope | Missing link with installed versions is a problem (skipped under `NO_JUNCTION`); unreadable link is a problem; link target outside the app dir or pointing at a missing dir is a problem. |
| `shim` | `<scope>/shims` | Every `.shim` needs a sibling `.exe`; every `.exe` (except `scoop.classic.*` backups) needs a sibling `.shim`; every `.shim` path target (via `shim.GetShimTarget`) must resolve to a file on disk. A `scoop.shim` target containing `gscoop` reports the swap as active; present `scoop.classic.*` files report the classic backup. |
| `metadata` | `apps/<app>/<version>/scoop-install.json` with `install.json` fallback | Unreadable file or invalid JSON is a problem; a version dir without metadata is a failed install marker and is a problem. `current`, `*.tmp`, `*.new`, and `_<v>.old` entries are skipped. |
| `bucket` | `<scoop>/buckets/<name>` | Unreadable bucket dir is a problem; a `.git` repo that `gitengine.Head` cannot open is a problem. Healthy repos report HEAD (12-char), pack file count plus total size, and `remote.origin.url` as information. Pack stores at or above 50 packs or 500 MB report a problem with delete plus re-clone maintenance guidance (go-git has no repack). |
| `path` | user plus global shim dirs against `PATH` | A shim dir that exists but is absent from `PATH` is a problem; comparison is case-insensitive on Windows. |
| `defender` | Windows only | Doctor cannot query Defender state without live Defender APIs, so it prints the remediation command `Add-MpPreference -ExclusionPath '<root>'` per existing scope, marked as requiring elevation when the caller lacks admin rights (`install.IsAdmin`). Informational only. |
| `sweep` | `<scope>/scoop.lock`, `apps/*/*.tmp`, version-dir `*.tmp`/`*.new`, cache `*.tmp` | Present lock files report as information (another gscoop process may hold them). Orphan staged entries from interrupted operations are problems. |

## Output and exit code

Problems print as `WARN  <message>`; informational results print as
`INFO  <message>`. The tail mirrors checkup: `Found N potential problems.`
when any problem exists, else `No problems identified!`. Exit code is
always 0, matching `scoop checkup`.

## Classic parity notes

- `check_windows_defender`, `check_main_bucket`, `check_long_paths`, and
  developer-mode checks stay in `RunCheckup`; doctor adds the state-level
  checks classic lacks (shim trio pairing, metadata readability, pack
  report, orphan sweep).
- The NTFS volume guards from `scoop-checkup.ps1` are not ported: junctions
  and reparse points already require NTFS, and a failed junction surfaces
  through the `junction` check.
