// A program's own pages and hooks in the wizard (spec 003), and an entry
// that starts at login (spec 005), driven headless against the real engine.

//go:build !nogui

package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynedesygn/fynetest"
	"github.com/ushineko/fynedesygn/wizard"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/extension"
	xgui "github.com/ushineko/fynstall/extension/gui"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// serverPage is the window of a page that sets the fixture's server
// parameter: one entry, and Next is held until it has a value.
type serverPage struct {
	c     *extension.Context
	entry *widget.Entry
}

func (p *serverPage) Title() string { return "Server" }
func (p *serverPage) Valid() bool   { return p.c.Param("server") != "" }
func (p *serverPage) Build(w *wizard.Wizard) fyne.CanvasObject {
	p.entry = widget.NewEntry()
	p.entry.SetText(p.c.Param("server"))
	p.entry.OnChanged = func(s string) {
		_ = p.c.SetParam("server", s)
		w.Revalidate()
	}
	return p.entry
}

func init() {
	xgui.Page("server", func(c *extension.Context) wizard.Page { return &serverPage{c: c} })
}

// pagedFixture is the wizard fixture with an extension: a page after the
// licence that sets the server parameter, and a hook at each point, each of
// which notes that it ran and what it saw.
func pagedFixture(t *testing.T) (*manifest.Manifest, Payload, Env, string, *[]string) {
	t.Helper()
	m, p, e, home := guiFixture(t)
	// The page asks for the server, so it has no default to pass with.
	m.Parameters[0].Default = ""
	m.Parameters[0].Required = true
	m.Parameters[1].Required = false
	var ran []string
	note := func(name string) func(context.Context, *extension.Context) error {
		return func(_ context.Context, c *extension.Context) error {
			ran = append(ran, name+" server="+c.Param("server")+" root="+filepath.Base(c.Root()))
			c.Log(extension.Info, name+" ran")
			return nil
		}
	}
	p.Extension = &extension.Extension{
		Pages: []extension.Page{{
			ID: "server", At: extension.After(extension.PageLicence), Sets: []string{"server"},
			CLI: func(context.Context, *extension.Context, extension.Prompter) error { return nil },
		}},
		BeforeInstall: note("before-install"), AfterInstall: note("after-install"),
		BeforeUninstall: note("before-uninstall"), AfterUninstall: note("after-uninstall"),
	}
	return m, p, e, home, &ran
}

/*
Spec 003. The program's page is in the wizard where it asked to be; the
parameter it sets has no field on the Settings page; Next waits for it; and
what it set is what the install uses. The hooks run around the changes: the
first before the install directory exists, the second once the program is
there.
*/
func TestAProgramsOwnPageSetsAParameterAndItsHooksRun(t *testing.T) {
	m, p, e, _, ran := pagedFixture(t)
	g, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.NoError(t, err)
	w := wizard.Headless(fynetest.App(t), g.options)
	var titles []string
	for _, pg := range g.options.Pages {
		titles = append(titles, pg.Title())
	}
	require.Equal(t, []string{"Welcome", "Licence", "Server", "Settings", "Location", "Ready", "Installing", "Done"}, titles)

	w.Next() // Welcome
	fynetest.FindCheck(w.Content()).SetChecked(true)
	w.Next() // Licence
	require.Equal(t, "Server", w.Current().Title())
	w.Next()
	require.Equal(t, "Server", w.Current().Title(), "the page's parameter is empty, so Next waits")
	fynetest.All[*widget.Entry](w.Content())[0].SetText("https://keyed.invalid")
	w.Next() // Server
	require.Equal(t, "Settings", w.Current().Title())
	entries := fynetest.All[*widget.Entry](w.Content())
	require.Len(t, entries, 1, "Settings has the token only: the server is the page's")
	require.True(t, entries[0].Password)
	w.Next() // Settings
	w.Next() // Location
	require.Equal(t, "Ready", w.Current().Title())
	require.NotNil(t, fynetest.FindLabel(w.Content(), "before the install, runs Hello's own set-up code"),
		"the summary says the program's own code will run (R7)")
	require.Empty(t, *ran, "nothing of the program's runs before Install")
	w.Next() // Install
	require.Equal(t, "Hello is installed.", w.Message())

	root := at(t, e, "programs", "io.example.hello")
	require.Equal(t, []string{
		"before-install server=https://keyed.invalid root=io.example.hello",
		"after-install server=https://keyed.invalid root=io.example.hello",
	}, *ran)
	cfg, err := os.ReadFile(at(t, e, "config", "hello", "config.yml"))
	require.NoError(t, err)
	require.Contains(t, string(cfg), "server: https://keyed.invalid\n", "the install used what the page set")
	_, err = os.Stat(filepath.Join(root, "bin", "hello"))
	require.NoError(t, err)
}

// A value from a flag starts the page with it, as it starts a Settings
// field: the page is valid at once.
func TestAProgramsOwnPageStartsWithTheValueAFlagGave(t *testing.T) {
	m, p, e, _, _ := pagedFixture(t)
	g, err := newInstallWizard(m, p, installFlags{scope: "user", params: map[string]string{"server": "https://flag.invalid"}}, e)
	require.NoError(t, err)
	w := wizard.Headless(fynetest.App(t), g.options)
	w.Next()
	fynetest.FindCheck(w.Content()).SetChecked(true)
	w.Next()
	require.Equal(t, "Server", w.Current().Title())
	require.Equal(t, "https://flag.invalid", fynetest.All[*widget.Entry](w.Content())[0].Text)
	w.Next()
	require.Equal(t, "Settings", w.Current().Title())
}

// A BeforeInstall hook that fails stops the install with the home as it
// was, and the window says nothing was changed.
func TestABeforeInstallHookThatFailsLeavesTheHomeUnchanged(t *testing.T) {
	m, p, e, home, _ := pagedFixture(t)
	p.Extension.BeforeInstall = func(context.Context, *extension.Context) error { return errors.New("the licence server said no") }
	before := snap(t, e, home)
	g, err := newInstallWizard(m, p, installFlags{scope: "user", params: map[string]string{"server": "https://keyed.invalid"}}, e)
	require.NoError(t, err)
	w := wizard.Headless(fynetest.App(t), g.options)
	w.Next()
	fynetest.FindCheck(w.Content()).SetChecked(true)
	for w.Current().Title() != "Installing" {
		w.Next()
	}
	require.Contains(t, w.Message(), "nothing was changed")
	require.Contains(t, w.Message(), "the licence server said no")
	require.Equal(t, before, snap(t, e, home))
}

// A page with no window stops the wizard before it opens, and the
// builder's check names the page (R4).
func TestAPageWithNoWindowIsNamed(t *testing.T) {
	m, p, e, _, _ := pagedFixture(t)
	p.Extension.Pages[0].ID = "nowhere"
	_, err := newInstallWizard(m, p, installFlags{scope: "user"}, e)
	require.ErrorContains(t, err, "page nowhere has no window")
	require.Equal(t, []string{"nowhere"}, pagesWithoutWindow(p.Extension))
	p.Extension.Pages[0].ID = "server"
	require.Empty(t, pagesWithoutWindow(p.Extension))
}

// The uninstaller's window says the program's own code will run, and runs
// it around the removal, with what the receipt kept.
func TestTheUninstallersWindowRunsTheProgramsOwnHooks(t *testing.T) {
	m, p, e, home, ran := pagedFixture(t)
	before := snap(t, e, home)
	plan, err := engine.NewPlan(m, engine.Options{Scope: "user", Env: e.Getenv, Uninstaller: p.Uninstaller,
		Params: map[string]string{"server": "https://keyed.invalid", "token": ""}})
	require.NoError(t, err)
	r, err := engine.Apply(context.Background(), plan, p.Files, p.Uninstaller, func(engine.Event) {})
	require.NoError(t, err)

	o, done := uninstallConfirm(r, false, e.Getenv, p.Extension)
	defer done()
	require.Contains(t, o.Detail, "Before removing anything, it runs Hello's own clean-up code.")
	require.NoError(t, o.Job(context.Background()))
	require.Equal(t, []string{
		"before-uninstall server=https://keyed.invalid root=io.example.hello",
		"after-uninstall server=https://keyed.invalid root=io.example.hello",
	}, *ran)
	require.Equal(t, before, snap(t, e, home), "the home is as it was")
}

/*
Spec 005. A launcher entry with autostart starts at login: on Windows a
shortcut in the Startup folder beside the Start Menu's, on Linux an entry in
the XDG autostart directory. The summary says so, and the uninstaller
removes it with the rest.
*/
func TestAnEntryThatStartsAtLoginIsInThePlaceTheDesktopLooks(t *testing.T) {
	m, p, e, home := guiFixture(t)
	m.Parameters, m.ConfigFiles, m.Licence = nil, nil, ""
	m.Desktop[0].Autostart = true
	before := snap(t, e, home)
	plan, err := engine.NewPlan(m, engine.Options{Scope: "user", Env: e.Getenv, Uninstaller: p.Uninstaller})
	require.NoError(t, err)
	require.Equal(t, []string{"Hello"}, plan.Autostart)
	r, err := engine.Apply(context.Background(), plan, p.Files, p.Uninstaller, func(engine.Event) {})
	require.NoError(t, err)

	v, err := platform.Vars("user", e.Getenv)
	require.NoError(t, err)
	if platform.Integration == platform.WindowsShell {
		_, err = os.Stat(filepath.Join(platform.StartupDir(v), "Hello.lnk"))
		require.NoError(t, err, "the Startup folder has the shortcut")
		_, err = os.Stat(filepath.Join(platform.StartMenu(v), "Hello.lnk"))
		require.NoError(t, err, "and the Start Menu still has its own")
	} else {
		b, err := os.ReadFile(filepath.Join(filepath.FromSlash(platform.AutostartDir(v)), "io.example.hello.desktop"))
		require.NoError(t, err, "the autostart directory has the entry")
		require.Contains(t, string(b), "Exec="+filepath.Join(at(t, e, "programs", "io.example.hello"), "bin", "hello")+"\n")
		require.NotContains(t, string(b), "Actions=", "the entry that starts at login has no Uninstall action")
	}

	_, err = engine.Uninstall(r, engine.ReasonUninstall, func(engine.Event) {})
	require.NoError(t, err)
	require.Equal(t, before, snap(t, e, home), "the uninstaller removes it with the rest")
}
