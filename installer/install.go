package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// guiAvailable is false in this build: the GUI front end arrives in spec
// 001 phase 4, and a --cli-only installer never has one (R12).
const guiAvailable = false

type installFlags struct {
	cli, gui, yes, dryRun, uninstall, forceReceipt, verbose, version bool
	dir, scope                                                       string
	params                                                           map[string]string
}

// Install runs the installer and returns its exit code.
func Install(args []string, p Payload, e Env) int {
	m, err := manifest.Parse(p.Manifest)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	}
	var f installFlags
	fl := newFlags(m.App.Basename()+"-installer", e)
	fl.BoolVar(&f.cli, "cli", false, "use the command-line front end")
	fl.BoolVar(&f.gui, "gui", false, "use the wizard")
	fl.BoolVar(&f.yes, "yes", false, "do not ask; accept the defaults")
	fl.BoolVar(&f.dryRun, "dry-run", false, "print what would change and change nothing")
	fl.BoolVar(&f.uninstall, "uninstall", false, "remove the installed copy, through its own uninstaller")
	fl.BoolVar(&f.forceReceipt, "force-receipt-uninstall", false, "remove the installed copy with this installer's engine, when its own uninstaller is missing or broken")
	fl.BoolVar(&f.verbose, "verbose", false, "list every file and directory as it is written")
	fl.BoolVar(&f.version, "version", false, "print the version and exit")
	fl.StringVar(&f.dir, "dir", "", "install directory (default from the installer)")
	fl.StringVar(&f.scope, "scope", "", "install scope")
	paramValues := paramFlags(fl, m)
	if err := fl.Parse(args); err != nil {
		return exitUsage
	}
	f.params = paramValues()
	if f.version {
		_, _ = fmt.Fprintf(e.Out, "%s %s installer (fynstall %s)\n", m.App.Name, m.App.Version, m.RuntimeVersion)
		return exitOK
	}
	if built := runtime.GOOS + "/" + runtime.GOARCH; m.Target != "" && m.Target != built {
		// The builder compiles each installer for the target whose payload
		// it embeds (spec 002 D1a), so these always agree unless the build
		// went wrong. This cannot tell which machine it runs on: under
		// emulation GOARCH is the binary's, not the host's.
		_, _ = fmt.Fprintf(e.Err, "This installer's payload is for %s, but the installer was built for %s. Rebuild it.\n", m.Target, built)
		return exitFail
	}
	if f.gui && !guiAvailable {
		_, _ = fmt.Fprintln(e.Err, "This installer was built without the wizard (--cli-only). Run it without --gui.")
		return exitUsage
	}
	if f.scope == "" {
		f.scope = m.Scopes[0]
	}

	ix, ixPath, err := engine.ReadIndex(m, f.scope, e.Getenv)
	switch {
	case err == nil && f.forceReceipt:
		return forceUninstall(ix, e, f.verbose)
	case err == nil && f.uninstall:
		return delegate(ix, f.yes, e)
	case err == nil:
		_, _ = fmt.Fprintf(e.Err, "%s %s is already installed in %s.\nRemove it first with --uninstall, which runs %s.\n", m.App.Name, ix.Version, ix.Root, ix.Uninstaller)
		return exitFail
	case !errors.Is(err, fs.ErrNotExist):
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	case f.uninstall || f.forceReceipt:
		_, _ = fmt.Fprintf(e.Err, "%s is not installed for scope %s (no %s).\n", m.App.Name, f.scope, ixPath)
		return exitFail
	}
	return install(m, p, f, e)
}

func install(m *manifest.Manifest, p Payload, f installFlags, e Env) int {
	if !f.yes && !f.dryRun && !e.Interactive {
		_, _ = fmt.Fprintln(e.Err, "There is no terminal to ask questions on. Run with --yes to install with the defaults.")
		return exitUsage
	}
	ask := newAsker(e)
	side, err := readSideFile(e.ExeDir, m)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitUsage
	}
	var prompt func(manifest.Parameter) (string, error)
	if !f.yes && !f.dryRun {
		prompt = ask.parameter
	}
	params, err := resolveParams(m, f.params, side, prompt)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitUsage
	}
	o := engine.Options{Scope: f.scope, Root: f.dir, Env: e.Getenv, Uninstaller: p.Uninstaller, Params: params}
	plan, err := engine.NewPlan(m, o)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	}
	if !f.yes && !f.dryRun && f.dir == "" {
		dir, err := ask.line(fmt.Sprintf("Install %s %s in [%s]: ", m.App.Name, m.App.Version, plan.Root))
		if err != nil {
			_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
			return exitFail
		}
		if dir != "" {
			o.Root = dir
			if plan, err = engine.NewPlan(m, o); err != nil {
				_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
				return exitFail
			}
		}
	}

	printPlan(e, plan, f.dryRun)
	if f.dryRun {
		return exitOK
	}
	if !f.yes {
		ok, err := ask.yes("Install?")
		if err != nil || !ok {
			_, _ = fmt.Fprintln(e.Out, "Nothing was changed.")
			return exitFail
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report := reporter(e, f.verbose)
	if _, err := engine.Apply(ctx, plan, p.Files, p.Uninstaller, report); err != nil {
		_, _ = fmt.Fprintf(e.Err, "Install failed, and the changes were undone: %v\n", err)
		return exitFail
	}
	if plan.RefreshMenu {
		refreshMenu(report)
	}
	_, _ = fmt.Fprintf(e.Out, "Installed %s %s in %s.\nTo remove it, run %s/%s.\n", m.App.Name, m.App.Version, plan.Root, plan.Root, engine.UninstallName)
	if len(plan.Links) > 0 {
		if bin := filepath.Dir(plan.Links[0].Dst); !onPath(bin, e.Getenv("PATH")) {
			_, _ = fmt.Fprintf(e.Out, "%s is not on your PATH, so the links in it are not found by name. Add it to PATH to run them that way.\n", bin)
		}
	}
	return exitOK
}

// refreshMenu asks the desktop to read the launcher entries again. A
// failure is a warning: the files are in place, and the menu catches up at
// the next login.
func refreshMenu(report engine.Reporter) {
	if err := platform.RefreshMenu(); err != nil {
		report(engine.Event{Kind: engine.Warn, Text: fmt.Sprintf("the launcher menu was not refreshed: %v", err)})
	}
}

func onPath(dir, path string) bool {
	for _, p := range filepath.SplitList(path) {
		if p != "" && filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// printPlan states what the install will do; with all, every path.
func printPlan(e Env, p *engine.Plan, all bool) {
	var size int64
	var replaced []string
	for _, f := range p.Files {
		size += f.Size
		if f.Exists {
			replaced = append(replaced, f.Dst)
		}
	}
	for _, l := range p.Links {
		if l.Exists {
			replaced = append(replaced, l.Dst)
		}
	}
	_, _ = fmt.Fprintf(e.Out, "%s %s will be installed in %s (%d files, %d bytes).\n",
		p.Manifest.App.Name, p.Manifest.App.Version, p.Root, len(p.Files), size)
	for _, r := range replaced {
		_, _ = fmt.Fprintf(e.Out, "  replaces %s (the uninstaller puts it back)\n", r)
	}
	if !all {
		return
	}
	for _, d := range p.Dirs {
		_, _ = fmt.Fprintf(e.Out, "  create   %s/\n", d)
	}
	for _, f := range p.Files {
		verb := "create  "
		if f.Exists {
			verb = "replace "
		}
		note := ""
		if f.Secret {
			note = ", holds a secret"
		}
		_, _ = fmt.Fprintf(e.Out, "  %s %s (%o%s)\n", verb, f.Dst, f.Mode, note)
	}
	for _, l := range p.Links {
		verb := "link    "
		if l.Exists {
			verb = "replace "
		}
		_, _ = fmt.Fprintf(e.Out, "  %s %s -> %s\n", verb, l.Dst, l.Target)
	}
	_, _ = fmt.Fprintf(e.Out, "  create   %s\n", p.Index)
	_, _ = fmt.Fprintf(e.Out, "  create   %s\n", engine.ReceiptPath(p.Root))
}

// delegate runs the installed program's own uninstaller and returns its
// exit code (R9e): the code that installed a program is the code that
// removes it.
func delegate(ix *engine.Index, yes bool, e Env) int {
	var args []string
	if yes {
		args = append(args, "--yes")
	}
	// No timeout and no cancellation: stopping an uninstaller part-way is
	// worse than letting it finish, and it may be waiting on a person.
	cmd := exec.CommandContext(context.Background(), ix.Uninstaller, args...) // #nosec G204 -- the path is the one this app's own install recorded
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.In, e.Out, e.Err
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &exit):
		return exit.ExitCode()
	default:
		_, _ = fmt.Fprintf(e.Err, "The installed uninstaller could not be run: %v\n"+
			"The record of what was installed is %s.\n"+
			"To remove it with this installer instead, run again with --force-receipt-uninstall.\n",
			err, engine.ReceiptPath(ix.Root))
		return exitFail
	}
}

// forceUninstall removes an install with this installer's engine. It is
// the labelled fallback for an install whose own uninstaller is gone.
func forceUninstall(ix *engine.Index, e Env, verbose bool) int {
	r, err := engine.ReadReceipt(engine.ReceiptPath(ix.Root))
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	}
	_, _ = fmt.Fprintf(e.Out, "Removing %s %s with this installer's engine, not its own uninstaller.\n", r.App.Name, r.App.Version)
	report := reporter(e, verbose)
	if err := engine.Uninstall(r, report); err != nil {
		_, _ = fmt.Fprintf(e.Err, "%v\n", err)
		return exitFail
	}
	if r.RefreshMenu {
		refreshMenu(report)
	}
	return exitOK
}
