// External 7z fallback: USE_EXTERNAL_7ZIP honored verbatim.
//
// Classic resolves 7z from PATH only under USE_EXTERNAL_7ZIP
// (lib/decompress.ps1:83-88); Go does the same and shells out to 7z
// for every archive format, which covers NSIS containers,
// multi-volume sets, and exotic filters the native stack cannot read.
// Output streams to memory and surfaces on failure; no log files litter
// the tree.
//
// USE_EXTERNAL_GIT is a separate fallback owned by the bucket sync
// path (gitengine/download); extraction never shells out to git and
// honors only USE_EXTERNAL_7ZIP here.
package extract

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExtractExternal7Zip expands an archive through 7z.exe from PATH.
func ExtractExternal7Zip(path, dest, extractDir string) error {
	sevenZip, err := exec.LookPath("7z")
	if err != nil {
		return fmt.Errorf("Cannot find external 7-Zip (7z.exe) while 'use_external_7zip' is 'true'! Run 'scoop config use_external_7zip false' or install 7-Zip manually and try again.")
	}
	args := External7ZipArgs(path, dest, extractDir)
	cmd := exec.Command(sevenZip, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extract files from %s: %v\n%s%s", path, err, strings.TrimSpace(string(output)), externalFailureHint(path))
	}
	return nil
}

// externalFailureHint names the native-stack gaps explicitly when the
// failed archive is one: multi-volume sets and NSIS containers only
// extract through external 7z, so a failure there needs a working
// full 7-Zip install, not a config change.
func externalFailureHint(path string) string {
	base := filepath.Base(path)
	switch {
	case IsMultiVolume(base):
		return "\nmulti-volume .7z.001 sets need a full 7-Zip install; repair the 7-Zip install and retry"
	case strings.EqualFold(filepath.Ext(base), ".exe"):
		return "\nNSIS installer containers need a full 7-Zip install; repair the 7-Zip install and retry"
	default:
		return ""
	}
}

// External7ZipArgs builds the 7z argument vector: extract with output
// dir, NSIS skipped, assume-yes (lib/decompress.ps1:94). ExtractDir
// scopes to a subdirectory except for tar wrappers, which 7z streams
// through in one pass.
func External7ZipArgs(path, dest, extractDir string) []string {
	clean := strings.TrimRight(dest, `\/`)
	args := []string{"x", path, "-o" + clean, "-xr!*.nsis", "-y"}
	if extractDir != "" && !isTarName(filepath.Base(path)) {
		args = append(args, "-ir!"+extractDir+`\*`)
	}
	return args
}

// isTarName reports names 7z treats as tar wrappers in classic two-pass
// handling (lib/decompress.ps1:95).
func isTarName(name string) bool {
	stripped := StripExt(name)
	if len(stripped) >= 4 && (stripped[len(stripped)-4:] == ".tar" || stripped[len(stripped)-4:] == ".TAR") {
		return true
	}
	return tarWrapperRe.MatchString(name)
}

// StripExt removes the last extension (strip_ext, lib/core.ps1:619).
func StripExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[:i]
	}
	return name
}
