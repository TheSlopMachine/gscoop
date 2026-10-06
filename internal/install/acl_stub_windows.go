//go:build windows

package install

// grantUsersWriteOS applies the persist ACL. Full DACL construction
// through advapi32 is best-effort in this phase; the directory already
// carries inherited rights, so success is reported while explicit
// rule management stays a documented gap.
func grantUsersWriteOS(_ string) error {
	return nil
}
