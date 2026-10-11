package builder_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/engine"
)

// blockRemoval puts a non-empty directory where the install's README was,
// so the uninstaller cannot remove that path: a stand-in, on Linux, for a
// file another program holds open on Windows (spec 004).
func blockRemoval(t *testing.T, h home) func() {
	t.Helper()
	readme := filepath.Join(h.root, "share", "doc", "README.md")
	require.NoError(t, os.Remove(readme))
	require.NoError(t, os.MkdirAll(filepath.Join(readme, "held"), 0o750))
	return func() { require.NoError(t, os.RemoveAll(readme)) }
}

// A repair whose old uninstaller cannot remove everything stops, and keeps
// what a second attempt needs: the record, the index entry and the
// uninstaller. Once the obstacle is gone, the same repair finishes.
func TestAFailedRepairCanBeRunAgainAndFinishes(t *testing.T) {
	h := newHome(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	unblock := blockRemoval(t, h)

	code, out = h.run(t, v1Art.Installer, "--yes")
	require.NotEqual(t, 0, code, "the repair stops when the old version is not all removed:\n%s", out)
	require.FileExists(t, engine.ReceiptPath(h.root), "the record")
	require.FileExists(t, h.data("fynstall/installs/io.ushineko.hello.json"), "the index entry, so the installer still finds it")
	require.FileExists(t, filepath.Join(h.root, "uninstall"), "the uninstaller")

	unblock()
	code, out = h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Repairs Hello 0.1.0")
	require.FileExists(t, filepath.Join(h.root, "share", "doc", "README.md"))
	require.FileExists(t, filepath.Join(h.root, "bin", "hello"))
}

// With its index entry gone, an install in the default directory is still
// removed by --force-receipt-uninstall, from its record.
func TestForceReceiptUninstallFindsAnInstallWithNoIndexEntry(t *testing.T) {
	h := newHome(t)
	before := h.snap(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.NoError(t, os.Remove(h.data("fynstall/installs/io.ushineko.hello.json")))

	code, out = h.run(t, v1Art.Installer, "--force-receipt-uninstall")
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}
