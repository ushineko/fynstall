package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

type installFlags struct {
	cli, gui, yes, dryRun, uninstall, forceReceipt, verbose, version, downgrade bool
	dir, scope                                                                  string
	params                                                                      map[string]string
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
	fl.BoolVar(&f.downgrade, "downgrade", false, "replace an installed newer version with this older one without asking")
	fl.StringVar(&f.dir, "dir", "", "install directory (default from the installer)")
	fl.StringVar(&f.scope, "scope", "", "install scope: user, or system for everyone on this computer")
	applyPlanFile := fl.String("apply-plan", "", "used by the installer itself: apply the plan in this file as root")
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
	if *applyPlanFile != "" {
		return applyPlan(*applyPlanFile, m, p, e)
	}
	if f.scope == "" {
		f.scope = m.Scopes[0]
	}
	md, err := e.modeFor(modeInput{
		wantGUI: f.gui, wantCLI: f.cli,
		cliOnly: f.yes || f.dryRun || f.uninstall || f.forceReceipt,
	})
	if err != nil {
		_, _ = fmt.Fprintln(e.Err, err)
		return exitUsage
	}
	if md == modeGUI {
		return installGUI(m, p, f, e)
	}

	ix, err := findExisting(m, f.scope, e.Getenv)
	switch {
	case err != nil:
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	case ix != nil && f.forceReceipt:
		return forceUninstall(ix, e, f.verbose)
	case ix != nil && f.uninstall:
		return delegate(ix, f.yes, e)
	case ix == nil && (f.uninstall || f.forceReceipt):
		_, _ = fmt.Fprintf(e.Err, "%s is not installed.\n", m.App.Name)
		return exitFail
	case ix == nil:
		return install(m, p, f, e, nil)
	}
	// An upgrade, repair or downgrade (R17).
	old, err := readExisting(ix)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	}
	if f.dir != "" && filepath.Clean(f.dir) != ix.Root {
		_, _ = fmt.Fprintf(e.Err, "%s %s is installed in %s, and a new version goes where it is. Leave out --dir, or uninstall it first with --uninstall.\n",
			m.App.Name, ix.Version, ix.Root)
		return exitUsage
	}
	if replacement(ix.Version, m.App.Version) == "downgrade" && !f.downgrade && (f.yes || !e.Interactive) {
		_, _ = fmt.Fprintf(e.Err, "%s %s is installed, which is newer than %s. Pass --downgrade to replace it with this older version.\n",
			m.App.Name, ix.Version, m.App.Version)
		return exitUsage
	}
	f.scope, f.dir = old.receipt.Scope, ix.Root
	return install(m, p, f, e, old)
}

// install installs m, replacing old when it is not nil.
func install(m *manifest.Manifest, p Payload, f installFlags, e Env, old *installed) int {
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
	elevate := needsElevation(f.scope, e.Getenv)
	var previous map[string]string
	var deferred []string
	if old != nil {
		// A secret the person's process cannot read (a system install's
		// config file) is read by the helper, as root.
		previous, deferred = engine.Previous(m, old.receipt, e.Getenv)
		if !elevate {
			deferred = nil
		}
	}
	params, err := resolveParams(m, f.params, side, previous, deferred, prompt)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitUsage
	}
	var l *lock
	if !f.dryRun && !elevate {
		if l, err = takeLock(m.App.ID, f.scope, e.Getenv); err != nil {
			_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
			return exitFail
		}
		defer l.Release()
	}
	o := engine.Options{Scope: f.scope, Root: f.dir, Env: e.Getenv, Uninstaller: p.Uninstaller, Params: params}
	if old != nil {
		o.Replacing = old.receipt
	}
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

	if old != nil {
		_, _ = fmt.Fprintf(e.Out, "%s: the uninstaller that came with %s removes it first, then this version installs.\n",
			replacementText(m, old.receipt.App.Version), old.receipt.App.Version)
	}
	printPlan(e, plan, f.dryRun)
	if f.dryRun {
		return exitOK
	}
	if old != nil && replacement(old.receipt.App.Version, m.App.Version) == "downgrade" && !f.downgrade {
		ok, err := ask.yes(fmt.Sprintf("Replace %s %s with the older %s?", m.App.Name, old.receipt.App.Version, m.App.Version))
		if err != nil || !ok {
			_, _ = fmt.Fprintln(e.Out, "Nothing was changed.")
			return exitFail
		}
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
	if elevate {
		_, _ = fmt.Fprintln(e.Out, "This install is for everyone on this computer, so it needs an administrator.")
		if err := applyRequest(ctx, plan, false, e, report); err != nil {
			_, _ = fmt.Fprintf(e.Err, "%v\n", err)
			return exitFail
		}
	} else if old != nil {
		if err := replace(ctx, plan, old, p, e.Getenv, l, report); err != nil {
			_, _ = fmt.Fprintf(e.Err, "%v\n", err)
			return exitFail
		}
	} else if _, err := engine.Apply(ctx, plan, p.Files, p.Uninstaller, report); err != nil {
		_, _ = fmt.Fprintf(e.Err, "Install failed, and the changes were undone: %v\n", err)
		return exitFail
	}
	if plan.RefreshMenu {
		refreshMenu(report)
	}
	var size int64
	for _, pf := range plan.Files {
		size += pf.Size
	}
	_, _ = fmt.Fprintf(e.Out, "Copied %s files (%s).\n", thousands(len(plan.Files)), humanSize(size))
	_, _ = fmt.Fprintf(e.Out, "Installed %s %s in %s.\nTo remove it, run %s.\n", m.App.Name, m.App.Version, plan.Root, filepath.Join(plan.Root, engine.UninstallName))
	for _, d := range plan.PathDirs {
		_, _ = fmt.Fprintf(e.Out, "%s is on %s. A console that you open from now on finds the programs in it.\n", d, whosePath(plan.Scope, "your PATH"))
	}
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
	for _, l := range slices.Concat(p.Symlinks, p.Links) {
		if l.Exists {
			replaced = append(replaced, l.Dst)
		}
	}
	_, _ = fmt.Fprintf(e.Out, "%s %s will be installed in %s (%d files, %d bytes).\n",
		p.Manifest.App.Name, p.Manifest.App.Version, p.Root, len(p.Files), size)
	for _, r := range replaced {
		_, _ = fmt.Fprintf(e.Out, "  replaces %s (the uninstaller puts it back)\n", r)
	}
	if m := p.Manifest; platform.Integration == platform.NoIntegration && len(m.Desktop)+len(m.Links)+len(m.Icons) > 0 {
		// A part of the config that does nothing here says so (spec 002).
		_, _ = fmt.Fprintln(e.Out, "  launcher entries, icons and links on PATH are not applied on this platform yet")
	}
	// What the install changes outside its own files is always shown.
	for _, l := range shellLines(p) {
		_, _ = fmt.Fprintf(e.Out, "  %s\n", l)
	}
	// Actions are always shown: a run action runs a payload program.
	for _, a := range actionLines(p) {
		_, _ = fmt.Fprintf(e.Out, "  %s\n", a)
	}
	if !all {
		return
	}
	for _, d := range p.Dirs {
		_, _ = fmt.Fprintf(e.Out, "  create   %s%c\n", d, filepath.Separator)
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
	for _, l := range slices.Concat(p.Symlinks, p.Links) {
		verb := "link    "
		if l.Exists {
			verb = "replace "
		}
		_, _ = fmt.Fprintf(e.Out, "  %s %s -> %s\n", verb, l.Dst, l.Target)
	}
	_, _ = fmt.Fprintf(e.Out, "  create   %s\n", p.Index)
	_, _ = fmt.Fprintf(e.Out, "  create   %s\n", engine.ReceiptPath(p.Root))
}

// shellLines says what p adds to the Start Menu, the registry and PATH, one
// line each. It is empty off Windows.
func shellLines(p *engine.Plan) []string {
	var out []string
	for _, s := range p.Shortcuts {
		line := fmt.Sprintf("shortcut %s, which starts %s", s.Dst, commandLine(s.Target, s.Args))
		if s.Exists {
			line += "; replaces the one there, which the uninstaller puts back"
		}
		out = append(out, line)
	}
	for _, k := range p.Registry {
		out = append(out, fmt.Sprintf("registry %s (%d values)", k.Key, len(k.Values)))
	}
	for _, d := range p.PathDirs {
		out = append(out, fmt.Sprintf("PATH     %s is added to %s, unless it is there already", d, whosePath(p.Scope, "yours")))
	}
	return out
}

// actionLines says what each action of p will do, in order, and what the
// uninstall hooks will run, one line each.
func actionLines(p *engine.Plan) []string {
	var out []string
	for _, a := range p.Actions {
		switch {
		case a.Service != nil:
			s := a.Service
			line := fmt.Sprintf("service  %s (%s)", s.Name, s.Unit)
			if s.Start {
				line += ", started"
			}
			if s.Exists {
				line += "; replaces a service of that name, which the uninstaller puts back"
			}
			out = append(out, line)
		case a.Run != nil:
			r := a.Run
			undo := "no undo"
			if !r.NoUndo {
				undo = "undo: " + commandLine(r.Exec, r.Undo)
			}
			out = append(out, fmt.Sprintf("run      %s (%s)", commandLine(r.Exec, r.Args), undo))
		case a.Migrate != nil:
			m := a.Migrate
			if m.Present {
				out = append(out, fmt.Sprintf("move     %s to %s (the uninstaller leaves it there)", m.From, m.To))
			}
		}
	}
	for _, h := range p.Hooks {
		out = append(out, "on uninstall, run "+commandLine(h.Exec, h.Args))
	}
	return out
}

func commandLine(exec string, args []string) string {
	return strings.Join(append([]string{exec}, args...), " ")
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
	platform.Background(cmd)
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
	unlock, err := engine.Lock(r.App.ID, r.Scope, e.Getenv)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	}
	defer unlock()
	_, _ = fmt.Fprintf(e.Out, "Removing %s %s with this installer's engine, not its own uninstaller.\n", r.App.Name, r.App.Version)
	report := reporter(e, verbose)
	left, err := engine.Uninstall(r, engine.ReasonUninstall, report)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "%v\n", err)
		return exitFail
	}
	if r.RefreshMenu {
		refreshMenu(report)
	}
	printLeftovers(e, left, verbose)
	return exitOK
}

// whosePath names the PATH a directory goes on: the person's own, as mine
// says it, or the computer's for an install for everyone.
func whosePath(scope, mine string) string {
	if scope == "system" {
		return "the PATH of this computer"
	}
	return mine
}
