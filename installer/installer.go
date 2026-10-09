/*
Package installer is what a generated installer and uninstaller run. The
builder writes a main package that embeds the payload and calls Main, and a
second one, with no payload, that calls UninstallMain.

Phase 1 has the CLI front end only.
*/
package installer

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ushineko/fynstall/engine"
)

// Payload is what a generated installer embeds.
type Payload struct {
	// Manifest is manifest.json.
	Manifest []byte
	// Files holds the payload, rooted so a manifest path opens directly.
	Files fs.FS
	// Uninstaller is the uninstaller binary built for the same target.
	Uninstaller []byte
}

// Env is the process an installer runs in; tests supply their own.
type Env struct {
	In       io.Reader
	Out, Err io.Writer
	Getenv   func(string) string
	// Interactive is true when In is a terminal a person can answer on.
	Interactive bool
	// ExeDir is the directory the installer is in, where it looks for
	// its parameter file.
	ExeDir string
}

// Main runs the installer with the process's arguments and exits.
func Main(p Payload) {
	os.Exit(Install(os.Args[1:], p, processEnv()))
}

// UninstallMain runs the uninstaller with the process's arguments and exits.
func UninstallMain() {
	os.Exit(Uninstall(os.Args[1:], processEnv()))
}

func processEnv() Env {
	e := Env{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv,
		Interactive: isTerminal(os.Stdin),
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			e.ExeDir = filepath.Dir(exe)
		}
	}
	return e
}

// Exit codes. 2 is a usage error, as with the flag package.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

func newFlags(name string, e Env) *flag.FlagSet {
	fl := flag.NewFlagSet(name, flag.ContinueOnError)
	fl.SetOutput(e.Err)
	return fl
}

// asker reads answers from a person, one line at a time.
type asker struct {
	e  Env
	in *bufio.Reader
}

func newAsker(e Env) *asker { return &asker{e: e, in: bufio.NewReader(e.In)} }

func (a *asker) line(prompt string) (string, error) {
	_, _ = fmt.Fprint(a.e.Out, prompt)
	s, err := a.in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return strings.TrimSpace(s), nil
}

// yes asks a yes-or-no question whose default is no.
func (a *asker) yes(prompt string) (bool, error) {
	s, err := a.line(prompt + " [y/N] ")
	if err != nil {
		return false, err
	}
	s = strings.ToLower(s)
	return s == "y" || s == "yes", nil
}

// reporter prints engine events: every step, and each file when verbose.
func reporter(e Env, verbose bool) engine.Reporter {
	return func(ev engine.Event) {
		switch ev.Kind {
		case engine.Step:
			_, _ = fmt.Fprintf(e.Out, "==> %s\n", ev.Text)
		case engine.Detail:
			if verbose {
				_, _ = fmt.Fprintf(e.Out, "    %s\n", ev.Text)
			}
		case engine.Warn:
			_, _ = fmt.Fprintf(e.Err, "warning: %s\n", ev.Text)
		}
	}
}
