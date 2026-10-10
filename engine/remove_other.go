//go:build !windows

package engine

import "os"

// UninstallName is the uninstaller's name inside an install directory (R9c).
const UninstallName = "uninstall"

// removeCreated removes a file the install created. A running program's
// file is removed like any other here; Windows has its own (R9f).
func removeCreated(path string) error {
	return os.Remove(path) //nolint:wrapcheck // os.Remove's error names the path
}
