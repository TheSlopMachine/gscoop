// Package extract expands manifest artifacts for gscoop.
//
// Dispatch mirrors Invoke-Extraction (lib/decompress.ps1:3-61): .zip
// goes through the built-in engine unless USE_EXTERNAL_7ZIP selects 7z,
// .msi uses the msiexec /a bridge, .exe with innosetup set fails with
// guidance, and Test-7zipRequirement names route to the 7z engine.
// Anything else is not an archive: extraction reports false and the
// installer runner handles the file, exactly like classic.
//
// Native coverage follows the technical plan section 8.3 option B:
// zip via archive/zip; tar families via archive/tar over gzip, bzip2,
// xz (ulikunitz/xz), and zstd (klauspost/compress); 7z via
// bodgit/sevenzip except multi-volume .001 (documented gap, external
// fallback); rar via nwaples/rardecode (RAR4/RAR5 reader, split sets
// spanned by the reader); nupkg is a zip container; msi stays on
// the msiexec /a bridge (OS component) with a native reader deferred
// to phase 3; Inno Setup fails with guidance naming innounp resolution
// (no silent external use); WiX Burn (dark) is a documented exception;
// NSIS-in-exe routes through USE_EXTERNAL_7ZIP only.
//
// USE_EXTERNAL_7ZIP=true shells out to 7z.exe, honored verbatim. No
// other external helper runs. No emojis.
package extract

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Options tunes one extraction.
type Options struct {
	// ExtractDir restricts output to a subdirectory of the archive,
	// moved up after extraction (lib/decompress.ps1:122-129).
	ExtractDir string
	// ExtractTo redirects output under the destination.
	ExtractTo string
	// Removal deletes the archive (and split siblings) on success.
	Removal bool
	// InnoSetup marks installers handled by innounp in classic.
	InnoSetup bool
	// UseExternal7ZIP shells out to 7z.exe instead of native engines.
	UseExternal7ZIP bool
}

// Format names the extraction engine for an archive.
type Format int

const (
	// FormatNone is not an archive (plain installer payload).
	FormatNone Format = iota
	// FormatZip is a zip container, nupkg included.
	FormatZip
	// FormatTar is tar and its compression wrappers.
	FormatTar
	// FormatSingle is a lone compressed stream (gz/bz2/xz/zst).
	FormatSingle
	// FormatSevenZip is a 7z archive via bodgit/sevenzip.
	FormatSevenZip
	// FormatRar is a rar archive (dependency pending).
	FormatRar
	// FormatMsi is an MSI package via the msiexec bridge.
	FormatMsi
	// FormatInno is an Inno Setup executable (guidance error).
	FormatInno
	// FormatISO is a cab/img/iso/lzma/lzh payload: classic 7z handled
	// these, so Go needs external 7z (fallback error otherwise).
	FormatISO
	// FormatDark is a WiX Burn bundle (documented exception).
	FormatDark
)

// NeedDependencyError marks formats awaiting a declared Go module.
type NeedDependencyError struct {
	// Format names the archive kind.
	Format string
	// Module is the required module.
	Module string
	// Reason explains the gap.
	Reason string
}

func (e *NeedDependencyError) Error() string {
	return fmt.Sprintf("%s extraction needs module %s: %s", e.Format, e.Module, e.Reason)
}

// NeedExternalError marks payloads only external 7z can open.
type NeedExternalError struct {
	// Name is the archive file name.
	Name string
	// Reason names the limitation.
	Reason string
}

func (e *NeedExternalError) Error() string {
	return fmt.Sprintf("cannot extract %s natively (%s); set USE_EXTERNAL_7ZIP=true to use 7z.exe", e.Name, e.Reason)
}

// Decide routes a file name to an engine, mirroring the Invoke-Extraction
// switch (lib/decompress.ps1:24-48). Matching is case-insensitive like
// -match. innoSetup mirrors $Manifest.innosetup for .exe names.
func Decide(name string, innoSetup, useExternal7ZIP bool) Format {
	lowered := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lowered, ".zip"):
		return FormatZip
	case strings.HasSuffix(lowered, ".msi"):
		return FormatMsi
	case strings.HasSuffix(lowered, ".exe"):
		if innoSetup {
			return FormatInno
		}
		return FormatNone
	}
	if !TestSevenZipRequirement(name) {
		return FormatNone
	}
	switch ClassifySuffix(name) {
	case SuffixRar:
		return FormatRar
	case SuffixTar, SuffixTarWrapper:
		return FormatTar
	case SuffixSingle:
		return FormatSingle
	case SuffixSevenZip:
		if IsMultiVolume(name) {
			return FormatSevenZip
		}
		return FormatSevenZip
	case SuffixNupkg:
		return FormatZip
	default:
		return FormatISO
	}
}

// Extract expands archive path into dest. It returns extracted=false for
// non-archive payloads. dest is the final directory (the caller joins
// extract_to beforehand); ExtractDir scopes inside the archive.
func Extract(path, dest string, opts Options) (bool, error) {
	name := filepath.Base(path)
	format := Decide(name, opts.InnoSetup, opts.UseExternal7ZIP)
	if format == FormatNone {
		return false, nil
	}
	if opts.UseExternal7ZIP && format != FormatMsi && format != FormatInno && format != FormatDark {
		if err := ExtractExternal7Zip(path, dest, opts.ExtractDir); err != nil {
			return false, err
		}
		if opts.Removal {
			return true, RemoveArchive(path)
		}
		return true, nil
	}
	var err error
	switch format {
	case FormatZip:
		err = ExtractZip(path, dest, opts.ExtractDir)
	case FormatTar:
		err = ExtractTar(path, dest, opts.ExtractDir)
	case FormatSingle:
		err = ExtractSingle(path, dest)
	case FormatSevenZip:
		err = ExtractSevenZip(path, dest, opts.ExtractDir)
	case FormatRar:
		err = ExtractRar(path, dest, opts.ExtractDir)
	case FormatMsi:
		err = ExtractMsi(path, dest, opts.ExtractDir)
	case FormatInno:
		err = InnoError(name)
	case FormatISO:
		err = &NeedExternalError{Name: name, Reason: "cab/img/iso/lzma/lzh containers need 7z.exe"}
	case FormatDark:
		err = DarkError(name)
	default:
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if opts.Removal {
		if err := RemoveArchive(path); err != nil {
			return true, err
		}
	}
	return true, nil
}

// File pairs one downloaded file with its manifest extraction fields,
// mirroring the parallel arrays in Invoke-Extraction
// (lib/decompress.ps1:16-20).
type File struct {
	// Name is the url_filename leaf.
	Name string
	// ExtractDir and ExtractTo are the paired manifest fields.
	ExtractDir string
	ExtractTo  string
	// InnoSetup mirrors $Manifest.innosetup.
	InnoSetup bool
}

// ExtractAll extracts files from dir, pairing extract_dir/extract_to by
// extraction index (lib/decompress.ps1:49-58: $extracted counts only
// extracted files). It returns the extracted count.
func ExtractAll(dir string, files []File, opts Options) (int, error) {
	extracted := 0
	for _, f := range files {
		name := f.Name
		if name == "" {
			continue
		}
		fileOpts := opts
		fileOpts.ExtractDir = f.ExtractDir
		fileOpts.InnoSetup = f.InnoSetup
		dest := filepath.Join(dir, f.ExtractTo)
		done, err := Extract(filepath.Join(dir, name), dest, fileOpts)
		if err != nil {
			return extracted, fmt.Errorf("extracting %s: %w", name, err)
		}
		if done {
			extracted++
		}
	}
	return extracted, nil
}

// RemoveArchive deletes an archive after extraction, including split
// siblings: .001 multi-part sets and .partN.rar sets
// (lib/decompress.ps1:133-144). Matching is case-insensitive.
func RemoveArchive(path string) error {
	lowered := strings.ToLower(path)
	if strings.HasSuffix(lowered, ".001") && isSevenZipSplit(path) {
		base := strings.TrimSuffix(path, filepath.Ext(path))
		return removeGlob(base + ".???")
	}
	if base, ok := SplitRarBase(filepath.Base(path)); ok && IsSplitRarFirst(filepath.Base(path)) {
		return removeGlob(filepath.Join(filepath.Dir(path), base+".part*.rar"))
	}
	return os.Remove(path)
}

// isSevenZipSplit reports .001 files belonging to a 7z split set:
// the stem ends in .7z (lib/decompress.ps1:134-136 matches any .001,
// but removal globs stem.??? which only makes sense for 7z splits;
// rar splits match the part pattern above).
func isSevenZipSplit(path string) bool {
	stem := strings.TrimSuffix(strings.ToLower(path), ".001")
	return strings.HasSuffix(stem, ".7z")
}

// removeGlob deletes every file matching pattern. A missing glob is
// not an error; removal failures surface.
func removeGlob(pattern string) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	var errs []error
	for _, m := range matches {
		if info, err := os.Stat(m); err != nil || info.IsDir() {
			continue
		}
		if err := os.Remove(m); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
