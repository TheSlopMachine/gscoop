# Migration: classic Scoop to gscoop

## Coexistence

Both implementations share one on-disk state and alternate freely.
After `scoop install gscoop`:

- `scoop` invokes the active implementation (gscoop after the swap).
- `~\scoop\apps\scoop\current\bin\scoop.ps1` always invokes classic
  directly, regardless of the swap state. Use it for A/B verification
  and as the recovery path when the active shim breaks.

`gscoop update` also fast-forwards the classic checkout best-effort, so
switching back never lands on a stale core. `HOLD_UPDATE_UNTIL` from
`scoop hold scoop` applies to both halves.

## Switch-back drill

Rehearse this drill before relying on gscoop day-to-day.

1. Record the baseline under classic:
   `~\scoop\apps\scoop\current\bin\scoop.ps1 list` and `status`.
2. Install gscoop and confirm the swap:
   `scoop install gscoop`, then `gscoop doctor` shows the active
   `scoop` shim pointing at gscoop and the `scoop.classic.*` backup.
3. Run one install and one update under gscoop.
4. Compare: classic `list`, `status`, and `checkup` output must agree
   with gscoop output, and every installed app must still launch.
5. Switch back with the hidden maintenance command: `gscoop unswap`.
   Confirm `scoop list` (classic) matches the gscoop output from step 4.
6. Re-enter: `scoop install gscoop -f` (or restore the swap), then
   confirm `gscoop doctor` is clean.
7. Full removal also restores classic:
   `scoop uninstall gscoop` moves `scoop.classic.*` back into place.

## Notes

- `gscoop doctor` validates the shared contract: junction targets, shim
  trio consistency, `scoop-install.json` readability, bucket repository
  integrity with pack count and size, `PATH` presence of shim dirs,
  Defender exclusion hints, and lock plus orphan `.tmp` sweep reports.
  Full check definitions live in `spec/doctor.md`.
- gscoop cannot detect a concurrent classic writer. The lock file only
  coordinates gscoop processes. Never run both implementations at once.
- Bucket pack stores grow because go-git has no repack. When doctor
  flags a large pack store, run `scoop bucket rm <name>` plus
  `scoop bucket add <name>` to compact it.
