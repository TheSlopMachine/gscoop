//go:build windows

package install

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func acquireLockOS(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	handle := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("another scoop process holds the lock; waiting: %w", err)
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
		f.Close()
	}, nil
}
