//go:build windows

package install

import (
	"fmt"
	"os"
)

// grantUsersWrite adds a BuiltinUsers write rule on path, mirroring
// persist_permission (lib/install.ps1:521-530). Full DACL editing
// through advapi32 stays behind this helper; failures surface.
func grantUsersWrite(path string) {
	if err := grantUsersWriteOS(path); err != nil {
		fmt.Fprintf(os.Stderr, "WARN  persist permission update failed: %v\n", err)
	}
}
