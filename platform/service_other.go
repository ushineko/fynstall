//go:build !linux

package platform

import (
	"fmt"
	"path/filepath"
	"runtime"
)

// The functions of this file are systemd's. Windows has services of
// another kind (scm_windows.go), and the engine does not call these there.

func errNoServices() error {
	return fmt.Errorf("%s has no systemd units", runtime.GOOS)
}

// UnitPath is where a unit file would go; no OS but Linux has them.
func UnitPath(v map[string]string, name string) string {
	return filepath.Join(v["config"], name+".service")
}

// HasServiceManager is false: no OS but Linux has systemd.
func HasServiceManager() bool { return false }

// ServiceState reports nothing: no OS but Linux has systemd.
func ServiceState(bool, string) (enabled, active bool) { return false, false }

// ReloadServices fails: no OS but Linux has systemd.
func ReloadServices(bool) error { return errNoServices() }

// EnableService fails: no OS but Linux has systemd.
func EnableService(bool, string) error { return errNoServices() }

// RestartService fails: no OS but Linux has systemd.
func RestartService(bool, string) error { return errNoServices() }

// StartService fails: no OS but Linux has systemd.
func StartService(bool, string) error { return errNoServices() }

// DisableService fails: no OS but Linux has systemd.
func DisableService(bool, string) error { return errNoServices() }
