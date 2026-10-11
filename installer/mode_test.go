package installer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChooseMode(t *testing.T) {
	full := modeInput{available: true}
	with := func(f func(*modeInput)) modeInput { in := full; f(&in); return in }
	cases := []struct {
		name string
		in   modeInput
		want mode
		err  string
	}{
		{"double-clicked on a desktop", with(func(i *modeInput) { i.display = true }), modeGUI, ""},
		{"typed in a terminal on a desktop", with(func(i *modeInput) { i.display, i.interactive = true, true }), modeCLI, ""},
		{"no terminal and no display", full, modeCLI, ""},
		{"--yes from a desktop script", with(func(i *modeInput) { i.display, i.cliOnly = true, true }), modeCLI, ""},
		{"--cli on a desktop", with(func(i *modeInput) { i.display, i.wantCLI = true, true }), modeCLI, ""},
		{"--gui in a terminal", with(func(i *modeInput) { i.display, i.interactive, i.wantGUI = true, true, true }), modeGUI, ""},
		{"--gui with no display", with(func(i *modeInput) { i.wantGUI = true }), modeCLI, "needs a display"},
		{"--gui in a CLI-only build", modeInput{wantGUI: true, display: true}, modeCLI, "built without the wizard"},
		{"a CLI-only build on a desktop", modeInput{display: true}, modeCLI, ""},
		{"--gui as an administrator", with(func(i *modeInput) { i.display, i.wantGUI, i.root = true, true, true }), modeCLI, "the window does not run as"},
		{"double-clicked as an administrator", with(func(i *modeInput) { i.display, i.root = true, true }), modeCLI, ""},
		{"--gui as an administrator in a CLI-only build", modeInput{wantGUI: true, display: true, root: true}, modeCLI, "built without the wizard"},
		{"--gui and --cli", with(func(i *modeInput) { i.wantGUI, i.wantCLI = true, true }), modeCLI, "give one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseMode(tc.in)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
