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
	// interactive is a terminal on stdin that a person typed the command
	// into; display is true when a window can be shown.
	interactive, display bool
	// root is true when the process runs as root, or elevated on Windows,
	// which the window never does (spec 001 R13).
	root bool
}

/*
chooseMode picks the front end (R11). A terminal means the CLI: a person
who typed the command is reading the terminal. No terminal and a display
means the wizard: the program was started from the desktop. Neither means a
CLI that asks no questions, which fails with a message naming --yes if it
needs an answer.

On Windows a program started from Explorer has a console too, one that
Windows made for it alone. That console is not a person's terminal, so the
caller leaves it out of interactive (R19).
*/
func chooseMode(in modeInput) (mode, error) {
	switch {
	case in.wantGUI && in.wantCLI:
		return modeCLI, errors.New("--gui and --cli ask for different things; give one")
	case in.wantGUI && !in.available:
		return modeCLI, errors.New("this installer was built without the wizard (--cli-only); run it without --gui")
	case in.wantGUI && in.root:
		return modeCLI, errors.New(msgNoWindowAsAdmin)
	case in.wantGUI && !in.display:
		return modeCLI, errors.New(msgNoDisplay)
	case in.wantGUI:
		return modeGUI, nil
	case in.wantCLI, !in.available, in.cliOnly, in.interactive, !in.display, in.root:
		return modeCLI, nil
	}
	return modeGUI, nil
}
