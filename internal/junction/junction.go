// Package junction manages NTFS directory junctions through the same
// Win32 primitives classic uses (New-DirectoryJunction in
// lib/install.ps1:554-561, link_current/unlink_current in
// lib/install.ps1:234-276).
//
// On Windows the reparse point is set with FSCTL_SET_REPARSE_POINT,
// read with FSCTL_GET_REPARSE_POINT, and removed with
// FSCTL_DELETE_REPARSE_POINT. On other systems junctions degrade to
// symlinks so pipeline logic stays testable cross-platform. No emojis.
package junction

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsJunction reports whether path carries a reparse point (Windows)
// or is a symlink (other systems).
func IsJunction(path string) bool {
	ok, _ := isReparse(path)
	return ok
}

// Create makes link point at target. link must not exist. The target
// must exist. On Windows the link is a mount-point junction; elsewhere
// it is a symlink.
func Create(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("junction target %q: %w", abs, err)
	}
	if _, err := os.Lstat(link); err == nil {
		return fmt.Errorf("junction link %q already exists", link)
	}
	return createReparse(link, abs)
}

// Target returns the target of link without the \??\ prefix.
func Target(link string) (string, error) {
	return readReparse(link)
}

// Delete removes link without touching the target, mirroring the
// attrib -R plus Remove-Item sequence in unlink_current.
func Delete(link string) error {
	return deleteReparse(link)
}

// Replace flips link to target with classic parity: the link briefly
// does not exist, matching link_current (lib/install.ps1:245-252).
// A version named current is rejected (lib/install.ps1:241-243).
func Replace(link, target string) error {
	if filepath.Base(filepath.Clean(target)) == "current" {
		return fmt.Errorf("version 'current' is not allowed")
	}
	if _, err := os.Lstat(link); err == nil {
		if err := Delete(link); err != nil {
			return err
		}
	}
	return Create(link, target)
}

// LinkCurrent flips appdir/current to versionDir, printing the classic
// Linking line through emit when non-nil.
func LinkCurrent(appDir, versionDir string, emit func(string)) (string, error) {
	current := filepath.Join(appDir, "current")
	if emit != nil {
		emit("Linking " + current + " => " + versionDir)
	}
	if err := Replace(current, versionDir); err != nil {
		return "", err
	}
	return current, nil
}

// UnlinkCurrent removes appdir/current when present and reports the
// reference dir: the version dir itself when no junction existed,
// mirroring unlink_current (lib/install.ps1:261-276).
func UnlinkCurrent(appDir, versionDir string, emit func(string)) string {
	current := filepath.Join(appDir, "current")
	if _, err := os.Lstat(current); err != nil {
		return versionDir
	}
	if emit != nil {
		emit("Unlinking " + current)
	}
	_ = Delete(current)
	return current
}
