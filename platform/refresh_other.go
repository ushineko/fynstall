//go:build !linux && !windows

package platform

// RefreshMenu does nothing on this OS yet.
func RefreshMenu() error { return nil }
