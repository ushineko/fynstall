//go:build !nogui

package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	fd "github.com/ushineko/fynedesygn"
	"github.com/ushineko/fynedesygn/forms"
	"github.com/ushineko/fynedesygn/logpane"
	"github.com/ushineko/fynedesygn/widgets"
	"github.com/ushineko/fynedesygn/wizard"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
)

// guiAvailable is true in the full build, which has the wizard.
const guiAvailable = true

// LaunchNow is the text of the finish page's launch check.
const LaunchNow = "Launch now"

// RunUninstaller is the text of the check that runs an existing install's
// own uninstaller.
const RunUninstaller = "Run its uninstaller now"

// installGUI runs the installer's wizard and returns the exit code.
func installGUI(m *manifest.Manifest, p Payload, f installFlags, e Env) int {
	g, err := newInstallWizard(m, p, f, e)
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		return exitFail
	}
	r := wizard.Run(g.options)
	return g.after(r)
}

// installWizard is the state the installer's pages share.
type installWizard struct {
	m       *manifest.Manifest
	p       Payload
	f       installFlags
	e       Env
	options wizard.Options
	// form holds the parameters; dir the install directory.
	form *forms.Form
	dir  *wizard.DirectoryPage
	// plan is made when the summary page is entered.
	plan    *engine.Plan
	planErr error
	launch  *widget.Check
	// existing is the index entry of an install already there.
	existing *engine.Index
	uninst   *widget.Check
}

// newInstallWizard builds the pages from the manifest (spec 001 phase 4):
// Welcome, Licence, Settings, Location, Ready, Installing, Done. A page
// with nothing to show is left out.
func newInstallWizard(m *manifest.Manifest, p Payload, f installFlags, e Env) (*installWizard, error) {
	g := &installWizard{m: m, p: p, f: f, e: e}
	g.options = wizard.Options{
		AppID: m.App.ID + ".installer",
		Name:  m.App.Name + " " + m.App.Version,
		Icon:  icon(m, p.Files),
	}
	ix, _, err := engine.ReadIndex(m, f.scope, e.Getenv)
	switch {
	case err == nil:
		g.existing = ix
		g.options.Pages = g.installedPages()
		return g, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}

	pages := []wizard.Page{wizard.Welcome("Welcome", g.welcome())}
	if m.Licence != "" {
		pages = append(pages, wizard.Licence(m.Licence))
	}
	if len(m.Parameters) > 0 {
		page, err := g.parametersPage()
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	root := f.dir
	if root == "" {
		if root, err = engine.DefaultRoot(m, f.scope, e.Getenv); err != nil {
			return nil, err
		}
	}
	g.dir = wizard.Directory("Location", "Install "+m.App.Name+" into:", root, func(s string) error {
		if !filepath.IsAbs(s) {
			return errors.New("choose an absolute path")
		}
		return nil
	})
	pages = append(pages, g.dir, &summaryPage{g: g})
	pages = append(pages, wizard.Progress("Installing", engine.ManifestSteps(m), m.App.Name+" is installed.", g.job).WithBar())
	var checks []*widget.Check
	if m.Launch != "" {
		g.launch = widget.NewCheck(LaunchNow, nil)
		checks = append(checks, g.launch)
	}
	pages = append(pages, wizard.Finish("Done", g.finishText(), checks...))
	g.options.Pages = pages
	return g, nil
}

func (g *installWizard) welcome() string {
	if g.m.Welcome != "" {
		return g.m.Welcome
	}
	s := fmt.Sprintf("This installs **%s %s**", g.m.App.Name, g.m.App.Version)
	if g.m.App.Publisher != "" {
		s += " from " + g.m.App.Publisher
	}
	return s + ".\n\nIt goes into your home directory and needs no administrator rights. " +
		"It installs an uninstaller beside the program, which puts back anything the install replaced."
}

func (g *installWizard) finishText() string {
	return fmt.Sprintf("**%s %s** is installed. To remove it, use the Uninstall action in the launcher, or run the `uninstall` program in its directory.",
		g.m.App.Name, g.m.App.Version)
}

// parametersPage is a form with a field per parameter, filled from the
// flags, the side file and the defaults; a secret is a password entry.
func (g *installWizard) parametersPage() (wizard.Page, error) {
	side, err := readSideFile(g.e.ExeDir, g.m)
	if err != nil {
		return nil, err
	}
	initial := map[string]string{}
	var fields []*forms.Field
	for _, p := range g.m.Parameters {
		v, ok := g.f.params[p.Name]
		if !ok {
			v, ok = side[p.Name]
		}
		if !ok {
			v = p.Default
		}
		initial[p.Name] = v
		if p.Secret {
			entry := widget.NewPasswordEntry()
			field := forms.Custom(p.Name, label(p), entry, func() string { return entry.Text }, entry.SetText)
			entry.OnChanged = func(s string) { field.Notify(s) }
			fields = append(fields, field)
			continue
		}
		fields = append(fields, forms.Entry(p.Name, label(p), p.Description))
	}
	g.form = forms.New(fields...)
	return wizard.Form("Settings", g.form, initial, func() bool {
		vals := g.form.Values()
		for _, p := range g.m.Parameters {
			if p.Required && strings.TrimSpace(vals[p.Name]) == "" {
				return false
			}
		}
		return true
	}), nil
}

// params are the values on the settings page, with empty ones taking the
// default.
func (g *installWizard) params() map[string]string {
	out := map[string]string{}
	var vals map[string]string
	if g.form != nil {
		vals = g.form.Values()
	}
	for _, p := range g.m.Parameters {
		v := vals[p.Name]
		if v == "" {
			v = p.Default
		}
		out[p.Name] = v
	}
	return out
}

// summaryPage makes the plan when it is entered and shows what it will do.
// A plan that cannot be made holds Install and says why.
type summaryPage struct {
	g   *installWizard
	box *fyne.Container
}

func (s *summaryPage) Title() string     { return "Ready" }
func (s *summaryPage) NextLabel() string { return "Install" }
func (s *summaryPage) Valid() bool       { return s.g.planErr == nil && s.g.plan != nil }

func (s *summaryPage) Build(*wizard.Wizard) fyne.CanvasObject {
	s.box = container.NewVBox()
	return s.box
}

func (s *summaryPage) Enter(w *wizard.Wizard) {
	g := s.g
	g.plan, g.planErr = engine.NewPlan(g.m, engine.Options{
		Scope: g.f.scope, Root: g.dir.Value(), Env: g.e.Getenv, Uninstaller: g.p.Uninstaller, Params: g.params(),
	})
	s.box.Objects = nil
	if g.planErr != nil {
		w.SetMessage(g.planErr.Error(), fd.StatusBad)
		s.box.Add(widgets.Wrapped("The install cannot go ahead. Go back and change the location or the settings."))
		s.box.Refresh()
		return
	}
	var size int64
	var replaced []string
	for _, f := range g.plan.Files {
		size += f.Size
		if f.Exists {
			replaced = append(replaced, f.Dst)
		}
	}
	rows := []fyne.CanvasObject{
		widgets.PlainRow("Program", g.m.App.Name+" "+g.m.App.Version),
		widgets.PlainRow("Location", g.plan.Root),
		widgets.PlainRow("Files", fmt.Sprintf("%d, %s", len(g.plan.Files), widgets.HumanSize(size))),
	}
	for _, l := range g.plan.Links {
		rows = append(rows, widgets.PlainRow("Link", l.Dst))
	}
	for _, d := range g.m.Desktop {
		rows = append(rows, widgets.PlainRow("Launcher entry", d.Name))
	}
	for _, f := range g.plan.Files {
		if f.Source == engine.FromContent && !strings.HasSuffix(f.Dst, ".desktop") {
			rows = append(rows, widgets.PlainRow("Settings file", f.Dst))
		}
	}
	for _, r := range replaced {
		rows = append(rows, widgets.PlainRow("Replaces", r+" (put back on uninstall)"))
	}
	s.box.Objects = rows
	s.box.Refresh()
}

// job installs the plan the summary page made, reporting the engine's
// steps and files to the progress page.
func (g *installWizard) job(ctx context.Context, r *wizard.Reporter) error {
	last := -1
	report := func(ev engine.Event) {
		switch ev.Kind {
		case engine.Step:
			if ev.Step >= 0 {
				if last >= 0 {
					r.Finish(last, "")
				}
				r.Advance(ev.Step, "")
				last = ev.Step
			} else {
				r.Log(logpane.Warn, ev.Text)
			}
		case engine.Detail:
			r.Log(logpane.Info, g.short(ev.Text))
		case engine.Warn:
			r.Log(logpane.Warn, ev.Text)
		case engine.Progress:
			fraction := 1.0
			if ev.Counts.BytesTotal > 0 {
				fraction = float64(ev.Counts.Bytes) / float64(ev.Counts.BytesTotal)
			}
			r.Progress(fraction, countsText(ev.Counts), g.short(ev.Text))
		}
	}
	if _, err := engine.Apply(ctx, g.plan, g.p.Files, g.p.Uninstaller, report); err != nil {
		return err
	}
	if last >= 0 {
		r.Finish(last, "")
	}
	if g.plan.RefreshMenu {
		refreshMenu(report)
	}
	return nil
}

// short is path relative to the install directory when it is inside it,
// so the log and the progress line show the part that differs.
func (g *installWizard) short(path string) string {
	if rel, err := filepath.Rel(g.plan.Root, path); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return path
}

// installedPages say the program is already installed, and offer its own
// uninstaller (R9e). Upgrade arrives in spec 001 phase 6.
func (g *installWizard) installedPages() []wizard.Page {
	g.uninst = widget.NewCheck(RunUninstaller, nil)
	return []wizard.Page{
		wizard.Welcome("Already installed", fmt.Sprintf(
			"**%s %s** is already installed in `%s`.\n\nTo install this copy, remove that one first with its own uninstaller.",
			g.m.App.Name, g.existing.Version, g.existing.Root)),
		wizard.Finish("Remove it first", "The uninstaller that came with that install removes it, and puts back anything it replaced.", g.uninst),
	}
}

// after acts on the wizard's result: launch the program, or run an
// existing install's uninstaller. It returns the exit code.
func (g *installWizard) after(r wizard.Result) int {
	switch r.Outcome {
	case wizard.Failed:
		return exitFail
	case wizard.Cancelled:
		return exitFail
	}
	if g.existing != nil {
		if r.Checks[RunUninstaller] {
			start(g.existing.Uninstaller, "--gui")
		}
		return exitOK
	}
	if g.launch != nil && r.Checks[LaunchNow] {
		start(filepath.Join(g.plan.Root, filepath.FromSlash(g.m.Launch)))
	}
	return exitOK
}

// start runs a program and does not wait for it: the installer exits and
// the program goes on.
func start(path string, args ...string) {
	cmd := exec.CommandContext(context.Background(), path, args...) // #nosec G204 -- a path this install wrote, or its index recorded
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}

// icon is the largest app icon in the payload, for the window.
func icon(m *manifest.Manifest, files fs.FS) fyne.Resource {
	if len(m.Icons) == 0 || files == nil {
		return nil
	}
	ic := m.Icons[len(m.Icons)-1]
	b, err := fs.ReadFile(files, ic.Path())
	if err != nil {
		return nil
	}
	return fyne.NewStaticResource(filepath.Base(ic.Path()), b)
}

// notice shows text in a window of its own, for a problem found before any
// wizard could start: started from the desktop, stderr goes nowhere.
func notice(title, text string) {
	wizard.Run(wizard.Options{Name: title, Pages: []wizard.Page{wizard.Finish(title, text)}})
}

// uninstallGUI asks once, removes, and reports, in one small window
// (fynedesygn spec 062). skip is --yes: the window shows only the result.
func uninstallGUI(r *engine.Receipt, skip bool) int {
	res := wizard.RunConfirm(uninstallConfirm(r, skip))
	if res.Outcome != wizard.Finished {
		return exitFail
	}
	return exitOK
}

// uninstallConfirm is the uninstaller's question, its job and its result.
func uninstallConfirm(r *engine.Receipt, skip bool) wizard.ConfirmOptions {
	detail := fmt.Sprintf("It removes `%s`, and puts back anything its install replaced.", r.Root)
	for _, k := range r.Keep {
		detail += fmt.Sprintf("\n\nYour data in `%s`, if any, is left where it is.", k)
	}
	return wizard.ConfirmOptions{
		AppID:    r.App.ID + ".uninstaller",
		Name:     "Uninstall " + r.App.Name,
		Question: fmt.Sprintf("Uninstall %s %s?", r.App.Name, r.App.Version),
		Detail:   detail,
		Action:   "Uninstall", Destructive: true,
		Done:         fmt.Sprintf("%s %s was removed.", r.App.Name, r.App.Version),
		SkipQuestion: skip,
		Job: func(context.Context) error {
			report := func(engine.Event) {}
			// The leftovers are not shown yet: the window's result is one
			// line, set before the job runs. The list and "Remove them
			// too" wait for fynedesygn (spec 002 phase 3).
			if _, err := engine.Uninstall(r, report); err != nil {
				return err
			}
			if r.RefreshMenu {
				refreshMenu(report)
			}
			return nil
		},
	}
}
