package install

import (
	"os"
	"path/filepath"
)

// makeJunction creates a directory junction or symlink fallback.
func makeJunction(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		return err
	}
	_ = os.Remove(link)
	return os.Symlink(abs, link)
}

func deleteJunctionLink(link string) error {
	return os.Remove(link)
}

// isJunctionLink reports reparse/symlink links for unlink paths.
func isJunctionLink(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return fi.Mode()&os.ModeSymlink != 0 || fi.IsDir(), nil
}

// grantUsersWrite is a no-op outside Windows; the Windows build
// applies a BuiltinUsers write rule.

// IsAdmin reports elevated rights. Portable builds check a marker
// env var for tests; Windows builds query the token.
func IsAdmin() bool {
	return isAdminOS()
}
