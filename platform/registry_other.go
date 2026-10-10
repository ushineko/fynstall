//go:build !windows

package platform

import (
	"errors"
)

var errNotWindows = errors.New("the registry and Start Menu shortcuts exist only on Windows")

// RegistryRoot is "": only Windows has a registry.
func RegistryRoot(func(string) string) string { return "" }

// RegCreateKey fails: only Windows has a registry.
func RegCreateKey(string) ([]string, error) { return nil, errNotWindows }

// RegGet fails: only Windows has a registry.
func RegGet(string, string) (RegValue, bool, error) { return RegValue{}, false, errNotWindows }

// RegSet fails: only Windows has a registry.
func RegSet(string, RegValue) error { return errNotWindows }

// RegDelete fails: only Windows has a registry.
func RegDelete(string, string) error { return errNotWindows }

// RegDeleteKeyIfEmpty fails: only Windows has a registry.
func RegDeleteKeyIfEmpty(string) (bool, error) { return false, errNotWindows }

// WriteShortcut fails: only Windows has shortcuts.
func WriteShortcut(string, Shortcut) error { return errNotWindows }
