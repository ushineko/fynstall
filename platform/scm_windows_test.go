package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// The real service manager. Every other test uses the stand-in, because a
// test must not change the computer it runs on. This one registers a service
// under a name of its own, starts it, and removes it, so it runs only when
// asked for (FYNSTALL_TEST_REAL_SERVICES=1) and as an administrator. The
// service is examples/beacon, which answers the service manager.
func TestTheRealServiceManagerRunsAServiceAndRemovesIt(t *testing.T) {
	if os.Getenv("FYNSTALL_TEST_REAL_SERVICES") == "" {
		t.Skip("set FYNSTALL_TEST_REAL_SERVICES=1 to register a service on this computer")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("registering a service needs an administrator")
	}
	exe := filepath.Join(t.TempDir(), "beacon.exe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", exe, "../examples/beacon")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	name := fmt.Sprintf("fynstall-test-%d", os.Getpid())
	key := servicesKey + `\` + name
	t.Cleanup(func() { _ = SCMRemove(key) })
	exists, err := SCMExists(key)
	require.NoError(t, err)
	require.False(t, exists)

	def := ServiceDef{Name: name, DisplayName: name, Description: "A test of fynstall. Safe to remove.", Exec: exe, Args: []string{"serve", "two words"}, Restart: "on-failure"}
	require.NoError(t, SCMCreate(key, def))
	exists, err = SCMExists(key)
	require.NoError(t, err)
	require.True(t, exists)
	require.ErrorIs(t, SCMCreate(key, def), windows.ERROR_SERVICE_EXISTS, "a second service of the name is refused, and the first is left")

	require.NoError(t, SCMStart(key))
	m, err := mgr.Connect()
	require.NoError(t, err)
	s, err := m.OpenService(name)
	require.NoError(t, err)
	st, err := s.Query()
	require.NoError(t, err)
	require.Equal(t, svc.Running, st.State)
	c, err := s.Config()
	require.NoError(t, err)
	require.Equal(t, exe+` serve "two words"`, c.BinaryPathName)
	require.Equal(t, uint32(mgr.StartAutomatic), c.StartType)
	require.Equal(t, "LocalSystem", c.ServiceStartName)
	require.Equal(t, def.Description, c.Description)
	actions, err := s.RecoveryActions()
	require.NoError(t, err)
	require.Len(t, actions, 3)
	require.Equal(t, mgr.ServiceRestart, actions[0].Type)
	nonCrash, err := s.RecoveryActionsOnNonCrashFailures()
	require.NoError(t, err)
	require.True(t, nonCrash, "a stop with an exit code other than 0 is a failure too")
	require.NoError(t, s.Close())
	require.NoError(t, m.Disconnect())

	require.NoError(t, SCMRemove(key))
	require.NoError(t, os.Remove(exe), "its process is gone, so its file can be removed")
	exists, err = SCMExists(key)
	require.NoError(t, err)
	require.False(t, exists)
	require.NoError(t, SCMRemove(key), "removing a service that is gone is not an error")
}

// An account of its own, against the real service manager (spec 010 R2).
// The program is under %ProgramData%, which every account may read: the
// account cannot read a test's temporary directory inside a user's profile.
func TestTheRealServiceManagerRunsAServiceAsItsOwnAccount(t *testing.T) {
	if os.Getenv("FYNSTALL_TEST_REAL_SERVICES") == "" {
		t.Skip("set FYNSTALL_TEST_REAL_SERVICES=1 to register a service on this computer")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("registering a service needs an administrator")
	}
	name := fmt.Sprintf("fynstall-test-own-%d", os.Getpid())
	dir := filepath.Join(os.Getenv("ProgramData"), name)
	require.NoError(t, os.Mkdir(dir, 0o750))
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	exe := filepath.Join(dir, "beacon.exe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", exe, "../examples/beacon")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	key := servicesKey + `\` + name
	t.Cleanup(func() { _ = SCMRemove(key) })
	def := ServiceDef{Name: name, DisplayName: name, Description: "A test of fynstall. Safe to remove.", Exec: exe, Args: []string{"serve"}, Restart: "no", Account: VirtualAccount(name)}
	require.NoError(t, SCMCreate(key, def))
	require.NoError(t, SCMStart(key), "the account may run a program that every account may read")

	m, err := mgr.Connect()
	require.NoError(t, err)
	s, err := m.OpenService(name)
	require.NoError(t, err)
	c, err := s.Config()
	require.NoError(t, err)
	require.Equal(t, `NT SERVICE\`+name, c.ServiceStartName)
	require.Equal(t, uint32(windows.SERVICE_SID_TYPE_UNRESTRICTED), c.SidType, "its SID can be named in an access list")
	require.NoError(t, s.Close())
	require.NoError(t, m.Disconnect())

	require.NoError(t, SCMRemove(key))
	require.NoError(t, os.Remove(exe), "its process is gone, so its file can be removed")
}
