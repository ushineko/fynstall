//go:build !windows

package platform

// ProtectIndex does nothing here: the record of a system install is in a
// directory only root writes (see the Windows file).
func ProtectIndex(map[string]string, string) error { return nil }

// IndexTrusted takes every record here, for the same reason.
func IndexTrusted(map[string]string, string) error { return nil }
