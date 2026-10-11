package platform

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Integration is the Windows shell's: a launcher entry is a Start Menu
// shortcut, a link is an entry on PATH, and every install has an Uninstall
// registry entry (spec 001 R18, spec 002 L8 and L10).
const Integration = WindowsShell

func vars(scope string, env func(string) string) (map[string]string, error) {
	switch scope {
	case "user":
		home := env("USERPROFILE")
		if home == "" || !filepath.IsAbs(home) {
			return nil, errors.New("USERPROFILE is not set to an absolute path")
		}
		// {data} is the local, per-machine folder: an installed program does
		// not roam. {config} roams with the profile, as settings do. There
		// is no {bin}: Windows has no directory of links, and a link becomes
		// a PATH entry instead (spec 002 L10).
		data := absOr(env, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		roaming := absOr(env, "APPDATA", filepath.Join(home, "AppData", "Roaming"))
		return map[string]string{
			"home":   home,
			"data":   data,
			"config": roaming,
			// Where Windows puts a program installed for one user (R18).
			"programs": filepath.Join(data, "Programs"),
			keyScope:   scope, keyIndex: filepath.Join(data, "fynstall", "installs"),
			keyStartMenu: filepath.Join(roaming, "Microsoft", "Windows", "Start Menu", "Programs"),
			keyRegRoot:   RegistryRoot(env),
		}, nil
	case "system":
		return nil, fmt.Errorf("an install for everyone on this computer (system scope) on Windows: %w", ErrScopeUnavailable)
	}
	return nil, unsupportedScope(scope)
}

// SystemRoot is "": system installs on Windows come later in spec 001
// phase 7.
func SystemRoot(func(string) string) string { return "" }
