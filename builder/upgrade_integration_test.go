package builder_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/engine"
)

// An upgrade runs the old version's own uninstaller, then installs (R17):
// the file 0.2.0 dropped is gone, kept data survives, and the record is
// the new one.
func TestAnUpgradeRemovesWhatTheOldVersionHadAndKeepsKeptData(t *testing.T) {
	h := newHome(t)
	before := h.snap(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	kept := filepath.Join(h.dir, ".config", "io.ushineko.hello", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o750))
	require.NoError(t, os.WriteFile(kept, []byte(`{"theme":"dark"}`), 0o600))

	code, out = h.run(t, hello020.Installer, "--yes", "--verbose")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Upgrades Hello 0.1.0 to 0.2.0")
	require.Contains(t, out, "==> "+engine.StepReplace)
	require.Contains(t, out, "fynstall uninstaller", "the old version's own uninstaller ran (R9e)")
	require.NoFileExists(t, filepath.Join(h.root, "share", "doc", "README.md"), "0.2.0 has no README")
	require.FileExists(t, filepath.Join(h.root, "bin", "hello"))
	require.FileExists(t, kept)
	r, err := engine.ReadReceipt(engine.ReceiptPath(h.root))
	require.NoError(t, err)
	require.Equal(t, "0.2.0", r.App.Version)

	code, out = h.run(t, filepath.Join(h.root, "uninstall"))
	require.Equal(t, 0, code, out)
	after := h.snap(t)
	for _, p := range []string{".config", ".config/io.ushineko.hello", ".config/io.ushineko.hello/settings.json"} {
		delete(after, p)
	}
	require.Equal(t, before, after, "only the kept data is left")
}

func TestARepairPutsBackADeletedFile(t *testing.T) {
	h := newHome(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	program := filepath.Join(h.root, "bin", "hello")
	want := sum(t, program)
	require.NoError(t, os.Remove(program))
	code, out = h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Repairs Hello 0.1.0")
	require.Equal(t, want, sum(t, program))
}

func TestADowngradeIsDoneOnlyWhenAskedFor(t *testing.T) {
	h := newHome(t)
	code, out := h.run(t, hello020.Installer, "--yes")
	require.Equal(t, 0, code, out)
	code, out = h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "Pass --downgrade")
	code, out = h.run(t, v1Art.Installer, "--yes", "--downgrade")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Replaces Hello 0.2.0 with the older 0.1.0")
	require.FileExists(t, filepath.Join(h.root, "share", "doc", "README.md"))
}

// A secret is never in the receipt; an upgrade reads it back from the
// config file the installed version wrote, and does not ask for it.
func TestASecretSurvivesAnUpgradeWithoutBeingAskedFor(t *testing.T) {
	const fakeToken = "example-not-a-credential"
	h := greetHome(t)
	code, out := h.run(t, greetAMD64.Installer, "--yes", "--token="+fakeToken, "--name=sam")
	require.Equal(t, 0, code, out)
	code, out = h.run(t, greetAMD64.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.NotContains(t, out, fakeToken)
	b, err := os.ReadFile(filepath.Join(h.dir, ".config", "io.ushineko.greet", "config.json"))
	require.NoError(t, err)
	require.Contains(t, string(b), `"token": "`+fakeToken+`"`)
	require.Contains(t, string(b), `"name": "sam"`)
}

// An uninstaller from before --upgrade existed is run with --quiet, and the
// upgrade still works: every installed uninstaller stays usable.
func TestAnUninstallerWithoutUpgradeIsRunQuietly(t *testing.T) {
	h := newHome(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	uninstall := filepath.Join(h.root, "uninstall")
	require.NoError(t, os.Rename(uninstall, uninstall+".real"))
	log := filepath.Join(t.TempDir(), "args")
	old := "#!/bin/sh\ncase \"$1\" in -h) echo 'usage: uninstall [--quiet] [--verbose]'; exit 2;; esac\n" +
		"echo \"$@\" > " + log + "\nexec \"$0.real\" \"$@\"\n"
	require.NoError(t, os.WriteFile(uninstall, []byte(old), 0o700)) // #nosec G306 -- a test program

	code, out = h.run(t, hello020.Installer, "--yes")
	require.Equal(t, 0, code, out)
	b, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "--quiet\n", string(b))
	r, err := engine.ReadReceipt(engine.ReceiptPath(h.root))
	require.NoError(t, err)
	require.Equal(t, "0.2.0", r.App.Version)
}

func TestUninstallHooksAreToldWhyTheyRun(t *testing.T) {
	h := beaconHome(t)
	code, out := h.run(t, beacon.Installer, "--yes")
	require.Equal(t, 0, code, out)
	code, out = h.run(t, beacon.Installer, "--yes", "--verbose")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "the uninstall hook ran (upgrade)")
	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--verbose")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "the uninstall hook ran (uninstall)")
}

// A system upgrade is one helper: it removes the old version and installs
// the new one after one elevation (R13), found without --scope.
func TestASystemUpgradeRunsInOneHelper(t *testing.T) {
	s := newSystem(t, passThrough)
	code, out := s.run(t, v1Art.Installer, "--yes", "--scope", "system")
	require.Equal(t, 0, code, out)
	require.NoError(t, os.Remove(filepath.Join(s.path, "elevate.log")))

	code, out = s.run(t, hello020.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Upgrades Hello 0.1.0 to 0.2.0")
	require.Equal(t, 1, strings.Count(s.elevateLog(t), " --apply-plan "), s.elevateLog(t))
	require.NoFileExists(t, s.at("opt/io.ushineko.hello/share/doc/README.md"))
	r, err := engine.ReadReceipt(engine.ReceiptPath(s.at("opt/io.ushineko.hello")))
	require.NoError(t, err)
	require.Equal(t, "0.2.0", r.App.Version)
	require.Equal(t, "system", r.Scope)
}
