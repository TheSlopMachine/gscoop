package junction

import (
	"io/fs"
	"os"
	"path/filepath"
)

// RemoveAll deletes a tree containing read-only junctions without
// touching junction targets. Classic clears the link read-only flag
// (attrib -R /L) before Remove-Item (lib/install.ps1:247,269,508);
// os.RemoveAll alone fails on those links, which breaks
// testing.T.TempDir cleanup on Windows.
func RemoveAll(path string) error {
	var junctions []string
	var others []string
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			others = append(others, p)
			return nil
		}
		if p != path && IsJunction(p) {
			junctions = append(junctions, p)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if p != path {
			others = append(others, p)
		}
		return nil
	})
	for _, j := range junctions {
		_ = os.Chmod(j, 0o700)
		_ = Delete(j)
	}
	for i := len(others) - 1; i >= 0; i-- {
		_ = os.Chmod(others[i], 0o700)
		_ = os.Remove(others[i])
	}
	_ = os.Chmod(path, 0o700)
	return os.RemoveAll(path)
}
