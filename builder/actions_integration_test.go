//go:build linux

package builder_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/engine"
)

// fakeSystemctl stands in for systemctl --user, so the tests never touch
// the person's own systemd manager (spec 002 phase 4a). It keeps each
// unit's state as files beside itself and logs its arguments.
const fakeSystemctl = `#!/bin/sh
PATH=/usr/bin:/bin
state="$(dirname "$0")/systemd"; mkdir -p "$state"
echo "$*" >> "$state/log"
[ "$1" = --user ] && shift
cmd=$1; shift; now=; [ "$1" = --now ] && { now=1; shift; }; unit=$1
case $cmd in
 is-enabled) [ -e "$state/$unit.enabled" ] && { echo enabled; exit 0; }; echo disabled; exit 1;;
 is-active) [ -e "$state/$unit.active" ] && { echo active; exit 0; }; echo inactive; exit 3;;
 enable) touch "$state/$unit.enabled";;
 disable) rm -f "$state/$unit.enabled"; [ -n "$now" ] && rm -f "$state/$unit.active";;
 start|restart) touch "$state/$unit.active";;
esac
exit 0
`

func beaconHome(t *testing.T) home {
	t.Helper()
	h := newHome(t)
	h.root = filepath.Join(h.dir, ".local", "share", "io.ushineko.beacon")
	require.NoError(t, os.WriteFile(filepath.Join(h.path, "systemctl"), []byte(fakeSystemctl), 0o700)) // #nosec G306 -- a test program
	return h
}

func (h home) unit() string {
	return filepath.Join(h.dir, ".config", "systemd", "user", "beacon.service")
}

// state is the fake systemd's view of beacon.service.
func (h home) state(t *testing.T) (enabled, active bool) {
	t.Helper()
	_, err := os.Stat(filepath.Join(h.path, "systemd", "beacon.service.enabled"))
	enabled = err == nil
	_, err = os.Stat(filepath.Join(h.path, "systemd", "beacon.service.active"))
	return enabled, err == nil
}

func (h home) systemctlLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.path, "systemd", "log"))
	require.NoError(t, err)
	return string(b)
}

func TestActionsAreAppliedAndUndone(t *testing.T) {
	h := beaconHome(t)
	old := filepath.Join(h.dir, ".local", "share", "beacon-0", "old.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o750))
	require.NoError(t, os.WriteFile(old, []byte("from beacon 0"), 0o600))
	before := h.snap(t)

	code, out := h.run(t, beacon.Installer, "--yes")
	require.Equal(t, 0, code, out)
	for _, want := range []string{"move ", "service  beacon", "run      " + filepath.Join(h.root, "bin", "beacon") + " setup", "on uninstall, run"} {
		require.Contains(t, out, want, "the install says what it runs before it runs it")
	}
	unit, err := os.ReadFile(h.unit())
	require.NoError(t, err)
	require.Contains(t, string(unit), `ExecStart="`+filepath.Join(h.root, "bin", "beacon")+`" "serve"`)
	require.Equal(t, "--user daemon-reload\n--user enable beacon.service\n--user restart beacon.service\n", h.systemctlLog(t))
	enabled, active := h.state(t)
	require.True(t, enabled && active)
	require.FileExists(t, filepath.Join(h.dir, ".config", "beacon", "setup-done"), "the run action ran")
	b, err := os.ReadFile(filepath.Join(h.root, "data", "old.txt"))
	require.NoError(t, err)
	require.Equal(t, "from beacon 0", string(b), "migrated")

	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--verbose")
	require.Equal(t, 0, code, out)
	hook, firstRemoval := strings.Index(out, "the uninstall hook ran"), strings.Index(out, "removed ")
	require.Positive(t, hook, out)
	require.Less(t, hook, firstRemoval, "the hook runs before anything is removed")
	require.Less(t, strings.Index(out, "removed service beacon"), strings.Index(out, "removed "+filepath.Join(h.root, "bin", "beacon")),
		"the service goes before its program")
	enabled, active = h.state(t)
	require.False(t, enabled || active)

	// The migrated data stays, in its kept directory; everything else is as
	// it was.
	after := h.snap(t)
	require.Equal(t, "dir 750", before[".local/share/beacon-0"])
	delete(before, ".local/share/beacon-0")
	require.Equal(t, before[".local/share/beacon-0/old.txt"], after[".local/share/io.ushineko.beacon/data/old.txt"])
	delete(before, ".local/share/beacon-0/old.txt")
	for _, p := range []string{".local/share/io.ushineko.beacon", ".local/share/io.ushineko.beacon/data", ".local/share/io.ushineko.beacon/data/old.txt"} {
		require.Contains(t, after, p)
		delete(after, p)
	}
	require.Equal(t, before, after)
}

func TestAServiceThatWasThereIsPutBackAsItWas(t *testing.T) {
	h := beaconHome(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(h.unit()), 0o750))
	require.NoError(t, os.WriteFile(h.unit(), []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(h.path, "systemd"), 0o750))
	for _, s := range []string{"enabled", "active"} {
		require.NoError(t, os.WriteFile(filepath.Join(h.path, "systemd", "beacon.service."+s), nil, 0o600))
	}
	before := h.snap(t)

	code, out := h.run(t, beacon.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "replaces a service of that name")
	code, out = h.run(t, filepath.Join(h.root, "uninstall"))
	require.Equal(t, 0, code, out)

	require.Equal(t, before, h.snap(t), "the old unit file is back")
	enabled, active := h.state(t)
	require.True(t, enabled && active, "and enabled and running again")
	require.True(t, strings.HasSuffix(h.systemctlLog(t), "--user enable beacon.service\n--user start beacon.service\n"), h.systemctlLog(t))
}

func TestAFailedRunUndoesTheInstallAndItsActions(t *testing.T) {
	h := beaconHome(t)
	old := filepath.Join(h.dir, ".local", "share", "beacon-0", "old.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(old), 0o750))
	require.NoError(t, os.WriteFile(old, []byte("from beacon 0"), 0o600))
	fail := filepath.Join(h.dir, ".config", "beacon", "fail-setup")
	require.NoError(t, os.MkdirAll(filepath.Dir(fail), 0o750))
	require.NoError(t, os.WriteFile(fail, nil, 0o600))
	before := h.snap(t)

	code, out := h.run(t, beacon.Installer, "--yes")
	require.NotEqual(t, 0, code)
	require.Contains(t, out, "setup failed")
	require.Equal(t, before, h.snap(t), "the service is gone and the data is moved back")
	enabled, active := h.state(t)
	require.False(t, enabled || active)
}

func TestAFailingUninstallHookStopsTheUninstall(t *testing.T) {
	h := beaconHome(t)
	before := h.snap(t)
	code, out := h.run(t, beacon.Installer, "--yes")
	require.Equal(t, 0, code, out)
	fail := filepath.Join(h.root, "fail-goodbye")
	require.NoError(t, os.WriteFile(fail, nil, 0o600))

	code, out = h.run(t, filepath.Join(h.root, "uninstall"))
	require.NotEqual(t, 0, code)
	require.Contains(t, out, "nothing was removed")
	require.FileExists(t, filepath.Join(h.root, "bin", "beacon"))
	require.FileExists(t, engine.ReceiptPath(h.root))
	enabled, active := h.state(t)
	require.True(t, enabled && active, "the service is untouched")

	require.NoError(t, os.Remove(fail))
	code, out = h.run(t, filepath.Join(h.root, "uninstall"))
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

func TestASecondInstallerWaitsForNone(t *testing.T) {
	h := beaconHome(t)
	unlock, err := engine.Lock("io.ushineko.beacon", "user", func(k string) string {
		return map[string]string{"XDG_RUNTIME_DIR": h.rundir}[k]
	})
	require.NoError(t, err)
	code, out := h.run(t, beacon.Installer, "--yes")
	require.NotEqual(t, 0, code)
	require.Contains(t, out, "another installer or uninstaller of this program is running")
	unlock()
	code, out = h.run(t, beacon.Installer, "--yes")
	require.Equal(t, 0, code, out)
}
