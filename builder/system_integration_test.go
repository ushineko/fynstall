//go:build linux

package builder_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/internal/snapshot"
)

// passThrough stands in for sudo and pkexec: it logs its arguments and
// runs them as the same user, so the helper protocol is tested without
// root (spec 001 phase 5). The system paths are under sys.
const passThrough = `#!/bin/sh
echo "$*" >> "${0%/*}/elevate.log"
exec "$@"
`

// system is a home plus a directory standing for /, and the elevation
// program the installer is told to use.
type system struct {
	home
	sys, elevate string
	// tmp is TMPDIR, where the installer writes the plan file.
	tmp string
}

func newSystem(t *testing.T, elevate string) system {
	t.Helper()
	s := system{home: newHome(t), sys: t.TempDir(), tmp: t.TempDir()}
	s.elevate = filepath.Join(s.path, "fake-elevate")
	require.NoError(t, os.WriteFile(s.elevate, []byte(elevate), 0o700)) // #nosec G306 -- a test program
	return s
}

// run is home.run with the system root and the stand-in elevation set.
func (s system) run(t *testing.T, prog string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), prog, args...)
	cmd.Env = []string{"HOME=" + s.dir, "PATH=" + s.path, "XDG_RUNTIME_DIR=" + s.rundir,
		"FYNSTALL_TEST_SYSTEM_ROOT=" + s.sys, "FYNSTALL_ELEVATE=" + s.elevate, "TMPDIR=" + s.tmp}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String()
	}
	require.NoError(t, err)
	return 0, out.String()
}

func (s system) at(p string) string { return filepath.Join(s.sys, filepath.FromSlash(p)) }

func (s system) snapAll(t *testing.T) [2]map[string]string {
	t.Helper()
	sys, err := snapshot.Take(s.sys)
	require.NoError(t, err)
	return [2]map[string]string{s.snap(t), sys}
}

func (s system) elevateLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.path, "elevate.log"))
	require.NoError(t, err)
	return string(b)
}

func TestASystemInstallRunsInTheHelperAndItsUninstallerElevatesToo(t *testing.T) {
	s := newSystem(t, passThrough)
	before := s.snapAll(t)

	code, out := s.run(t, v1Art.Installer, "--yes", "--scope", "system")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "needs an administrator")
	require.Contains(t, s.elevateLog(t), " --apply-plan ", "the changes ran in the helper")
	for _, p := range []string{"opt/io.ushineko.hello/bin/hello", "usr/local/share/applications/io.ushineko.hello.desktop",
		"var/lib/fynstall/installs/io.ushineko.hello.json"} {
		require.FileExists(t, s.at(p))
	}
	target, err := os.Readlink(s.at("usr/local/bin/hello"))
	require.NoError(t, err)
	require.Equal(t, s.at("opt/io.ushineko.hello/bin/hello"), target)
	require.Equal(t, before[0], s.snap(t), "nothing in the person's home")
	tmp, err := os.ReadDir(s.tmp)
	require.NoError(t, err)
	require.Empty(t, tmp, "the plan file, which holds the parameters, is removed")

	code, out = s.run(t, s.at("opt/io.ushineko.hello/uninstall"))
	require.Equal(t, 0, code, out)
	require.Contains(t, s.elevateLog(t), " --apply-uninstall")
	require.Equal(t, before, s.snapAll(t))
}

// The helper makes the plan again and refuses one whose digest is not the
// one the person approved (R14).
func TestAPlanFileThatWasEditedIsRefused(t *testing.T) {
	s := newSystem(t, passThrough)
	before := s.snapAll(t)
	plan := filepath.Join(t.TempDir(), "plan.json")
	b, err := json.Marshal(map[string]any{"scope": "system", "root": s.at("opt/io.ushineko.hello"), "digest": strings.Repeat("0", 64)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(plan, b, 0o600))
	code, out := s.run(t, v1Art.Installer, "--apply-plan", plan)
	require.Equal(t, 1, code)
	var w struct{ Kind, Text string }
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &w), out)
	require.Equal(t, "error", w.Kind)
	require.Contains(t, w.Text, "not the one that was shown")
	require.Equal(t, before, s.snapAll(t))
}

func TestAHelperThatDiesIsAFailedInstall(t *testing.T) {
	crash := "#!/bin/sh\necho '{\"kind\":\"step\",\"text\":\"Creating directories\",\"step\":0}'\nexit 3\n"
	s := newSystem(t, crash)
	code, out := s.run(t, v1Art.Installer, "--yes", "--scope", "system")
	require.Equal(t, 1, code)
	require.Contains(t, out, "==> Creating directories", "the events before it died are shown")
	require.Contains(t, out, "stopped before it finished")
}

func TestARefusedAdministratorChangesNothing(t *testing.T) {
	s := newSystem(t, "#!/bin/sh\nexit 126\n")
	before := s.snapAll(t)
	code, out := s.run(t, v1Art.Installer, "--yes", "--scope", "system")
	require.Equal(t, 1, code)
	require.Contains(t, out, "an administrator did not allow it")
	require.Equal(t, before, s.snapAll(t))
}

// The uninstall helper reports the leftovers and keeps them when nobody
// says otherwise; --remove-leftovers removes them in the same elevation.
func TestASystemUninstallListsOrRemovesTheLeftovers(t *testing.T) {
	for _, remove := range []bool{false, true} {
		s := newSystem(t, passThrough)
		before := s.snapAll(t)
		code, out := s.run(t, v1Art.Installer, "--yes", "--scope", "system")
		require.Equal(t, 0, code, out)
		made := s.at("opt/io.ushineko.hello/bin/state.db")
		require.NoError(t, os.WriteFile(made, []byte("the program's"), 0o600))
		args := []string{}
		if remove {
			args = append(args, "--remove-leftovers")
		}
		code, out = s.run(t, s.at("opt/io.ushineko.hello/uninstall"), args...)
		require.Equal(t, 0, code, out)
		if remove {
			require.Equal(t, before, s.snapAll(t))
			continue
		}
		require.Contains(t, out, "Left 1 file the program made")
		require.FileExists(t, made)
	}
}

func TestASystemServiceIsASystemUnit(t *testing.T) {
	s := newSystem(t, passThrough)
	require.NoError(t, os.WriteFile(filepath.Join(s.path, "systemctl"), []byte(fakeSystemctl), 0o700)) // #nosec G306 -- a test program
	before := s.snapAll(t)
	code, out := s.run(t, beacon.Installer, "--yes", "--scope", "system")
	require.Equal(t, 0, code, out)
	unit, err := os.ReadFile(s.at("etc/systemd/system/beacon.service"))
	require.NoError(t, err)
	require.Contains(t, string(unit), "WantedBy=multi-user.target")
	require.FileExists(t, s.at("etc/beacon/setup-done"), "{config} is /etc")
	log := s.systemctlLog(t)
	require.Contains(t, log, "daemon-reload\nenable beacon.service\nrestart beacon.service\n")
	require.NotContains(t, log, "--user", "the system manager")

	code, out = s.run(t, s.at("opt/io.ushineko.beacon/uninstall"), "--verbose")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "the uninstall hook ran", "the helper's events reach the person's terminal")
	require.Equal(t, before, s.snapAll(t))
}
