package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlyAReleaseOfThisModuleIsTakenFromTheBuildInformation(t *testing.T) {
	for _, c := range []struct {
		path, version, want string
	}{
		{module, "v0.1.1", "0.1.1"},
		{module, "v12.0.34", "12.0.34"},
		{module, "(devel)", ""},
		{module, "", ""},
		{module, "v0.1.1+dirty", ""},
		{module, "v0.1.2-0.20261011120000-ac1bd1f0a1b2", ""},
		{module, "v0.1.2-0.20261011120000-ac1bd1f0a1b2+dirty", ""},
		{module, "v0.2.0-rc.1", ""},
		{module, "0.1.1", ""},
		{"fynstall.local/generated", "v0.1.1", ""},
	} {
		got, ok := release(c.path, c.version)
		require.Equal(t, c.want, got, "%s %s", c.path, c.version)
		require.Equal(t, c.want != "", ok, "%s %s", c.path, c.version)
	}
}

// A test program is not a release, and nothing stamps it.
func TestABuildThatIsNotAReleaseSaysDev(t *testing.T) {
	require.Equal(t, unstamped, Version)
}
