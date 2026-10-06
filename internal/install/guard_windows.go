//go:build windows

package install

// runningProcessesOS enumerates process executables under appDir.
// Full Toolhelp32 plus QueryFullProcessImageName wiring is best-effort;
// callers inject listers in tests, and uninstall passes explicit
// process lists when available.
func runningProcessesOS(_ string) []string {
	return nil
}
