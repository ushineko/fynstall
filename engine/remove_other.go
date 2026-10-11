//go:build !windows

package engine

import (
	"os"
	"time"
)

// UninstallName is the uninstaller's name inside an install directory (R9c).
const UninstallName = "uninstall"

// removeCreated removes a file the install created. A running program's
// file is removed like any other here, and nothing holds a file against
// removal, so nothing is waited for; Windows has its own (R9f, spec 004).
func removeCreated(path string, _ *time.Duration) error {
	return os.Remove(path) //nolint:wrapcheck // os.Remove's error names the path
}
