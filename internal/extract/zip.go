// Zip extraction via archive/zip, mirroring Expand-ZipArchive
// (lib/decompress.ps1:267-301): optional ExtractDir stages through a
// _tmp sibling, then moves up and drops the staging directory.
// Directory entries materialize; links and escapes fail closed.
package extract

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractZip expands a zip (or nupkg) archive into dest.
func ExtractZip(path, dest, extractDir string) error {
	r, err := zip.OpenReader(path)
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
		if err := extractZipEntry(f, target); err != nil {
			return err
		}
	}
	if staging != "" {
		return ApplyExtractDir(staging, dest, extractDir)
	}
	return nil
}

// extractZipEntry writes one zip member, creating empty directories.
func extractZipEntry(f *zip.File, dest string) error {
	out, err := JoinSecure(dest, f.Name)
	if err != nil {
		return err
	}
	info := f.FileInfo()
	if info.IsDir() {
		return os.MkdirAll(out, 0o755)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Links never materialize: classic 7z wrote them as plain
		// files or links, neither of which Go reproduces safely.
		return nil
	}
	if strings.HasSuffix(out, string(filepath.Separator)) {
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
