package installer

import "errors"

// mode is which front end runs.
type mode int

const (
	modeCLI mode = iota
	modeGUI
)

// modeInput is what chooseMode decides from.
type modeInput struct {
	// wantGUI and wantCLI are --gui and --cli.
	wantGUI, wantCLI bool
	// available is false in a --cli-only build.
	available bool
	// cliOnly is a flag that only the CLI has: --yes, --dry-run and the
	// like ask for no person, so they ask for no window either.
	cliOnly bool
	// interactive is a terminal on stdin; display is DISPLAY or
	// WAYLAND_DISPLAY set.
	interactive, display bool
}

/*
chooseMode picks the front end (R11). A terminal means the CLI: a person
who typed the command is reading the terminal. No terminal and a display
means the wizard: the program was started from the desktop. Neither means a
CLI that asks no questions, which fails with a message naming --yes if it
needs an answer.
*/
func chooseMode(in modeInput) (mode, error) {
	switch {
	case in.wantGUI && in.wantCLI:
		return modeCLI, errors.New("--gui and --cli ask for different things; give one")
	case in.wantGUI && !in.available:
		return modeCLI, errors.New("this installer was built without the wizard (--cli-only); run it without --gui")
	case in.wantGUI && !in.display:
		return modeCLI, errors.New("--gui needs a display, and neither DISPLAY nor WAYLAND_DISPLAY is set")
	case in.wantGUI:
		return modeGUI, nil
	case in.wantCLI, !in.available, in.cliOnly, in.interactive, !in.display:
		return modeCLI, nil
	}
	return modeGUI, nil
}

func hasDisplay(getenv func(string) string) bool {
	return getenv != nil && (getenv("WAYLAND_DISPLAY") != "" || getenv("DISPLAY") != "")
}
