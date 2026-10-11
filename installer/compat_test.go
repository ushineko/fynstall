package installer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/manifest"
)

func TestTheSwitchesOfAnNSISInstallerBecomeTheInstallersOwn(t *testing.T) {
	m := &manifest.Manifest{Parameters: []manifest.Parameter{{Name: "server-url"}, {Name: "token", Secret: true}}}
	for _, c := range []struct {
		name     string
		in, want []string
	}{
		{"silent", []string{"/S"}, []string{"--yes"}},
		{"a parameter, however its name is cased", []string{"/ServerUrl=https://example.invalid/a=b", "/TOKEN=x"}, []string{"--server-url=https://example.invalid/a=b", "--token=x"}},
		{"the directory takes the rest of the line", []string{"/S", `/D=C:\Program Files\My`, "App"}, []string{"--yes", `--dir=C:\Program Files\My App`}},
		{"native flags pass", []string{"--dry-run", "--scope", "system", "/S"}, []string{"--dry-run", "--scope", "system", "--yes"}},
		{"an empty value is a value", []string{"/Token="}, []string{"--token="}},
	} {
		got, err := nsisArgs(c.in, m)
		require.NoError(t, err, c.name)
		require.Equal(t, c.want, got, c.name)
	}
	for _, bad := range []string{"/s", "/NCRC", "/Unknown=1", "/D", "/"} {
		_, err := nsisArgs([]string{bad}, m)
		require.ErrorContains(t, err, bad+" is not a switch of this installer", "nothing is guessed at")
	}
}
