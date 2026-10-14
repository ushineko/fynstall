//go:build !nogui

package installer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
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
	"github.com/ushineko/fynstall/extension"
	xgui "github.com/ushineko/fynstall/extension/gui"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
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
		// Started from the desktop, stderr goes nowhere: say it in a window.
		_, _ = fmt.Fprintf(e.Err, "installer: %v\n", err)
		notice(m.App.Name+" "+m.App.Version, "The installer cannot start: "+err.Error())
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
	// form holds the parameters; dirs the install directory of each scope,
	// of which the one for scope is shown.
	form  *forms.Form
	dirs  map[string]*scopedDir
	scope string
	// plan is made when the summary page is entered.
	plan    *engine.Plan
	planErr error
	launch  *widget.Check
	// existing is the index entry of an install already there, and old
	// its receipt when it can be read: then this install replaces it.
	existing *engine.Index
	old      *installed
	uninst   *widget.Check
	// previous are the values the installed version was given; deferred
	// the secrets among them only the privileged helper can read.
	previous map[string]string
	deferred []string
	// uninstalling is true when the person chose "Uninstall instead".
	uninstalling bool
	// ext is the program's own installer code (spec 003), xc what it sees,
	// and xparams the values of the parameters its pages set. xlog is
	// where its log lines go: the progress page's log once the job runs.
	ext     *extension.Extension
	xc      *extension.Context
	xparams map[string]string
	xlog    func(extension.Level, string)
}

// placed is a page and its rank among the wizard's pages (spec 003).
type placed struct {
	rank float64
	page wizard.Page
}

// newInstallWizard builds the pages from the manifest (spec 001 phase 4):
// Welcome, Licence, Settings, Location, Ready, Installing, Done. A page
// with nothing to show is left out.
func newInstallWizard(m *manifest.Manifest, p Payload, f installFlags, e Env) (*installWizard, error) {
	g := &installWizard{m: m, p: p, f: f, e: e, scope: f.scope, dirs: map[string]*scopedDir{}, ext: p.Extension, xparams: map[string]string{}}
	g.xc = installContext(m, func() string { return g.scope }, func() string {
		if g.plan == nil {
			return "" // not chosen until the summary has made the plan
		}
		return g.plan.Root
	}, g.xparams, func(l extension.Level, text string) {
		if g.xlog != nil {
			g.xlog(l, text)
		}
	})
	g.options = wizard.Options{
		AppID: m.App.ID + ".installer",
		Name:  m.App.Name + " " + m.App.Version,
		Icon:  icon(m, p.Files),
	}
	ix, err := findExisting(m, f.scope, e.Getenv)
	if err != nil {
		return nil, err
	}
	var pages []placed
	add := func(id string, page wizard.Page) { pages = append(pages, placed{extension.Rank(id), page}) }
	if ix != nil {
		g.existing = ix
		if g.old, err = readExisting(ix); err != nil {
			// Without its record there is nothing to replace it from: the
			// window says so and offers its uninstaller.
			g.options.Pages = g.installedPages()
			return g, nil //nolint:nilerr // shown by the window instead
		}
		g.scope = g.old.receipt.Scope
		g.previous, g.deferred = engine.Previous(m, g.old.receipt, e.Getenv)
		if !needsElevation(g.scope, g.e.Getenv) {
			g.deferred = nil
		}
		add(extension.PageWelcome, &replacePage{g: g})
	} else {
		add(extension.PageWelcome, wizard.Welcome("Welcome", g.welcome()))
	}
	if m.Licence != "" {
		add(extension.PageLicence, wizard.Licence(m.Licence))
	}
	initial, err := g.initialValues()
	if err != nil {
		return nil, err
	}
	paged := g.ext.Sets()
	for _, name := range paged {
		g.xparams[name] = initial[name]
	}
	if len(m.Parameters) > len(paged) {
		add(extension.PageSettings, g.parametersPage(initial, paged))
	}
	for _, xp := range orderedPages(g.ext) {
		build, ok := xgui.Lookup(xp.ID)
		if !ok {
			return nil, fmt.Errorf("its page %s has no window", xp.ID)
		}
		pages = append(pages, placed{xp.At.Rank(), build(g.xc)})
	}
	scopes, err := offeredScopes(m, e.Getenv)
	if err != nil {
		return nil, err
	}
	if len(scopes) > 1 && g.old == nil {
		add(extension.PageLocation, &scopePage{g: g})
	}
	for _, scope := range scopes {
		if g.old != nil {
			break // a new version goes where the old one is
		}
		root := f.dir
		if root == "" || scope != f.scope {
			if root, err = engine.DefaultRoot(m, scope, e.Getenv); err != nil {
				return nil, err
			}
		}
		d := &scopedDir{g: g, scope: scope, DirectoryPage: wizard.Directory("Location", "Install "+m.App.Name+" into:", root, func(s string) error {
			if !filepath.IsAbs(s) {
				return errors.New("choose an absolute path")
			}
			return nil
		})}
		g.dirs[scope] = d
		add(extension.PageLocation, d)
	}
	add(extension.PageSummary, &summaryPage{g: g})
	add(extension.PageInstalling, wizard.Progress("Installing", engine.ManifestSteps(m, g.existing != nil), m.App.Name+" is installed.", g.job).WithBar())
	var checks []*widget.Check
	if m.Launch != "" {
		g.launch = widget.NewCheck(LaunchNow, nil)
		// Ticked where the config says the program is started by default
		// (spec 008); a person clears it to say no.
		g.launch.SetChecked(m.LaunchDefault)
		checks = append(checks, g.launch)
	}
	add(extension.PageFinish, wizard.Finish("Done", g.finishText(), checks...))
	// The program's own pages go among these by rank; pages of one rank
	// keep the order they were added in.
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].rank < pages[j].rank })
	for _, pl := range pages {
		g.options.Pages = append(g.options.Pages, pl.page)
	}
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
	switch {
	case slices.Contains(g.m.Scopes, "system") && elevated():
		// Run as an administrator already: nothing more is asked for.
		s += ".\n\n"
	case slices.Contains(g.m.Scopes, "system") && len(g.m.Scopes) > 1:
		s += ".\n\nInstalled for you, it needs no administrator rights. Installed for everyone on this computer, " +
			"it asks for an administrator once. "
	case slices.Contains(g.m.Scopes, "system"):
		s += ".\n\nIt is installed for everyone on this computer, so it asks for an administrator once. "
	default:
		s += ".\n\nIt goes into your home directory and needs no administrator rights. "
	}
	return s + "It installs an uninstaller beside the program, which puts back anything the install replaced."
}

func (g *installWizard) finishText() string {
	return fmt.Sprintf("**%s %s** is installed. To remove it, use the Uninstall action in the launcher, or run the `uninstall` program in its directory.",
		g.m.App.Name, g.m.App.Version)
}

// initialValues is what each parameter starts as: from the flags, the side
// file, the installed version, or its default.
func (g *installWizard) initialValues() (map[string]string, error) {
	side, err := readSideFile(g.e.ExeDir, g.m)
	if err != nil {
		return nil, err
	}
	initial := map[string]string{}
	for _, p := range g.m.Parameters {
		v, ok := g.f.params[p.Name]
		if !ok {
			v, ok = side[p.Name]
		}
		if !ok {
			v, ok = g.previous[p.Name]
		}
		if !ok {
			v = p.Default
		}
		initial[p.Name] = v
	}
	return initial, nil
}

// parametersPage is a form with a field per parameter, filled from
// initial; a secret is a password entry. A parameter that a page of the
// program's own sets (paged) has no field here: its page asks.
func (g *installWizard) parametersPage(initial map[string]string, paged []string) wizard.Page {
	var fields []*forms.Field
	for _, p := range g.m.Parameters {
		if slices.Contains(paged, p.Name) {
			continue
		}
		if p.Secret {
			entry := widget.NewPasswordEntry()
			if slices.Contains(g.deferred, p.Name) {
				entry.PlaceHolder = "kept from the installed version"
			}
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
			if slices.Contains(paged, p.Name) {
				continue
			}
			if p.Required && strings.TrimSpace(vals[p.Name]) == "" && !slices.Contains(g.deferred, p.Name) {
				return false
			}
		}
		return true
	})
}

// params are the values on the settings page, with empty ones taking the
// default.
func (g *installWizard) params() map[string]string {
	out := map[string]string{}
	var vals map[string]string
	if g.form != nil {
		vals = g.form.Values()
	}
	paged := g.ext.Sets()
	for _, p := range g.m.Parameters {
		v := vals[p.Name]
		if slices.Contains(paged, p.Name) {
			v = g.xparams[p.Name]
		}
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
	o := engine.Options{Scope: g.scope, Env: g.e.Getenv, Uninstaller: g.p.Uninstaller, Params: g.params()}
	if g.old != nil {
		o.Root, o.Replacing = g.old.ix.Root, g.old.receipt
	} else {
		o.Root = g.dirs[g.scope].Value()
	}
	g.plan, g.planErr = engine.NewPlan(g.m, o)
	if g.planErr == nil {
		g.planErr = requiredAfterPages(g.m, g.ext, o.Params)
	}
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
	}
	if g.old != nil {
		rows = append(rows, widgets.PlainRow("Replaces", replacementText(g.m, g.old.receipt.App.Version)+", through its own uninstaller"))
	}
	if account := runsAs(); account != "" && g.plan.Scope == "user" {
		// Started with another account's name and password: "for you" is
		// that account, and the person at the desktop will not find it.
		rows = append(rows, widgets.PlainRow("Installed for", account+", the account this installer runs as, not the one at this desktop"))
	}
	rows = append(rows,
		widgets.PlainRow("Location", g.plan.Root),
		widgets.PlainRow("Files", fmt.Sprintf("%d, %s", len(g.plan.Files), widgets.HumanSize(size))),
	)
	for _, l := range g.plan.Links {
		rows = append(rows, widgets.PlainRow("Link", l.Dst))
	}
	for _, d := range g.m.Desktop {
		rows = append(rows, widgets.PlainRow("Launcher entry", d.Name))
	}
	for _, name := range g.plan.Autostart {
		rows = append(rows, widgets.PlainRow("Starts at login", name))
	}
	for _, f := range g.plan.Files {
		if f.Source == engine.FromContent && !strings.HasSuffix(f.Dst, ".desktop") {
			rows = append(rows, widgets.PlainRow("Settings file", f.Dst))
		}
	}
	for _, r := range replaced {
		rows = append(rows, widgets.PlainRow("Replaces", r+" (put back on uninstall)"))
	}
	for _, a := range actionLines(g.plan) {
		label, text, _ := strings.Cut(a, " ")
		if strings.HasPrefix(a, "on uninstall") {
			label, text = "On uninstall", strings.TrimPrefix(a, "on uninstall, ")
		}
		rows = append(rows, widgets.PlainRow(gloss[label], strings.TrimSpace(text)))
	}
	for _, h := range installHookLines(g.ext, g.m.App.Name) {
		rows = append(rows, widgets.PlainRow("Its own code", h))
	}
	s.box.Objects = rows
	s.box.Refresh()
}

// Labels of the replace page.
const (
	ConfirmDowngrade = "Replace it with this older version"
	UninstallInstead = "Uninstall it instead"
)

// replacePage is the first page when the app is installed already (R17):
// what this installer will do to it. A downgrade waits for a check. The
// button hands over to the installed uninstaller.
type replacePage struct {
	g     *installWizard
	check *widget.Check
}

func (r *replacePage) Title() string {
	switch replacement(r.g.old.receipt.App.Version, r.g.m.App.Version) {
	case "upgrade":
		return "Upgrade"
	case "downgrade":
		return "Downgrade"
	}
	return "Repair"
}

func (r *replacePage) Valid() bool {
	return replacement(r.g.old.receipt.App.Version, r.g.m.App.Version) != "downgrade" || (r.check != nil && r.check.Checked)
}

func (r *replacePage) Build(w *wizard.Wizard) fyne.CanvasObject {
	g, old := r.g, r.g.old.receipt
	box := container.NewVBox(
		widgets.Wrapped(fmt.Sprintf("%s %s is installed in %s.", old.App.Name, old.App.Version, old.Root)),
		widgets.Wrapped(fmt.Sprintf("%s. The uninstaller that came with %s removes it first, and puts back anything it replaced; "+
			"then this version installs in the same place.", replacementText(g.m, old.App.Version), old.App.Version)),
	)
	for _, k := range old.Keep {
		box.Add(widgets.Wrapped(fmt.Sprintf("Your data in %s stays.", k)))
	}
	if replacement(old.App.Version, g.m.App.Version) == "downgrade" {
		r.check = widget.NewCheck(ConfirmDowngrade, func(bool) { w.Revalidate() })
		box.Add(r.check)
	}
	box.Add(widget.NewSeparator())
	box.Add(container.NewHBox(widget.NewButton(UninstallInstead, func() {
		g.uninstalling = true
		start(g.existing.Uninstaller, true, "--gui")
		w.Cancel()
	})))
	return box
}

// Labels of the scope choice.
const (
	ForMe       = "Just me"
	ForEveryone = "Everyone on this computer (asks for an administrator)"
)

// offeredScopes are the scopes of m that this platform installs in. A
// config offers the same scopes on every platform; one that a platform's
// backend does not have yet is left out, as the command line leaves it out
// unless it is asked for by name.
func offeredScopes(m *manifest.Manifest, getenv func(string) string) ([]string, error) {
	var out []string
	var last error
	for _, scope := range m.Scopes {
		_, err := platform.Vars(scope, getenv)
		switch {
		case errors.Is(err, platform.ErrScopeUnavailable):
			last = err
		case err != nil:
			return nil, fmt.Errorf("resolve paths: %w", err)
		default:
			out = append(out, scope)
		}
	}
	if len(out) == 0 {
		return nil, last
	}
	return out, nil
}

// scopePage asks who the install is for, when the config offers both.
type scopePage struct {
	g     *installWizard
	radio *widget.RadioGroup
}

func (s *scopePage) Title() string { return "Install for" }

func (s *scopePage) Build(w *wizard.Wizard) fyne.CanvasObject {
	s.radio = widget.NewRadioGroup([]string{ForMe, ForEveryone}, func(choice string) {
		s.g.scope = "user"
		if choice == ForEveryone {
			s.g.scope = "system"
		}
		w.Revalidate()
	})
	s.radio.Required = true
	if s.g.scope == "system" {
		s.radio.SetSelected(ForEveryone)
	} else {
		s.radio.SetSelected(ForMe)
	}
	return container.NewVBox(widgets.Wrapped("Who is "+s.g.m.App.Name+" for?"), s.radio)
}

// scopedDir is the location page of one scope; it is skipped while the
// other scope is chosen, so each scope keeps its own default and what the
// person typed for it.
type scopedDir struct {
	*wizard.DirectoryPage
	g     *installWizard
	scope string
}

func (d *scopedDir) Skip() bool { return d.g.scope != d.scope }

// job installs the plan the summary page made, reporting the engine's
// steps and files to the progress page.
func (g *installWizard) job(ctx context.Context, r *wizard.Reporter) error {
	elevate := needsElevation(g.plan.Scope, g.e.Getenv)
	var l *lock
	if !elevate {
		var err error
		if l, err = takeLock(g.m.App.ID, g.plan.Scope, g.e.Getenv); err != nil {
			return err
		}
		defer l.Release()
	}
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
	g.xlog = func(lv extension.Level, text string) {
		level := logpane.Info
		if lv == extension.Warn {
			level = logpane.Warn
		}
		r.Log(level, text)
	}
	// For an install for everyone the hooks run in the helper (R6).
	if !elevate {
		if err := beforeInstall(ctx, g.ext, g.xc); err != nil {
			return err
		}
	}
	switch {
	case elevate:
		if err := applyRequest(ctx, g.plan, true, g.e, report); err != nil {
			return err
		}
	case g.old != nil:
		if err := replace(ctx, g.plan, g.old, g.p, g.e.Getenv, l, report); err != nil {
			return err
		}
	default:
		if _, err := engine.Apply(ctx, g.plan, g.p.Files, g.p.Uninstaller, report); err != nil {
			return err
		}
	}
	if last >= 0 {
		r.Finish(last, "")
	}
	if !elevate {
		if err := afterInstall(ctx, g.ext, g.xc); err != nil {
			return err
		}
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
	if g.uninstalling {
		return exitOK
	}
	switch r.Outcome {
	case wizard.Failed:
		return exitFail
	case wizard.Cancelled:
		return exitFail
	}
	// Only the pages that could not replace the install offer its
	// uninstaller. A repair or an upgrade has an existing install too, and
	// ends on the same finish page as a first install: a desk check found
	// its "Launch now" doing nothing.
	if g.uninst != nil {
		if r.Checks[RunUninstaller] {
			start(g.existing.Uninstaller, true, "--gui")
		}
		return exitOK
	}
	if g.launch != nil && r.Checks[LaunchNow] {
		launch(g.m, g.plan.Root, g.e.Getenv)
	}
	return exitOK
}

// icon is the largest app icon in the payload, for the window. A Windows
// installer carries one .ico file and no PNGs, and the window takes the PNG
// inside it.
func icon(m *manifest.Manifest, files fs.FS) fyne.Resource {
	if files == nil {
		return nil
	}
	if len(m.Icons) == 0 {
		b, err := fs.ReadFile(files, manifest.WindowsIcon)
		if err != nil {
			return nil
		}
		return pngResource(icoPNG(b))
	}
	ic := m.Icons[len(m.Icons)-1]
	b, err := fs.ReadFile(files, ic.Path())
	if err != nil {
		return nil
	}
	return fyne.NewStaticResource(filepath.Base(ic.Path()), b)
}

// pngResource is b as a window icon, or nil when there is no image.
func pngResource(b []byte) fyne.Resource {
	if b == nil {
		return nil
	}
	return fyne.NewStaticResource("icon.png", b)
}

// installedIcon is the icon of the install at root, for the uninstaller's
// window: the .ico that a Windows install keeps in its directory. Elsewhere
// there is none there, and the window has Fyne's icon as before.
func installedIcon(root string) fyne.Resource {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifest.WindowsIcon))) // #nosec G304 -- a file of this install, named by fynstall
	if err != nil {
		return nil
	}
	return pngResource(icoPNG(b))
}

// notice shows text in a window of its own, for a problem found before any
// wizard could start: started from the desktop, stderr goes nowhere.
func notice(title, text string) {
	wizard.Run(wizard.Options{Name: title, Pages: []wizard.Page{wizard.Finish(title, text)}})
}

// uninstallGUI asks once, removes, and reports, in one small window
// (fynedesygn spec 062). skip is --yes: the window shows only the result.
func uninstallGUI(r *engine.Receipt, skip bool, env func(string) string, ext *extension.Extension) int {
	o, done := uninstallConfirm(r, skip, env, ext)
	res := wizard.RunConfirm(o)
	done()
	if res.Outcome != wizard.Finished {
		return exitFail
	}
	return exitOK
}

// uninstallConfirm is the uninstaller's question, its job and its result.
// When the program left files the install did not create, the window then
// lists them and offers to remove them too (spec 002 D5).
//
// A system install is removed by a helper under pkexec (spec 001 R13). The
// helper waits after the uninstall while the window asks about the
// leftovers, so the person is asked for an administrator once. done ends a
// helper the window left waiting.
func uninstallConfirm(r *engine.Receipt, skip bool, env func(string) string, ext *extension.Extension) (wizard.ConfirmOptions, func()) {
	var left []engine.Leftover
	var h *helper
	done := func() {
		if h != nil {
			_ = h.finish() // keeps the leftovers
			h = nil
		}
	}
	detail := fmt.Sprintf("It removes `%s`, and puts back anything its install replaced.", r.Root)
	if needsElevation(r.Scope, env) {
		detail += "\n\nIt is installed for everyone on this computer, so it asks for an administrator."
	}
	for _, hk := range r.Hooks {
		detail += fmt.Sprintf("\n\nBefore removing anything, it runs `%s`.", commandLine(hk.Exec, hk.Args))
	}
	for _, line := range uninstallHookLines(ext, r.App.Name) {
		detail += "\n\n" + line
	}
	for _, k := range r.Keep {
		detail += fmt.Sprintf("\n\nYour data in `%s`, if any, is left where it is.", k)
	}
	report := func(engine.Event) {}
	uninstall := func(ctx context.Context) error {
		if needsElevation(r.Scope, env) {
			// Not the job's context: the window cancels that when the job
			// returns, and the helper must outlive it to hear the answer
			// about the leftovers. An uninstall is not cancelled part-way.
			var err error
			if h, err = startElevated(context.WithoutCancel(ctx), true, env, os.Stderr, []string{"--apply-uninstall"}, report); err != nil {
				return err
			}
			if left = h.next(); len(left) == 0 {
				err, h = h.finish(), nil
				return err
			}
			return nil
		}
		unlock, err := engine.Lock(r.App.ID, r.Scope, env)
		if err != nil {
			return err
		}
		defer unlock()
		// The program's own hooks, around the removal (spec 003). An
		// install for everyone runs them in its helper, above.
		xc := uninstallContext(r, engine.ReasonUninstall, nil)
		if err := beforeUninstall(ctx, ext, xc); err != nil {
			return err
		}
		if left, err = engine.Uninstall(r, engine.ReasonUninstall, report); err != nil {
			return err
		}
		return afterUninstall(ctx, ext, xc)
	}
	return wizard.ConfirmOptions{
		AppID:    r.App.ID + ".uninstaller",
		Name:     "Uninstall " + r.App.Name,
		Icon:     installedIcon(r.Root),
		Question: fmt.Sprintf("Uninstall %s %s?", r.App.Name, r.App.Version),
		Detail:   detail,
		Action:   "Uninstall", Destructive: true,
		Done:         fmt.Sprintf("%s %s was removed.", r.App.Name, r.App.Version),
		SkipQuestion: skip,
		Job: func(ctx context.Context) error {
			if err := uninstall(ctx); err != nil {
				return err
			}
			if r.RefreshMenu {
				refreshMenu(report)
			}
			return nil
		},
		Then: &wizard.ConfirmStep{
			Action: "Remove them too", Destructive: true, Decline: "Keep them",
			Ask: func() (string, string, bool) {
				return fmt.Sprintf("%s left %s it made.", r.App.Name, leftoverCount(left)), leftoversDetail(left), len(left) > 0
			},
			Job: func(context.Context) error {
				if h != nil {
					err := h.removeLeftovers()
					h = nil
					return err
				}
				return engine.RemoveLeftovers(r, left, report)
			},
			Done: fmt.Sprintf("%s %s was removed, with the files it made.", r.App.Name, r.App.Version),
		},
	}, done
}

// leftoversDetail is the Markdown under the leftovers question: why they
// were left, and their paths.
func leftoversDetail(left []engine.Leftover) string {
	var b strings.Builder
	b.WriteString("The install did not create these, so they were left. Keep them if the program's data matters to you.\n\n")
	for i, l := range left {
		if i == shownLeftoversGUI {
			fmt.Fprintf(&b, "- and %d more\n", len(left)-i)
			break
		}
		fmt.Fprintf(&b, "- `%s`\n", l)
	}
	return b.String()
}

// pagesWithoutWindow are the IDs of ext's pages that no window is
// registered for (spec 003 R4).
func pagesWithoutWindow(ext *extension.Extension) []string {
	var out []string
	if ext == nil {
		return nil
	}
	for _, p := range ext.Pages {
		if _, ok := xgui.Lookup(p.ID); !ok {
			out = append(out, p.ID)
		}
	}
	return out
}

// gloss labels the summary rows of the actions by the word actionLines
// starts each line with.
var gloss = map[string]string{ //nolint:gochecknoglobals // a fixed table
	"service": "Service", "run": "Runs", "move": "Moves", "On uninstall": "On uninstall",
}

// shownLeftoversGUI is how many leftovers the window lists by name.
const shownLeftoversGUI = 50
