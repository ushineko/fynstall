package installer

import (
	"bytes"
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/manifest"
)

// seams replaces what a launch reads of the machine and returns what was
// started.
func seams(t *testing.T, desktop, root bool) *[]string {
	t.Helper()
	var started []string
	oldStart, oldDisplay, oldElevated := start, display, elevated
	start = func(path string, _ bool, args ...string) { started = append(append(started, path), args...) }
	display = func(func(string) string) bool { return desktop }
	elevated = func() bool { return root }
	t.Cleanup(func() { start, display, elevated = oldStart, oldDisplay, oldElevated })
	return &started
}

/*
Spec 008. What the command line does with the program to start when an
install is done: --launch starts it, --launch=false does not, and without
the flag the config's default decides for an install that asks nothing and
is the default answer for one that asks.
*/
func TestTheCommandLineStartsTheProgramByTheRule(t *testing.T) {
	root := filepath.Join("x", "hello")
	program := filepath.Join(root, "bin", "hello")
	cases := []struct {
		name            string
		args            []string
		byDefault       bool
		answer          string // "-" is an install that asks nothing
		desktop         bool
		starts          bool
		says, saysNoMor string
	}{
		{name: "nothing asked for, nothing by default", answer: "-", desktop: true},
		{name: "the flag", args: []string{"--launch"}, answer: "-", desktop: true, starts: true, says: "Started Hello."},
		{name: "the flag, with no desktop: a person asked, so it is tried", args: []string{"--launch"}, answer: "-", starts: true},
		{name: "the default", byDefault: true, answer: "-", desktop: true, starts: true, says: "Started Hello."},
		{name: "the default, refused by the flag", args: []string{"--launch=false"}, byDefault: true, answer: "-", desktop: true, saysNoMor: "Hello"},
		{name: "the default, with no desktop", byDefault: true, answer: "-", says: "Hello was not started: this session has no desktop"},
		{name: "asked, and yes", answer: "y\n", desktop: true, starts: true, says: "Start Hello now? [y/N]"},
		{name: "asked, and Enter", answer: "\n", desktop: true, says: "Start Hello now? [y/N]"},
		{name: "asked, where yes is the default, and Enter", byDefault: true, answer: "\n", desktop: true, starts: true, says: "Start Hello now? [Y/n]"},
		{name: "asked, where yes is the default, and no", byDefault: true, answer: "n\n", desktop: true, says: "[Y/n]"},
		{name: "asked with no desktop: the default answer is no", byDefault: true, answer: "\n", says: "[y/N]"},
		{name: "the flag answers the question", args: []string{"--launch"}, answer: "n\n", desktop: true, starts: true, saysNoMor: "now?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			started := seams(t, tc.desktop, false)
			m := &manifest.Manifest{App: manifest.App{Name: "Hello"}, Launch: "bin/hello", LaunchDefault: tc.byDefault}
			var f installFlags
			fl := flag.NewFlagSet("t", flag.ContinueOnError)
			fl.Var(&f.launch, "launch", "")
			require.NoError(t, fl.Parse(tc.args))
			var out bytes.Buffer
			e := Env{Out: &out, Getenv: func(string) string { return "" }}
			var ask *asker
			if tc.answer != "-" {
				e.In = strings.NewReader(tc.answer)
				ask = newAsker(e)
			}
			launchCLI(m, f, e, root, ask)
			if tc.starts {
				require.Equal(t, []string{program}, *started)
			} else {
				require.Empty(t, *started)
			}
			if tc.says != "" {
				require.Contains(t, out.String(), tc.says)
			}
			if tc.saysNoMor != "" {
				require.NotContains(t, out.String(), tc.saysNoMor)
			}
		})
	}
}

// A program with nothing to start has no --launch, and starts nothing.
func TestAnInstallerWithNothingToStartStartsNothing(t *testing.T) {
	started := seams(t, true, false)
	var out bytes.Buffer
	f := installFlags{launch: launchFlag{set: true, on: true}}
	launchCLI(&manifest.Manifest{App: manifest.App{Name: "Hello"}}, f, Env{Out: &out}, "x", nil)
	require.Empty(t, *started)
	require.Empty(t, out.String())
}

// Where no window runs as root, a command line that runs as root starts
// nothing, asked or not: there is nobody else to start the program as.
func TestACommandLineAsRootStartsNothing(t *testing.T) {
	if windowAsAdmin {
		t.Skip("an administrator's installer starts the program through Explorer here (spec 007)")
	}
	started := seams(t, true, true)
	var out bytes.Buffer
	m := &manifest.Manifest{App: manifest.App{Name: "Hello"}, Launch: "bin/hello"}
	launchCLI(m, installFlags{launch: launchFlag{set: true, on: true}}, Env{Out: &out, Getenv: func(string) string { return "" }}, "x", nil)
	require.Empty(t, *started)
	require.Contains(t, out.String(), "Hello was not started: this installer runs as root")
}

func TestTheLaunchFlagTakesTrueOrFalse(t *testing.T) {
	var l launchFlag
	require.True(t, l.IsBoolFlag())
	require.Error(t, l.Set("perhaps"))
	require.False(t, l.set)
	require.NoError(t, l.Set("false"))
	require.True(t, l.set)
	require.False(t, l.on)
	m := &manifest.Manifest{Launch: "bin/hello", LaunchDefault: true}
	require.False(t, l.planned(m), "the flag is over the default")
	require.True(t, launchFlag{}.planned(m))
	require.False(t, launchFlag{set: true, on: true}.planned(&manifest.Manifest{}), "nothing to start")
}
