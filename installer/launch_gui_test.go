// The launch, against the real engine.

//go:build !nogui

package installer

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

/*
Spec 008. An install from the command line with --launch starts the program
it has just installed, from where it was installed, and says so; the same
install without the flag starts nothing. A dry run says the program would
be started and starts nothing.
*/
func TestAnInstallFromTheCommandLineStartsTheProgram(t *testing.T) {
	m, p, e, _ := guiFixture(t)
	b, err := m.Marshal()
	require.NoError(t, err)
	p.Manifest = b
	started := seams(t, true, false)
	var out, errOut bytes.Buffer
	e.Out, e.Err = &out, &errOut
	program := filepath.Join(at(t, e, "programs", "io.example.hello"), "bin", "hello")

	require.Equal(t, exitOK, Install([]string{"--dry-run", "--launch", "--token=" + fakeToken}, p, e), errOut.String())
	require.Contains(t, out.String(), "after the install, starts "+program)
	require.Empty(t, *started, "a dry run starts nothing")

	out.Reset()
	require.Equal(t, exitOK, Install([]string{"--yes", "--launch", "--token=" + fakeToken}, p, e), errOut.String())
	require.Equal(t, []string{program}, *started)
	require.FileExists(t, program)
	require.Contains(t, out.String(), "Started Hello.")
}

func TestAnInstallFromTheCommandLineWithoutTheFlagStartsNothing(t *testing.T) {
	m, p, e, _ := guiFixture(t)
	b, err := m.Marshal()
	require.NoError(t, err)
	p.Manifest = b
	started := seams(t, true, false)
	var out, errOut bytes.Buffer
	e.Out, e.Err = &out, &errOut
	require.Equal(t, exitOK, Install([]string{"--yes", "--token=" + fakeToken}, p, e), errOut.String())
	require.Empty(t, *started)
	require.NotContains(t, out.String(), "Started")
	require.NotContains(t, out.String(), "after the install, starts")
}

// Spec 008. The finish page's check starts ticked where the config says the
// program is started by default, and clear where it does not.
func TestTheLaunchCheckStartsAsTheConfigSays(t *testing.T) {
	m, p, e, _ := guiFixture(t)
	g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	require.False(t, g.launch.Checked)

	m.LaunchDefault = true
	g, err = newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	require.True(t, g.launch.Checked)
}
