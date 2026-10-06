//go:build !windows

package junction

import "os"

func isReparse(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return fi.Mode()&os.ModeSymlink != 0, nil
}

func createReparse(link, absTarget string) error {
	return os.Symlink(absTarget, link)
}

func readReparse(link string) (string, error) {
	return os.Readlink(link)
}

func deleteReparse(link string) error {
	return os.Remove(link)
}
