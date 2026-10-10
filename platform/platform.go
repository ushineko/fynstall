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
// bin) for scope; system scope has no home. It also holds the scope, the
// index directory and the system root under keys no template can name.
// env reads an environment variable; tests pass their own.
func Vars(scope string, env func(string) string) (map[string]string, error) {
	return vars(scope, env)
}

// IndexDir is where an install's index entry goes: one file per app ID,
// pointing at its install directory and uninstaller, so a later installer
// finds the install wherever the user put it.
func IndexDir(v map[string]string) string { return v[keyIndex] }

// System reports whether v are the values of system scope.
func System(v map[string]string) bool { return v[keyScope] == "system" }

// Rooted puts an absolute install directory under v's system root, which is
// "" except in the tests (see SystemRoot).
func Rooted(v map[string]string, dir string) string {
	if v[keyRoot] == "" || !filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(v[keyRoot], dir)
}

// Keys of Vars that are not placeholders: a template cannot name them,
// because a placeholder is a plain word.
const (
	keyScope = "<scope>"
	keyIndex = "<index>"
	keyRoot  = "<root>"
)

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
	return fmt.Errorf("scope %q is not supported", scope)
}
