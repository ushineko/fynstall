//go:build !windows

package platform

import "errors"

var errNoSCM = errors.New("the Windows service manager exists only on Windows")

// SCMExists fails: only Windows has its service manager.
func SCMExists(string) (bool, error) { return false, errNoSCM }

// SCMCreate fails: only Windows has its service manager.
func SCMCreate(string, ServiceDef) error { return errNoSCM }

// SCMStart fails: only Windows has its service manager.
func SCMStart(string) error { return errNoSCM }

// SCMRemove fails: only Windows has its service manager.
func SCMRemove(string) error { return errNoSCM }
