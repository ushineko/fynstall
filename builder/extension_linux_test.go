package builder_test

import (
	"path/filepath"
	"testing"
)

// keyedHome is a temporary HOME for examples/keyed.
func keyedHome(t *testing.T) home { return newHome(t) }

// keyedUninstaller is the uninstaller Keyed's install leaves.
func keyedUninstaller(h home) string {
	return filepath.Join(h.dir, ".local", "share", "io.ushineko.keyed", "uninstall")
}
