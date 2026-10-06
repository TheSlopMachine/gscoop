// Inno Setup and WiX Burn: finalized innounp exception with guidance.
//
// Policy: native Inno extraction is deferred. The Inno data format
// spans versions 2-6 and needs a dedicated port (innounp/innoextract
// sources as reference); no such code runs here. Inno installers
// fail closed with an actionable error instead: classic resolves the
// helper as innounp-unicode first, innounp second
// (Get-HelperPath, lib/core.ps1:500-503), and runs innounp -x
// -d<destination> <archive> -y with a -c{app} component selector
// (Expand-InnoArchive, lib/decompress.ps1:244-250). InnoArgs ports
// that vector exactly so the guidance names a runnable command.
// There is no silent external use and no USE_EXTERNAL_INNO opt-in:
// classic has none, and helpers are removed in Go.
//
// WiX Burn bundles stay a documented exception (technical plan
// section 8.3): DarkError names the manual dark/wix invocation.
package extract

import (
	"fmt"
	"strings"
)

// InnoHelperName is the helper classic resolves for innosetup
// installers (Get-InstallationHelper, lib/depends.ps1:115-117).
const InnoHelperName = "innounp"

// InnoHelperUnicode is the package classic tries before
// InnoHelperName (Get-HelperPath, lib/core.ps1:500-503).
const InnoHelperUnicode = "innounp-unicode"

// InnoHelperNames is the classic resolution order for the innounp
// helper: innounp-unicode first, innounp as fallback.
var InnoHelperNames = []string{InnoHelperUnicode, InnoHelperName}

// InnoError reports an Inno Setup executable. Guidance names the
// classic helper resolution order and the exact innounp invocation
// so the user can extract manually.
func InnoError(name string) error {
	return fmt.Errorf("cannot extract %s: Inno Setup executables need innounp (classic Scoop tries innounp-unicode then innounp; install one with classic Scoop and run innounp -x -d<destination> %s -y -c{app}); native Inno extraction is deferred", name, name)
}

// InnoArgs builds the innounp argument vector (Expand-InnoArchive,
// lib/decompress.ps1:244-250): extract, destination, archive,
// assume-yes, then the component selector. ExtractDir scopes inside
// {app} unless it already carries braces.
func InnoArgs(path, dest, extractDir string) []string {
	args := []string{"-x", "-d" + dest, path, "-y"}
	switch {
	case extractDir == "":
		args = append(args, "-c{app}")
	case strings.HasPrefix(extractDir, "{"):
		args = append(args, "-c"+extractDir)
	default:
		args = append(args, `-c{app}\`+extractDir)
	}
	return args
}

// DarkError reports a WiX Burn bundle, which stays a documented
// exception in phases 1-2 (technical plan section 8.3).
func DarkError(name string) error {
	return fmt.Errorf("cannot extract %s: WiX Burn bundles need dark.exe; extract manually with 'wix burn extract %s -out <destination>'", name, name)
}
