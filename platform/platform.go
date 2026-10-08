/*
Package platform resolves the path placeholders of a manifest for this OS
and an install scope, and says where the install index lives.
*/
package platform

import (
	"fmt"
	"path/filepath"
)

// Vars returns a value for every location placeholder (home, data, config,
// bin) for scope. env reads an environment variable; tests pass their own.
func Vars(scope string, env func(string) string) (map[string]string, error) {
	return vars(scope, env)
}

// IndexDir is where an install's index entry goes: one file per app ID,
// pointing at its install directory and uninstaller, so a later installer
// finds the install wherever the user put it.
func IndexDir(v map[string]string) string {
	return filepath.Join(v["data"], "fynstall", "installs")
}

// absOr returns env's value for key when it is an absolute path, and def
// otherwise: the XDG base directory spec says a relative value is invalid
// and must be ignored.
func absOr(env func(string) string, key, def string) string {
	if v := env(key); v != "" && filepath.IsAbs(v) {
		return v
	}
	return def
}

func unsupportedScope(scope string) error {
	return fmt.Errorf("scope %q is not supported yet (system scope arrives in spec 001 phase 5)", scope)
}
