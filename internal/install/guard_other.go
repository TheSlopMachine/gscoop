//go:build !windows

package install

func runningProcessesOS(_ string) []string {
	return nil
}
