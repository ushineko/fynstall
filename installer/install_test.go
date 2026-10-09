package installer

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/manifest"
)

// The builder embeds a manifest staged for the target it compiles for, so
// a mismatch is a build fault. The installer refuses rather than install a
// payload chosen for another target.
func TestAnInstallerRefusesAPayloadForAnotherTarget(t *testing.T) {
	other := "linux/riscv64"
	if runtime.GOOS+"/"+runtime.GOARCH == other {
		other = "linux/amd64"
	}
	m := &manifest.Manifest{Schema: manifest.Schema, Target: other, Scopes: []string{"user"},
		App: manifest.App{ID: "io.example.hello", Name: "Hello", Version: "0.1.0"}}
	b, err := m.Marshal()
	require.NoError(t, err)
	var out, errOut bytes.Buffer
	code := Install([]string{"--yes"}, Payload{Manifest: b}, Env{Out: &out, Err: &errOut, Getenv: func(string) string { return "" }})
	require.Equal(t, exitFail, code)
	require.Contains(t, errOut.String(), "payload is for "+other)

	// --version still answers: it is how a person finds out what they have.
	out.Reset()
	require.Equal(t, exitOK, Install([]string{"--version"}, Payload{Manifest: b}, Env{Out: &out, Err: &errOut}))
}
