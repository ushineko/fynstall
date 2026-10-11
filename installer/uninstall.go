package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/internal/version"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

/*
Uninstall runs the uninstaller and returns its exit code. It removes the
install whose receipt is beside its own executable.

A copy that is not inside an install, such as the one beside the installer
in dist/, finds the install of its app through the index and hands over to
that install's own uninstaller: only the uninstaller an install came with
removes it (R9e). Started from the desktop, it shows its problems in a
window, never only on stderr, which nobody sees there.
*/
func Uninstall(args []string, app manifest.App, e Env) int {
	var yes, quiet, keepData, verbose, gui, cli, removeLeftovers bool
	fl := newFlags("uninstall", e)
	fl.BoolVar(&yes, "yes", false, "in the window, skip the question (the command line never asks)")
	fl.BoolVar(&quiet, "quiet", false, "do not ask, and print only problems")
	fl.BoolVar(&keepData, "keep-data", true, "leave the paths the program keeps on uninstall (always true)")
	fl.BoolVar(&verbose, "verbose", false, "list every file as it is removed")
	fl.BoolVar(&removeLeftovers, "remove-leftovers", false, "also remove the files the program made in the directories the install created")
	fl.BoolVar(&gui, "gui", false, "use the wizard")
	fl.BoolVar(&cli, "cli", false, "use the command line")
	applyUninstallFlag := fl.Bool("apply-uninstall", false, "used by the uninstaller itself: remove the install as root")
	upgrade := fl.Bool("upgrade", false, "used by a newer installer: remove this version for an upgrade, without asking or listing leftovers")
	if err := fl.Parse(args); err != nil {
		return exitUsage
	}
	why := engine.ReasonUninstall
	if *upgrade {
		// The installer that runs it shows its steps; the closing lines
		// and the leftovers are that installer's to tell.
		why, yes = engine.ReasonUpgrade, true
	}
	if *applyUninstallFlag {
		r, err := ownReceipt()
		if err != nil {
			return newHelperOut(e.Out).fail(err)
		}
		return applyUninstall(r, removeLeftovers, why, e)
	}
	if quiet {
		yes = true
	} else {
		// The first line says which uninstaller is running, so a newer
		// installer's --uninstall shows it ran the installed one (R9e).
		_, _ = fmt.Fprintf(e.Out, "fynstall uninstaller %s\n", version.Version)
	}

	// The CLI never asks: running the uninstaller is the decision (spec 001
	// phase 4). --yes is accepted for scripts and the installer's
	// --uninstall; in the window it skips the question.
	md, err := e.modeFor(modeInput{wantGUI: gui, wantCLI: cli, cliOnly: quiet || *upgrade})
	if err != nil {
		_, _ = fmt.Fprintln(e.Err, err)
		return exitUsage
	}
	problem := func(text string) int {
		_, _ = fmt.Fprintln(e.Err, "uninstall: "+text)
		if md == modeGUI {
			notice("Uninstall "+app.Name, text)
		}
		return exitFail
	}

	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return problem(fmt.Sprintf("find this program: %v", err))
	}
	r, err := engine.ReadReceipt(engine.ReceiptPath(filepath.Dir(exe)))
	if errors.Is(err, fs.ErrNotExist) {
		return handOver(app, exe, args, e, problem)
	}
	if err != nil {
		return problem(err.Error())
	}
	if md == modeGUI {
		return uninstallGUI(r, yes, e.Getenv)
	}

	if needsElevation(r.Scope, e.Getenv) {
		return uninstallElevated(r, removeLeftovers, quiet, verbose, why, e)
	}
	unlock, err := engine.Lock(r.App.ID, r.Scope, e.Getenv)
	if err != nil {
		return problem(err.Error())
	}
	defer unlock()
	report := reporter(e, verbose)
	if quiet {
		report = reporter(Env{Out: io.Discard, Err: e.Err}, false)
	}
	for _, h := range r.Hooks {
		if !quiet {
			_, _ = fmt.Fprintf(e.Out, "Before removing anything, it runs %s\n", commandLine(h.Exec, h.Args))
		}
	}
	left, err := engine.Uninstall(r, why, report)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "%v\nRun the uninstaller again to retry; its record is %s.\n", err, engine.ReceiptPath(r.Root))
		return exitFail
	}
	code := exitOK
	if removeLeftovers && len(left) > 0 {
		if err := engine.RemoveLeftovers(r, left, report); err != nil {
			_, _ = fmt.Fprintf(e.Err, "uninstall: %v\n", err)
			code = exitFail
		}
		left = nil
	}
	if r.RefreshMenu {
		refreshMenu(report)
	}
	if !quiet && why != engine.ReasonUpgrade {
		_, _ = fmt.Fprintf(e.Out, "Removed %s %s.\n", r.App.Name, r.App.Version)
		printLeftovers(e, left, verbose)
		for _, k := range r.Keep {
			_, _ = fmt.Fprintf(e.Out, "Your data, if any, was left in %s.\n", k)
		}
	}
	return code
}

// ownReceipt reads the receipt of the install this program is in.
func ownReceipt() (*engine.Receipt, error) {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return nil, fmt.Errorf("find this program: %w", err)
	}
	return engine.ReadReceipt(engine.ReceiptPath(filepath.Dir(exe)))
}

// uninstallElevated is the command-line uninstall of a system install: the
// removal runs in a helper as root, under sudo. The leftovers are listed,
// or removed with --remove-leftovers, as in a per-user uninstall.
func uninstallElevated(r *engine.Receipt, removeLeftovers, quiet, verbose bool, why engine.Reason, e Env) int {
	report := reporter(e, verbose)
	if quiet {
		report = reporter(Env{Out: io.Discard, Err: e.Err}, false)
	} else if why != engine.ReasonUpgrade {
		_, _ = fmt.Fprintln(e.Out, "This install is for everyone on this computer, so removing it needs an administrator.")
		for _, h := range r.Hooks {
			_, _ = fmt.Fprintf(e.Out, "Before removing anything, it runs %s\n", commandLine(h.Exec, h.Args))
		}
	}
	args := []string{"--apply-uninstall"}
	if removeLeftovers {
		args = append(args, "--remove-leftovers")
	}
	if why == engine.ReasonUpgrade {
		args = append(args, "--upgrade")
	}
	h, err := startElevated(context.Background(), false, e.Getenv, e.Err, args, report)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "uninstall: %v\n", err)
		return exitFail
	}
	left := h.next() // the helper keeps the leftovers when stdin closes
	if err := h.finish(); err != nil {
		_, _ = fmt.Fprintf(e.Err, "uninstall: %v\n", err)
		return exitFail
	}
	if r.RefreshMenu {
		refreshMenu(report)
	}
	if !quiet && why != engine.ReasonUpgrade {
		_, _ = fmt.Fprintf(e.Out, "Removed %s %s.\n", r.App.Name, r.App.Version)
		printLeftovers(e, left, verbose)
		for _, k := range r.Keep {
			_, _ = fmt.Fprintf(e.Out, "Your data, if any, was left in %s.\n", k)
		}
	}
	return exitOK
}

// shownLeftovers is how many leftovers are listed without --verbose.
const shownLeftovers = 20

// printLeftovers lists what the uninstall left because the install did
// not create it (spec 002 D5).
func printLeftovers(e Env, left []engine.Leftover, all bool) {
	if len(left) == 0 {
		return
	}
	_, _ = fmt.Fprintf(e.Out, "Left %s the program made, which the install did not create:\n", leftoverCount(left))
	for i, l := range left {
		if i == shownLeftovers && !all {
			_, _ = fmt.Fprintf(e.Out, "  and %s more (--verbose lists them all)\n", thousands(len(left)-i))
			break
		}
		_, _ = fmt.Fprintf(e.Out, "  %s\n", l)
	}
	_, _ = fmt.Fprintln(e.Out, "Delete them if you no longer need them. --remove-leftovers removes them as part of the uninstall.")
}

// leftoverCount says how many leftovers there are: the files, or, when the
// program made only directories, those.
func leftoverCount(left []engine.Leftover) string {
	if n := engine.LeftoverFiles(left); n > 0 {
		return plural(n, "file", "files")
	}
	return plural(len(left), "empty folder", "empty folders")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return thousands(n) + " " + many
}

// handOver is Uninstall for a copy that is not inside an install: it runs
// the uninstaller of its app's install, with the same arguments, and
// returns its exit code.
func handOver(app manifest.App, self string, args []string, e Env, problem func(string) int) int {
	if app.ID == "" {
		return problem("this uninstaller is not inside an install, and does not know which program it belongs to")
	}
	// A per-user install first: it is the person's own.
	ix, _, err := engine.ReadIndex(&manifest.Manifest{App: app}, "user", e.Getenv)
	if errors.Is(err, fs.ErrNotExist) {
		ix, _, err = engine.ReadIndex(&manifest.Manifest{App: app}, "system", e.Getenv)
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, platform.ErrScopeUnavailable) {
		return problem(fmt.Sprintf("%s is not installed for this user or for everyone, so there is nothing to remove.", app.Name))
	}
	if err != nil {
		return problem(err.Error())
	}
	if resolved, err := filepath.EvalSymlinks(ix.Uninstaller); err == nil && resolved == self {
		return problem(fmt.Sprintf("the install record of %s is missing from %s; run the installer with --force-receipt-uninstall", app.Name, ix.Root))
	}
	cmd := exec.CommandContext(context.Background(), ix.Uninstaller, args...) // #nosec G204 G702 -- the path this app's own install recorded
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.In, e.Out, e.Err
	platform.Background(cmd)
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		return problem(fmt.Sprintf("run %s: %v", ix.Uninstaller, err))
	}
}
