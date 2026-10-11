package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// UninstallName is the uninstaller's name inside an install directory (R9c).
const UninstallName = "uninstall.exe"

/*
removeCreated removes a file the install created. Windows does not delete
the file of a running program, and the uninstaller is one of the files it
removes (R9f). It can still be renamed, so the uninstaller moves its own
file to the temporary directory: the install directory is then empty and is
removed in the same run, not at the next start of the computer. The moved
file is marked for deletion at that start, which Windows allows only an
administrator; otherwise it stays in the temporary directory.
*/
func removeCreated(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, fs.ErrNotExist) || !isSelf(path) {
		return err //nolint:wrapcheck // os.Remove's error names the path
	}
	tmp, terr := os.CreateTemp("", "fynstall-removed-*.exe")
	if terr != nil {
		return fmt.Errorf("remove %s: %w", path, terr)
	}
	aside := tmp.Name()
	_ = tmp.Close()
	// A rename only: across volumes it would be a copy and a delete, and
	// the delete is what Windows refuses.
	if rerr := os.Rename(path, aside); rerr != nil {
		_ = os.Remove(aside)
		return fmt.Errorf("remove %s: %w (moving it aside: %w)", path, err, rerr)
	}
	if p, perr := windows.UTF16PtrFromString(aside); perr == nil {
		_ = windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
	return nil
}

// isSelf reports whether path is the file of the running program.
func isSelf(path string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	a, err := os.Stat(exe)
	if err != nil {
		return false
	}
	b, err := os.Stat(path)
	return err == nil && os.SameFile(a, b)
}
