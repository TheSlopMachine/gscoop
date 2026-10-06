package install

import (
	"fmt"
	"os"
	"path/filepath"
)

// AcquireLock holds $scoopdir/scoop.lock during mutation, mirroring
// plan section 5.3. Classic has no lock; the file is invisible to
// it. Release closes the handle.
func AcquireLock(scoopDir string) (func(), error) {
	return acquireLockOS(filepath.Join(scoopDir, "scoop.lock"))
}

// AcquireGlobalLock holds the global-scope lock file.
func AcquireGlobalLock(globalDir string) (func(), error) {
	return acquireLockOS(filepath.Join(globalDir, "scoop.lock"))
}

// WithLock runs fn while holding the scope lock.
func WithLock(scoopDir string, fn func() error) error {
	release, err := AcquireLock(scoopDir)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

var _ = fmt.Sprint
var _ = os.MkdirAll
