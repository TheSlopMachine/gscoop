package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isInDir reports whether check equals dir or sits under it,
// mirroring is_in_dir (lib/core.ps1).
func isInDir(dir, check string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	absCheck, err := filepath.Abs(check)
	if err != nil {
		absCheck = check
	}
	if strings.EqualFold(absCheck, absDir) {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(absDir, sep) {
		absDir += sep
	}
	return strings.HasPrefix(strings.ToLower(absCheck), strings.ToLower(absDir))
}

// FindDirOrSubdir splits path entries into kept plus removed, where
// removed entries equal dir or sit under it. It mirrors
// find_dir_or_subdir (lib/install.ps1:296-307).
func FindDirOrSubdir(path, dir string) (fixed string, removed []string) {
	dir = strings.TrimRight(dir, `\`)
	var kept []string
	for _, part := range strings.Split(path, ";") {
		if part == "" {
			continue
		}
		if strings.EqualFold(part, dir) || strings.HasPrefix(strings.ToLower(part), strings.ToLower(dir+`\`)) {
			removed = append(removed, part)
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, ";"), removed
}

// EnsureInstallDirNotInPath strips the install dir from PATH after
// installers run, mirroring ensure_install_dir_not_in_path
// (lib/install.ps1:279-294). Registry edits go through SetPath.
func EnsureInstallDirNotInPath(dir string, global bool) {
	current := GetPath(global)
	fixed, removed := FindDirOrSubdir(current, dir)
	if len(removed) > 0 {
		for _, r := range removed {
			fmt.Printf("Installer added '%s' to path. Removing.\n", r)
		}
		SetPath(fixed, global)
	}
	if !global {
		sys := GetPath(true)
		_, removed := FindDirOrSubdir(sys, dir)
		for _, r := range removed {
			fmt.Fprintf(os.Stderr, "WARN  Installer added '%s' to system path. You might want to remove this manually (requires admin permission).\n", r)
		}
	}
}
