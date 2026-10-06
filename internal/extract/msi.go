// MSI extraction through the msiexec /a bridge (OS component).
//
// Classic runs msiexec.exe /a <msi> /qn TARGETDIR=<dest>\SourceDir by
// default, using lessmsi only under USE_LESSMSI (lib/decompress.ps1:186-225).
// Go keeps the bridge: lessmsi was an installed helper, and helpers are
// removed (technical plan section 8.3). A native CFB/cabinet reader is
// deferred to phase 3. Elevation may prompt, exactly like classic.
package extract

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExtractMsi expands an MSI package into dest through msiexec /a.
func ExtractMsi(path, dest, extractDir string) error {
	work := strings.TrimRight(dest, `\/`)
	var ori string
	if extractDir != "" {
		ori = work
		work = work + string(filepath.Separator) + "_tmp"
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	args := MsiAdminArgs(path, work)
	cmd := exec.Command("msiexec.exe", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("msiexec /a failed for %s: %v\n%s", filepath.Base(path), err, strings.TrimSpace(string(output)))
	}
	return ApplyMsiLayout(path, work, ori, extractDir)
}

// MsiAdminArgs builds the msiexec argument vector (Expand-MsiArchive,
// lib/decompress.ps1:196).
func MsiAdminArgs(msiPath, dest string) []string {
	return []string{"/a", msiPath, "/qn", "TARGETDIR=" + dest + `\SourceDir`}
}

// ApplyMsiLayout performs the SourceDir/ExtractDir hand-off after the
// msiexec run (lib/decompress.ps1:206-217). archivePath locates the
// stray archive copy classic removes; ori is empty unless extractDir
// scoped the run into a _tmp sibling.
func ApplyMsiLayout(archivePath, work, ori, extractDir string) error {
	sourceDir := filepath.Join(work, "SourceDir")
	hasSource, _ := isDir(sourceDir)
	switch {
	case extractDir != "" && hasSource:
		if err := MoveDir(filepath.Join(sourceDir, extractDir), ori); err != nil {
			return err
		}
		return os.RemoveAll(work)
	case extractDir != "":
		if err := MoveDir(filepath.Join(work, extractDir), ori); err != nil {
			return err
		}
		return os.RemoveAll(work)
	case hasSource:
		if err := MoveDir(sourceDir, work); err != nil {
			return err
		}
	}
	stray := filepath.Join(work, filepath.Base(archivePath))
	if filepath.Clean(work) != filepath.Clean(filepath.Dir(archivePath)) {
		if _, err := os.Stat(stray); err == nil {
			return os.Remove(stray)
		}
	}
	return nil
}

func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
