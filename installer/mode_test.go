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

func TestHasDisplay(t *testing.T) {
	require.True(t, hasDisplay(func(k string) string { return map[string]string{"WAYLAND_DISPLAY": "wayland-0"}[k] }))
	require.True(t, hasDisplay(func(k string) string { return map[string]string{"DISPLAY": ":0"}[k] }))
	require.False(t, hasDisplay(func(string) string { return "" }))
}
