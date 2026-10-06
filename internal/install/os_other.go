//go:build !windows

package install

import "os"

// defaultStoreOS falls back to process environment outside Windows so
// pipeline logic stays testable cross-platform.
func defaultStoreOS() EnvStore {
	return &memoryStore{user: map[string]string{"PATH": os.Getenv("PATH")}, system: map[string]string{}}
}

func isAdminOS() bool {
	return os.Getenv("GSCOOP_TEST_ADMIN") == "1"
}
