package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserVarsComeFromTheProfile(t *testing.T) {
	env := map[string]string{"USERPROFILE": `C:\Users\ada`, "LOCALAPPDATA": `D:\local`, "APPDATA": `relative`}
	v, err := Vars("user", func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, `C:\Users\ada`, v["home"])
	require.Equal(t, `D:\local`, v["data"])
	require.Equal(t, `C:\Users\ada\AppData\Roaming`, v["config"], "a relative value is ignored")
	require.Equal(t, `D:\local\fynstall\installs`, IndexDir(v))
	require.False(t, System(v))
	_, has := v["bin"]
	require.False(t, has, "Windows has no directory of links")

	_, err = Vars("user", func(string) string { return "" })
	require.ErrorContains(t, err, "USERPROFILE")
}

func TestSystemScopeIsNotThereYet(t *testing.T) {
	_, err := Vars("system", func(string) string { return `C:\x` })
	require.ErrorIs(t, err, ErrScopeUnavailable)
	_, err = Vars("machine", func(string) string { return `C:\x` })
	require.ErrorContains(t, err, `scope "machine" is not supported`)
}
