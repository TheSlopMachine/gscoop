package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckRunning aborts update and uninstall while app processes run,
// mirroring test_running_process (lib/install.ps1:533-550). Lister
// returns executable paths of running processes; nil selects the OS
// implementation. ignore mirrors IGNORE_RUNNING_PROCESSES.
func CheckRunning(appDir string, ignore bool, lister func(appDir string) []string) error {
	var running []string
	if lister != nil {
		running = lister(appDir)
	} else {
		running = runningProcesses(appDir)
	}
	if len(running) == 0 {
		return nil
	}
	if ignore {
		fmt.Fprintf(os.Stderr, "WARN  The following instances are still running. Scoop is configured to ignore this condition.\n")
		for _, p := range running {
			fmt.Fprintln(os.Stderr, p)
		}
		return nil
	}
	return fmt.Errorf("The following instances are still running. Close them and try again.")
}

// runningProcesses lists process executables under appDir.
func runningProcesses(appDir string) []string {
	return runningProcessesOS(appDir)
}

// Sweep removes orphan .tmp dirs and reports dangling current links,
// mirroring the startup consistency sweep (plan section 6.4). Orphan
// staging dirs delete; current pointing at a missing dir reports.
func Sweep(appsDir string, report func(string)) (removed []string, dangling []string) {
	apps, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, nil
	}
	for _, app := range apps {
		if !app.IsDir() {
			continue
		}
		appPath := filepath.Join(appsDir, app.Name())
		entries, err := os.ReadDir(appPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				full := filepath.Join(appPath, e.Name())
				_ = os.RemoveAll(full)
				removed = append(removed, full)
				if report != nil {
					report("Removing orphan staging dir " + full)
				}
			}
		}
		current := filepath.Join(appPath, "current")
		if fi, err := os.Lstat(current); err == nil {
			target := current
			if fi.Mode()&os.ModeSymlink != 0 {
				if t, err := os.Readlink(current); err == nil {
					target = t
				}
			}
			if _, err := os.Stat(target); os.IsNotExist(err) {
				dangling = append(dangling, current)
				if report != nil {
					report("Dangling current link " + current)
				}
			}
		}
	}
	return removed, dangling
}
