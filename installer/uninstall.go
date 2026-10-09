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
	var yes, quiet, keepData, verbose, gui, cli bool
	fl := newFlags("uninstall", e)
	fl.BoolVar(&yes, "yes", false, "in the window, skip the question (the command line never asks)")
	fl.BoolVar(&quiet, "quiet", false, "do not ask, and print only problems")
	fl.BoolVar(&keepData, "keep-data", true, "leave the paths the program keeps on uninstall (always true)")
	fl.BoolVar(&verbose, "verbose", false, "list every file as it is removed")
	fl.BoolVar(&gui, "gui", false, "use the wizard")
	fl.BoolVar(&cli, "cli", false, "use the command line")
	if err := fl.Parse(args); err != nil {
		return exitUsage
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
	md, err := chooseMode(modeInput{
		wantGUI: gui, wantCLI: cli, available: guiAvailable, cliOnly: quiet,
		interactive: e.Interactive, display: hasDisplay(e.Getenv),
	})
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
		return uninstallGUI(r, yes)
	}

	report := reporter(e, verbose)
	if quiet {
		report = reporter(Env{Out: io.Discard, Err: e.Err}, false)
	}
	if err := engine.Uninstall(r, report); err != nil {
		_, _ = fmt.Fprintf(e.Err, "%v\nRun the uninstaller again to retry; its record is %s.\n", err, engine.ReceiptPath(r.Root))
		return exitFail
	}
	if r.RefreshMenu {
		refreshMenu(report)
	}
	if !quiet {
		_, _ = fmt.Fprintf(e.Out, "Removed %s %s.\n", r.App.Name, r.App.Version)
		for _, k := range r.Keep {
			_, _ = fmt.Fprintf(e.Out, "Your data, if any, was left in %s.\n", k)
		}
	}
	return exitOK
}

// handOver is Uninstall for a copy that is not inside an install: it runs
// the uninstaller of its app's install, with the same arguments, and
// returns its exit code.
func handOver(app manifest.App, self string, args []string, e Env, problem func(string) int) int {
	if app.ID == "" {
		return problem("this uninstaller is not inside an install, and does not know which program it belongs to")
	}
	ix, _, err := engine.ReadIndex(&manifest.Manifest{App: app}, "user", e.Getenv)
	if errors.Is(err, fs.ErrNotExist) {
		return problem(fmt.Sprintf("%s is not installed for this user, so there is nothing to remove.", app.Name))
	}
	if err != nil {
		return problem(err.Error())
	}
	if resolved, err := filepath.EvalSymlinks(ix.Uninstaller); err == nil && resolved == self {
		return problem(fmt.Sprintf("the install record of %s is missing from %s; run the installer with --force-receipt-uninstall", app.Name, ix.Root))
	}
	cmd := exec.CommandContext(context.Background(), ix.Uninstaller, args...) // #nosec G204 G702 -- the path this app's own install recorded
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.In, e.Out, e.Err
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
