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
	"time"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
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
	// OutTerminal is true when Out is a terminal, where a progress line
	// can be redrawn in place.
	OutTerminal bool
	// OwnConsole is true when the terminal is a console Windows made for
	// this process alone: the program was started from the desktop, not
	// typed into a shell (spec 001 R19).
	OwnConsole bool
}

// modeFor chooses the front end for this process, and closes the console
// that Windows made for a program which then shows a window.
func (e Env) modeFor(in modeInput) (mode, error) {
	in.available = guiAvailable
	in.interactive = e.Interactive && !e.OwnConsole
	in.display = hasDisplay(e.Getenv)
	in.root = privileged()
	md, err := chooseMode(in)
	if err == nil && md == modeGUI && e.OwnConsole {
		releaseConsole()
	}
	return md, err
}

// Main runs the installer with the process's arguments and exits.
func Main(p Payload) {
	os.Exit(Install(os.Args[1:], p, processEnv()))
}

// UninstallMain runs the uninstaller with the process's arguments and exits.
// app is the program it belongs to, compiled in by the builder.
func UninstallMain(app manifest.App) {
	os.Exit(Uninstall(os.Args[1:], app, processEnv()))
}

func processEnv() Env {
	e := Env{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv,
		Interactive: isTerminal(os.Stdin), OutTerminal: isTerminal(os.Stdout),
		OwnConsole: ownConsole(),
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

// progressEvery is how often the CLI's progress line is redrawn.
const progressEvery = 100 * time.Millisecond

/*
reporter prints engine events: every step, and each file when verbose. On a
terminal it also keeps one progress line up to date, redrawn at most every
progressEvery, so an install of thousands of files shows how far it is
without printing thousands of lines. Elsewhere (a log, a pipe) it prints no
progress line, which would only be noise there.
*/
func reporter(e Env, verbose bool) engine.Reporter {
	var last time.Time
	var shown bool
	clearLine := func() {
		if shown {
			_, _ = fmt.Fprint(e.Out, "\r\033[K")
			shown = false
		}
	}
	return func(ev engine.Event) {
		switch ev.Kind {
		case engine.Step:
			clearLine()
			_, _ = fmt.Fprintf(e.Out, "==> %s\n", ev.Text)
		case engine.Detail:
			if verbose {
				clearLine()
				_, _ = fmt.Fprintf(e.Out, "    %s\n", ev.Text)
			}
		case engine.Warn:
			clearLine()
			_, _ = fmt.Fprintf(e.Err, "warning: %s\n", ev.Text)
		case engine.Progress:
			if !e.OutTerminal || verbose {
				return
			}
			done := ev.Counts.Files == ev.Counts.FilesTotal && ev.Counts.Bytes == ev.Counts.BytesTotal
			if !done && time.Since(last) < progressEvery {
				return
			}
			last = time.Now()
			_, _ = fmt.Fprintf(e.Out, "\r\033[K    %s", countsText(ev.Counts))
			shown = true
			if done {
				clearLine()
			}
		}
	}
}

// countsText is "2,914 of 5,603 files · 61 MB of 118 MB".
func countsText(c engine.Counts) string {
	return fmt.Sprintf("%s of %s files · %s of %s", thousands(c.Files), thousands(c.FilesTotal),
		humanSize(c.Bytes), humanSize(c.BytesTotal))
}

// humanSize is a byte count in the largest unit that keeps it at or above
// one, with one decimal below ten: "512 B", "3.4 MB", "118 MB". It is here
// rather than fynedesygn's widgets.HumanSize because the CLI-only build
// links no Fyne (R12).
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, units := float64(n), []string{"KB", "MB", "GB", "TB"}
	i := -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if v < 10 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}

// thousands writes n with a comma between each group of three digits.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
