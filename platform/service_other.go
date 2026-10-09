//go:build !linux

package platform

import (
	"fmt"
	"path/filepath"
	"runtime"
)

func errNoServices() error {
	return fmt.Errorf("service actions on %s arrive in spec 001 phase 7", runtime.GOOS)
}

// UnitPath is where a service definition would go; no OS but Linux has
// service actions yet.
func UnitPath(v map[string]string, name string) string {
	return filepath.Join(v["config"], name+".service")
}

// HasServiceManager is false: no OS but Linux has service actions yet.
func HasServiceManager() bool { return false }

// ServiceState reports nothing: no OS but Linux has service actions yet.
func ServiceState(string) (enabled, active bool) { return false, false }

// ReloadServices fails: no OS but Linux has service actions yet.
func ReloadServices() error { return errNoServices() }

// EnableService fails: no OS but Linux has service actions yet.
func EnableService(string) error { return errNoServices() }

// RestartService fails: no OS but Linux has service actions yet.
func RestartService(string) error { return errNoServices() }

// StartService fails: no OS but Linux has service actions yet.
func StartService(string) error { return errNoServices() }

// DisableService fails: no OS but Linux has service actions yet.
func DisableService(string) error { return errNoServices() }
