package platform

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserScopeFollowsXDGAndIgnoresRelativeValues(t *testing.T) {
	env := map[string]string{"HOME": "/h", "XDG_DATA_HOME": "/d", "XDG_CONFIG_HOME": "rel"}
	v, err := Vars("user", func(k string) string { return env[k] })
	require.NoError(t, err)
	for k, want := range map[string]string{"home": "/h", "data": "/d", "config": "/h/.config", "bin": "/h/.local/bin"} {
		require.Equal(t, want, v[k], k)
	}
	require.Equal(t, "/d/fynstall/installs", IndexDir(v))
	require.False(t, System(v))
	require.Equal(t, "/h/.config/systemd/user/x.service", UnitPath(v, "x"))
	_, err = Vars("user", func(string) string { return "" })
	require.ErrorContains(t, err, "HOME")
}

// System scope uses /usr/local and /var/lib, since /usr/share belongs to
// the package manager, and has no {home} (spec 001 R15).
func TestSystemScopeIsUnderUsrLocalAndHasNoHome(t *testing.T) {
	v, err := Vars("system", func(string) string { return "" })
	require.NoError(t, err)
	for k, want := range map[string]string{"data": "/usr/local/share", "config": "/etc", "bin": "/usr/local/bin"} {
		require.Equal(t, want, v[k], k)
	}
	require.NotContains(t, v, "home")
	require.True(t, System(v))
	require.Equal(t, "/var/lib/fynstall/installs", IndexDir(v))
	require.Equal(t, "/etc/systemd/system/x.service", UnitPath(v, "x"))
	require.Equal(t, "/opt/io.x.y", Rooted(v, "/opt/io.x.y"))
}

func TestTheTestSystemRootMovesEverySystemPathButNeverForRoot(t *testing.T) {
	env := func(k string) string { return map[string]string{"FYNSTALL_TEST_SYSTEM_ROOT": "/tmp/sys"}[k] }
	v, err := Vars("system", env)
	require.NoError(t, err)
	if os.Geteuid() == 0 {
		require.Equal(t, "/usr/local/share", v["data"], "ignored as root")
		return
	}
	require.Equal(t, "/tmp/sys/usr/local/share", v["data"])
	require.Equal(t, "/tmp/sys/var/lib/fynstall/installs", IndexDir(v))
	require.Equal(t, "/tmp/sys/opt/io.x.y", Rooted(v, "/opt/io.x.y"))
}

func TestOtherScopesAreErrors(t *testing.T) {
	_, err := Vars("machine", func(string) string { return "/h" })
	require.ErrorContains(t, err, "not supported")
}
