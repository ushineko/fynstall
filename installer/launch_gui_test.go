// The launch, against the real engine.

//go:build !nogui

package installer

import (
	"bytes"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
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

/*
The wizard is made before the app is: wizard.Run starts one. An installer
whose config starts the program by default stopped here with a nil pointer,
before any window, because ticking the check drew it. Every other test of the
wizard has the test app behind it, which is why none of them said so.
*/
func TestTheWizardIsMadeBeforeThereIsAnApp(t *testing.T) {
	m, p, e, _ := guiFixture(t)
	m.LaunchDefault = true
	// Without the settings page, which is not made before the app either
	// and has an issue of its own (#52).
	m.Parameters = nil
	was := fyne.CurrentApp()
	fyne.SetCurrentApp(nil)
	t.Cleanup(func() { fyne.SetCurrentApp(was) })

	var g *installWizard
	require.NotPanics(t, func() {
		var err error
		g, err = newInstallWizard(m, p, installFlags{scope: "user"}, e)
		require.NoError(t, err)
	})
	require.True(t, g.launch.Checked)
}
