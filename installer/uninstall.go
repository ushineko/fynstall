package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/internal/version"
)

// Uninstall runs the uninstaller and returns its exit code. It removes the
// install whose receipt is beside its own executable.
func Uninstall(args []string, e Env) int {
	var yes, quiet, keepData, verbose bool
	fl := newFlags("uninstall", e)
	fl.BoolVar(&yes, "yes", false, "do not ask for confirmation")
	fl.BoolVar(&quiet, "quiet", false, "do not ask, and print only problems")
	fl.BoolVar(&keepData, "keep-data", true, "leave the paths the program keeps on uninstall (always true)")
	fl.BoolVar(&verbose, "verbose", false, "list every file as it is removed")
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

	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "uninstall: find this program: %v\n", err)
		return exitFail
	}
	r, err := engine.ReadReceipt(engine.ReceiptPath(filepath.Dir(exe)))
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "uninstall: %v\n", err)
		return exitFail
	}

	if !yes {
		if !e.Interactive {
			_, _ = fmt.Fprintln(e.Err, "There is no terminal to ask on. Run with --yes to remove without asking.")
			return exitUsage
		}
		ok, err := newAsker(e).yes(fmt.Sprintf("Remove %s %s from %s?", r.App.Name, r.App.Version, r.Root))
		if err != nil || !ok {
			_, _ = fmt.Fprintln(e.Out, "Nothing was changed.")
			return exitFail
		}
	}

	report := reporter(e, verbose)
	if quiet {
		report = reporter(Env{Out: io.Discard, Err: e.Err}, false)
	}
	if err := engine.Uninstall(r, report); err != nil {
		_, _ = fmt.Fprintf(e.Err, "%v\nRun the uninstaller again to retry; its record is %s.\n", err, engine.ReceiptPath(r.Root))
		return exitFail
	}
	if !quiet {
		_, _ = fmt.Fprintf(e.Out, "Removed %s %s.\n", r.App.Name, r.App.Version)
		for _, k := range r.Keep {
			_, _ = fmt.Fprintf(e.Out, "Your data, if any, was left in %s.\n", k)
		}
	}
	return exitOK
}
