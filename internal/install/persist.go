package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PersistData links persisted files and dirs, mirroring persist_data
// (lib/install.ps1:444-494): existing target content wins with source
// renamed to .original; existing source moves to the target; neither
// existing creates a directory target. Directories link as junctions,
// files as hard links.
func PersistData(entries []PersistView, dir, persistDir string, emit func(string)) error {
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(persistDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		source, target := e.Source, e.Target
		if target == "" {
			target = source
		}
		if source == "" {
			continue
		}
		if emit != nil {
			emit("Persisting " + source)
		}
		trimmed := strings.TrimRight(strings.TrimRight(source, "/"), `\`)
		src := filepath.Join(dir, trimmed)
		dst := filepath.Join(persistDir, target)
		if _, err := os.Stat(dst); err == nil {
			if _, err := os.Stat(src); err == nil {
				if err := os.Rename(src, src+".original"); err != nil {
					return err
				}
			}
		} else if _, err := os.Stat(src); err == nil {
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Rename(src, dst); err != nil {
				return err
			}
		} else {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		}
		if err := linkPersist(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// UnlinkPersistData removes persist links before directory removal,
// mirroring unlink_persist_data (lib/install.ps1:496-518).
func UnlinkPersistData(entries []PersistView, dir string) {
	for _, e := range entries {
		src := filepath.Join(dir, e.Source)
		fi, err := os.Lstat(src)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
			// Junction links report as dirs; remove the link only.
			_ = removeLink(src)
		} else {
			_ = os.Remove(src)
		}
	}
}

// PersistPermission grants BuiltinUsers write on the global persist
// root when run as admin, mirroring persist_permission
// (lib/install.ps1:521-530).
func PersistPermission(base string, entries []PersistView, global bool) {
	if !global || len(entries) == 0 {
		return
	}
	if !IsAdmin() {
		return
	}
	grantUsersWrite(filepath.Join(base, "persist"))
}

func linkPersist(src, dst string) error {
	info, err := os.Stat(dst)
	if err != nil {
		return err
	}
	_ = os.Remove(src)
	if info.IsDir() {
		if err := makeJunction(src, dst); err != nil {
			return err
		}
		return nil
	}
	return os.Link(dst, src)
}

func removeLink(path string) error {
	// Junction removal must not recurse into the target.
	if ok, _ := isJunctionLink(path); ok {
		return deleteJunctionLink(path)
	}
	return os.Remove(path)
}

// InstallPSModule links the app dir as modules/<name>, mirroring
// install_psmodule (lib/psmodules.ps1:1-25).
func InstallPSModule(base, dir, moduleName string, emit func(string)) error {
	if moduleName == "" {
		return nil
	}
	targetDir := filepath.Join(base, "modules")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	EnsureInPSModulePath(targetDir, false)
	linkFrom := filepath.Join(targetDir, moduleName)
	if emit != nil {
		emit(fmt.Sprintf("Installing PowerShell module '%s'", moduleName))
		emit(fmt.Sprintf("Linking %s => %s", linkFrom, dir))
	}
	if _, err := os.Lstat(linkFrom); err == nil {
		fmt.Fprintf(os.Stderr, "WARN  %s already exists. It will be replaced.\n", linkFrom)
		_ = os.RemoveAll(linkFrom)
	}
	return makeJunction(linkFrom, dir)
}

// UninstallPSModule removes the modules/<name> link, mirroring
// uninstall_psmodule (lib/psmodules.ps1:27-42).
func UninstallPSModule(base, moduleName string, emit func(string)) {
	if moduleName == "" {
		return
	}
	linkFrom := filepath.Join(base, "modules", moduleName)
	if _, err := os.Lstat(linkFrom); err != nil {
		return
	}
	if emit != nil {
		emit(fmt.Sprintf("Uninstalling PowerShell module '%s'.", moduleName))
	}
	_ = os.RemoveAll(linkFrom)
}
