package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/manifest"
)

var params = &manifest.Manifest{Parameters: []manifest.Parameter{
	{Name: "greeting", Default: "hello"},
	{Name: "name"},
	{Name: "token", Secret: true, Required: true},
}}

func TestAFlagBeatsTheSideFileWhichBeatsAPersonWhoBeatsTheDefault(t *testing.T) {
	asked := map[string]bool{}
	ask := func(p manifest.Parameter) (string, error) {
		asked[p.Name] = true
		return "typed-" + p.Name, nil
	}
	got, err := resolveParams(params,
		map[string]string{"token": "from-flag"},
		map[string]string{"token": "from-file", "name": "from-file"},
		nil, nil, ask)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"greeting": "typed-greeting", "name": "from-file", "token": "from-flag"}, got)
	require.Equal(t, map[string]bool{"greeting": true}, asked, "a person is asked only for what nothing else gave")

	got, err = resolveParams(params, map[string]string{"token": "t"}, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "hello", got["greeting"], "the default, when nobody is asked")
}

// On an upgrade, what the installed version was given comes after a flag
// and the side file and before a person, who is not asked again; a secret
// only the privileged helper can read is left for it.
func TestAnUpgradeKeepsWhatTheInstalledVersionWasGiven(t *testing.T) {
	asked := false
	ask := func(manifest.Parameter) (string, error) { asked = true; return "typed", nil }
	got, err := resolveParams(params, map[string]string{"name": "from-flag"}, nil,
		map[string]string{"greeting": "before", "name": "before", "token": "kept"}, nil, ask)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"greeting": "before", "name": "from-flag", "token": "kept"}, got)
	require.False(t, asked)

	got, err = resolveParams(params, nil, nil, map[string]string{"greeting": "before"}, []string{"token"}, nil)
	require.NoError(t, err, "a required secret the helper reads is not missing")
	require.Empty(t, got["token"])
}

func TestARequiredParameterNamesItsFlag(t *testing.T) {
	_, err := resolveParams(params, nil, nil, nil, nil, nil)
	require.EqualError(t, err, "token is required: pass --token=…, or set token in fynstall-params.yml")
}

func TestTheSideFileRefusesANameThatIsNotAParameter(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, SideFile), []byte("greting: hi\n"), 0o600))
	_, err := readSideFile(dir, params)
	require.ErrorContains(t, err, "greting is not a parameter")

	vals, err := readSideFile(t.TempDir(), params)
	require.NoError(t, err)
	require.Nil(t, vals, "no file is no values")
}
