package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserScopeFollowsXDGAndIgnoresRelativeValues(t *testing.T) {
	env := map[string]string{"HOME": "/h", "XDG_DATA_HOME": "/d", "XDG_CONFIG_HOME": "rel"}
	v, err := Vars("user", func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, map[string]string{"home": "/h", "data": "/d", "config": "/h/.config", "bin": "/h/.local/bin"}, v)
	require.Equal(t, "/d/fynstall/installs", IndexDir(v))
}

func TestSystemScopeAndAMissingHomeAreErrors(t *testing.T) {
	_, err := Vars("system", func(string) string { return "/h" })
	require.ErrorContains(t, err, "phase 5")
	_, err = Vars("user", func(string) string { return "" })
	require.ErrorContains(t, err, "HOME")
}
