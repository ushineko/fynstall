package builder_test

import "testing"

// keyedHome is a temporary profile for examples/keyed.
func keyedHome(t *testing.T) home { return newHome(t, "io.ushineko.keyed") }

// keyedUninstaller is the uninstaller Keyed's install leaves.
func keyedUninstaller(h home) string { return h.uninstaller() }
