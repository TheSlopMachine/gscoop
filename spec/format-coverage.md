# Format coverage scan

Date: 2026-10-06. Source: manifests installed under
`%USERPROFILE%\scoop\buckets` (buckets: main, extras, versions, games,
java, sysinternals, abyss, charm).

## Method

1. Enumerate `*.json` under each bucket directory, excluding `.git`.
   Standard buckets keep manifests under `<bucket>\bucket`; the `charm`
   bucket keeps them at the bucket root. Both layouts were included.
2. Parse each file as JSON. Zero parse failures.
3. Collect URLs from top-level `url` plus
   `architecture.{64bit,32bit,arm64}.url`, matching the resolution order
   in classic `arch_specific`.
4. Strip `#fragment` and `?query` suffixes, take the URL path leaf,
   lowercase it, and classify by extension. Extensionless CDN endpoints
   (SourceForge `download.php`, `fwlink`, bare version strings) fall into
   `other`.
5. Record the top-level `innosetup` flag per manifest (the only location
   recognized by `schema.json` and `Get-InstallationHelper`).

## Totals

| Measure | Count |
|---|---|
| Manifest files scanned | 6923 |
| URLs collected | 10140 |
| Manifests with no URL (installer/script-only or meta packages) | 42 |
| Manifests with `"innosetup": true` | 180 |

Manifests per bucket: main 1703, extras 2474, versions 638, games 421,
java 421, sysinternals 79, abyss 1171, charm 16.

## URL formats

| Format | URLs | Classic dispatch (`lib/decompress.ps1`) |
|---|---|---|
| zip | 5651 | `Expand-7zipArchive` when 7zip is installed, else `Expand-ZipArchive` (.NET) |
| exe | 2444 | `Expand-InnoArchive` when `innosetup` is set; otherwise direct installer execution |
| 7z | 542 | `Expand-7zipArchive` |
| tar.gz / .tgz | 354 | `Expand-7zipArchive`, two-pass (outer decompress, then inner tar) |
| msi | 331 | `Expand-MsiArchive` (`msiexec /a`, or lessmsi when `USE_LESSMSI`) |
| nupkg | 128 | `Test-7zipRequirement` match; nupkg is a zip container |
| tar.lzma | 65 | `Test-7zipRequirement` match |
| tar.zst | 59 | `Test-7zipRequirement` match |
| jar | 49 | Single-file payload, no extraction |
| msix / msixbundle | 47 | Single-file payload (AppX/MSIX container) |
| tar.xz | 23 | `Test-7zipRequirement` match |
| cab | 11 | `Test-7zipRequirement` does not match; 7z handles it when routed there |
| gz (single) | 9 | `Test-7zipRequirement` match |
| rar | 9 | `Expand-7zipArchive`, incl. split-RAR removal logic |
| 7z split (`.7z.001`) | 7 | `Expand-7zipArchive`, multi-part removal logic |
| xz (single) | 4 | `Test-7zipRequirement` match |
| zst (single) | 2 | `Test-7zipRequirement` match |
| other / extensionless | ~230 | Mixed single-file payloads and CDN endpoints |

No URLs observed with plain `.tar`, `.tar.bz2`, `.iso`, `.img`, `.lzh`,
`.vsix`, or `.deb` extensions in this snapshot.

`other` consists of extensionless download endpoints (`download`,
`download.php`, `dl.php`, `fwlink`, bare version numbers, `stable`,
`windows`, `archive`), plus single-file payloads installed without
extraction: `.dll` (PHP xdebug extensions), `.ps1`/`.cmd`/`.bat`
helpers, `.reg`/`.reg.templ` templates, `.ico`, `.plgx` (KeePass
plugins), `.whl`, `.phar`, `.pyz`, `.ttf`, `.vbox-extpack`, `.war`,
`.pup`, `.cpl`, `.traineddata`, `.dict.yaml`, and CUDA installers
with bare `cuda_*_win10` names.

## innosetup

- 180 manifests set `"innosetup": true`.
- 228 `.exe` URLs carry the flag; 5 flagged manifests reference
  atypical URL leaves (`c:\`, `dl1.php`, a bare setup name, numeric IDs).
  These rely on `innounp` against whatever the URL downloads.
- Non-exe URLs under flagged manifests are supporting files, not
  installer payloads.

## Consequences for the Go extraction stack (plan section 8.3, option B)

- zip (55.7% of URLs) is covered by `archive/zip`.
- tar.gz / tar.xz / tar.zst / tar.lzma plus single gz/xz/zst are covered
  by `archive/tar` with stdlib gzip/bzip2, `ulikunitz/xz`, and
  `klauspost/compress/zstd`.
- 7z (5.3%) is covered by `bodgit/sevenzip` except multi-volume
  `.7z.001` (7 URLs) and any NSIS-container executables, which stay on
  the `USE_EXTERNAL_7ZIP` fallback.
- rar volume is small (9 URLs); `nwaples/rardecode` covers RAR4/RAR5
  reads, with split-RAR assembly ported from classic removal logic.
- msi (331 URLs) uses the `msiexec /a` bridge in phases 1-2 and a
  native reader in phase 3.
- Inno Setup (180 manifests) keeps the innounp helper in phases 1-2
  with a documented native port in phase 3.
- Long-tail single-file payloads require no extraction; the installer
  runner handles them as classic does.
