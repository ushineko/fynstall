package platform

import (
	"errors"
	"fmt"
	"path/filepath"
)

// HasDesktopIntegration is false: the Start Menu shortcut and the PATH entry
// that launcher entries and links become on Windows (spec 002 L10) come with
// the registry journal, later in spec 001 phase 7.
const HasDesktopIntegration = false

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
		return map[string]string{
			"home":   home,
			"data":   data,
			"config": absOr(env, "APPDATA", filepath.Join(home, "AppData", "Roaming")),
			keyScope: scope, keyIndex: filepath.Join(data, "fynstall", "installs"),
		}, nil
	case "system":
		return nil, fmt.Errorf("an install for everyone on this computer (system scope) on Windows: %w", ErrScopeUnavailable)
	}
	return nil, unsupportedScope(scope)
}

// SystemRoot is "": system installs on Windows come later in spec 001
// phase 7.
func SystemRoot(func(string) string) string { return "" }
