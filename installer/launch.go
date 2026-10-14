package installer

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

/*
Starting the program after an install (spec 008). The wizard offers it as
the "Launch now" check on its last page, and the command line as --launch
and, where it asks questions, as one more question. Both start the same
program the same way.
*/

// launchFlag is --launch: not given, yes or no. Not given leaves the
// config's default (ui.launch_default).
type launchFlag struct{ set, on bool }

func (l *launchFlag) String() string { return strconv.FormatBool(l.on) }

func (l *launchFlag) Set(s string) error {
	on, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("%q is not true or false", s)
	}
	l.set, l.on = true, on
	return nil
}

// IsBoolFlag lets --launch stand alone, as the other switches do.
func (l *launchFlag) IsBoolFlag() bool { return true }

// planned reports whether the install starts the program when nobody is
// asked: the flag when it was given, and the config's default otherwise.
func (l launchFlag) planned(m *manifest.Manifest) bool {
	if m.Launch == "" {
		return false
	}
	if l.set {
		return l.on
	}
	return m.LaunchDefault
}

// display is hasDisplay, as a variable, so a test answers for a session it
// is not in.
//
//nolint:gochecknoglobals // replaced by tests
var display = hasDisplay

/*
launchRefusal is why the program is not started here, or "" when it can be.
asked is true when a person asked for the launch, with the flag or with an
answer, and false when it is only the config's default.

A command line that runs as root has nobody else to start the program as,
and a program a person uses does not run as root. A session with no desktop
has nowhere to show a window: a launch that is only the default is passed
over there, and one a person asked for is tried.
*/
func launchRefusal(e Env, asked bool) string {
	switch {
	case elevated() && !windowAsAdmin:
		return "this installer runs as root, and the program is not started as root"
	case !asked && !display(e.Getenv):
		return "this session has no desktop to show it on"
	}
	return ""
}

/*
launchCLI is the command line's end of the install: it starts the program,
or says why not, or does nothing. ask is nil for an install that asks
nothing.
*/
func launchCLI(m *manifest.Manifest, f installFlags, e Env, root string, ask *asker) {
	if m.Launch == "" || (f.launch.set && !f.launch.on) {
		return
	}
	want, asked := f.launch.planned(m), f.launch.set
	if !f.launch.set && ask != nil {
		if launchRefusal(e, true) != "" {
			return
		}
		// The default answer is the config's, and no where there is no
		// desktop: Enter does not start a window nobody sees.
		yes, err := ask.yesOr(fmt.Sprintf("Start %s now?", m.App.Name), m.LaunchDefault && display(e.Getenv))
		if err != nil {
			return
		}
		want, asked = yes, true
	}
	if !want {
		return
	}
	if why := launchRefusal(e, asked); why != "" {
		_, _ = fmt.Fprintf(e.Out, "%s was not started: %s.\n", m.App.Name, why)
		return
	}
	launch(m, root, e.Getenv)
	_, _ = fmt.Fprintf(e.Out, "Started %s.\n", m.App.Name)
}

// launch starts the program of m installed in root, as the person at the
// desktop, also when the installer runs as an administrator (spec 007).
func launch(m *manifest.Manifest, root string, getenv func(string) string) {
	name, args := desktopCommand(filepath.Join(root, filepath.FromSlash(m.Launch)), getenv)
	start(name, false, args...)
}

// start runs a program and does not wait for it: the installer exits and
// the program goes on, also when the terminal the installer was typed into
// closes. ours is true for a program of fynstall's, which shows a window
// and needs no console; the app's own program gets what Windows gives it.
//
// A variable, so a test sees what would be started.
//
//nolint:gochecknoglobals // replaced by tests
var start = func(path string, ours bool, args ...string) {
	cmd := exec.CommandContext(context.Background(), path, args...) // #nosec G204 G702 -- a path this install wrote, or its index recorded
	if ours {
		platform.Background(cmd)
	}
	detach(cmd)
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}
