package platform

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// The record of a system install is in the machine's data folder, where any
// user may make files. An installer that an administrator runs later reads
// the record and runs the uninstaller it names, so a record that somebody
// else wrote must not be believed. ProtectIndex makes the file the
// administrators' and closed to writing by others, and IndexTrusted takes
// only a file that is the administrators' or the system's. A user cannot
// give a file away to them.
//
// Neither applies to a per-user install, whose record is in the user's own
// folder, or under the tests' system root.

// indexACL: owned by the administrators; they and the system have full
// control, users read. The list is protected, so nothing is inherited.
const indexACL = "O:BAD:PAI(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)"

// ProtectIndex makes path, a record of a system install that was just
// written, the administrators' own.
func ProtectIndex(v map[string]string, path string) error {
	if !System(v) || v[keyRoot] != "" {
		return nil
	}
	sd, err := windows.SecurityDescriptorFromString(indexACL)
	if err != nil {
		return fmt.Errorf("protect %s: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("protect %s: %w", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("protect %s: %w", path, err)
	}
	const what = windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, what, owner, nil, dacl, nil); err != nil {
		return fmt.Errorf("protect %s: %w", path, err)
	}
	return nil
}

// IndexTrusted returns an error when path, a record of a system install, was
// not written by an administrator.
func IndexTrusted(v map[string]string, path string) error {
	if !System(v) || v[keyRoot] != "" {
		return nil
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("check who wrote %s: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("check who wrote %s: %w", path, err)
	}
	if owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) || owner.IsWellKnown(windows.WinLocalSystemSid) {
		return nil
	}
	return fmt.Errorf("%s was not written by an administrator, so it is not taken as the record of an install; an administrator can remove it", path)
}
