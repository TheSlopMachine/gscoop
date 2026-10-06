# msiexec /a invocation parity note

Document-only spike. No installer was executed. No elevation was requested.

## Classic behavior (C:\devel\Scoop\lib\decompress.ps1, Expand-MsiArchive)

Default path (`USE_LESSMSI` unset):

```powershell
msiexec.exe /a <Path> /qn TARGETDIR=<DestinationPath>\SourceDir
```

Variants:

- `USE_LESSMSI=true`: `lessmsi.exe x <Path> <DestinationPath>\` instead.
- `ExtractDir` set: `<DestinationPath>` becomes `<DestinationPath>\_tmp`,
  then the requested subdirectory is moved to the real destination and
  `_tmp` is removed.
- After a successful administrative install, content under
  `<DestinationPath>\SourceDir` is moved up to `<DestinationPath>`, and a
  copy of the `.msi` left beside the destination is removed.
- Failures abort with `Failed to extract files from <Path>` plus the log
  file path and a bucket issue link.

## Go parity plan

The Go extractor reproduces the same argument vector through `os/exec`
against the OS component `C:\Windows\system32\msiexec.exe`:

1. Build argv exactly: `/a`, `<msi path>`, `/qn`, `TARGETDIR=<dest>\SourceDir`.
2. Capture the exit code. Non-zero output maps to the same failure message
   shape as classic, including the log path.
3. Apply the same `SourceDir` move-up and `ExtractDir` move semantics.
4. Phase 3 replaces this bridge with a native reader (`richardlehane/mscfb`
   plus a cabinet extractor) for true `lessmsi` parity.

## Why this spike does not execute msiexec

Running `msiexec /a` performs a real administrative installation pass:
it writes to the destination, can trigger Windows Installer service
activity, and fixture `.msi` files are not available offline. Executing it
adds machine state without proving anything about argument parity. The
parity surface is the argument vector and the post-layout moves, both of
which are fully determined by reading `Expand-MsiArchive`.
