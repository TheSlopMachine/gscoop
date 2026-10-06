//go:build !windows

package install

import "os"

func acquireLockOS(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	return func() { f.Close() }, nil
}
