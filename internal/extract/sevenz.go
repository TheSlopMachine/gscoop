// 7z extraction via bodgit/sevenzip (already pinned, pure Go).
//
// Coverage: LZMA/LZMA2/BCJ/Delta/PPMd/Deflate/BZip2/COPY codecs, which
// the phase-0 spike verified byte-identical against real 7z output
// (spec/spike-results.md). Multi-volume .7z.001 sets are unsupported
// by the library and fail with an external-7z fallback error;
// NSIS-container executables are likewise outside its format support.
package extract

import (
	"io"
	"os"
	"path/filepath"

	"github.com/bodgit/sevenzip"
)

// ExtractSevenZip expands a .7z archive into dest with ExtractDir
// staging identical to the zip path.
func ExtractSevenZip(path, dest, extractDir string) error {
	if IsMultiVolume(filepath.Base(path)) {
		return &NeedExternalError{Name: filepath.Base(path), Reason: "multi-volume .7z.001 sets need 7z.exe"}
	}
	r, err := sevenzip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	target := dest
	var staging string
	if extractDir != "" {
		staging = dest + "_tmp"
		target = staging
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		if err := extractSevenZipEntry(f, target); err != nil {
			return err
		}
	}
	if staging != "" {
		return ApplyExtractDir(staging, dest, extractDir)
	}
	return nil
}

func extractSevenZipEntry(f *sevenzip.File, dest string) error {
	out, err := JoinSecure(dest, f.Name)
	if err != nil {
		return err
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(out, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	return os.WriteFile(out, data, 0o644)
}
