/*
Package extension is what a program's own installer code is written
against: Go that fynstall links into the program's installer and
uninstaller, to add wizard pages and to run at fixed points of an install
and an uninstall (spec 003).

A program names a package of its own in fynstall.yaml:

	extension: ./installer

That package exports a variable named Extension, of the type below. This
package imports no UI: the page's window side is registered separately, in
package extension/gui, from a file built only with the wizard (//go:build
!nogui), so the same extension links into the command-line-only installer.
*/
package extension

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

/*
Extension is a program's installer code.

Before* runs before fynstall changes anything, and an error from it stops
the install or the uninstall with nothing changed. After* runs once fynstall
has finished: the program is installed and recorded, or removed.

A hook is code fynstall cannot see into, so what it changes is not recorded
and not undone. A hook that changes the system undoes it in the matching
uninstall hook. The installer says, before it starts, that a hook will run.

An install hook runs in the process that makes the changes: the person's
own for a per-user install, the administrator's helper for an install for
everyone.
*/
type Extension struct {
	// Pages are added among the wizard's own pages.
	Pages []Page

	BeforeInstall, AfterInstall     func(ctx context.Context, c *Context) error
	BeforeUninstall, AfterUninstall func(ctx context.Context, c *Context) error
}

// The wizard's own pages, as a Position names them.
const (
	PageWelcome    = "welcome"
	PageLicence    = "licence"
	PageSettings   = "settings"
	PageLocation   = "location"
	PageSummary    = "summary"
	PageInstalling = "installing"
	PageFinish     = "finish"
)

// standard is the order of the wizard's own pages.
var standard = []string{PageWelcome, PageLicence, PageSettings, PageLocation, PageSummary, PageInstalling, PageFinish} //nolint:gochecknoglobals // a fixed table

// Position places a page beside one of the wizard's own.
type Position struct {
	// Page is one of the wizard's own pages.
	Page string
	// Before is true for the place before Page; otherwise the page goes
	// after it.
	Before bool
}

// problem says why a page cannot go at a, or "". A page gives parameters
// their values, and the summary makes the plan from them: so a page goes
// after the welcome and before the summary.
func (a Position) problem() string {
	if a.Page == "" {
		return ""
	}
	i := slices.Index(standard, a.Page)
	last := slices.Index(standard, PageSummary)
	switch {
	case i < 0:
		return fmt.Sprintf("%q is not a page of the wizard (%v)", a.Page, standard)
	case a.Before && i == 0:
		return "a page cannot go before the welcome"
	case i > last || (i == last && !a.Before):
		return "a page goes before the summary, which makes the plan from what the pages set"
	}
	return ""
}

// After is the place after one of the wizard's own pages.
func After(page string) Position { return Position{Page: page} }

// Before is the place before one of the wizard's own pages.
func Before(page string) Position { return Position{Page: page, Before: true} }

/*
Page is a page a program adds to its installer.

Every page has a command-line path. Sets names the parameters the page gives
values to, each declared in fynstall.yaml. On the command line a page whose
parameters all have values already, from a flag or fynstall-params.yml or
the version being replaced, is passed over. Otherwise CLI asks in the
terminal. An install that may not ask (--yes) takes each parameter's
default, and stops, naming the flags, when one has none.

The page's window is registered by ID with extension/gui.Page.
*/
type Page struct {
	// ID names the page: lowercase letters, digits and dashes.
	ID string
	// At places the page. The zero value is before the summary.
	At Position
	// Sets are the parameters the page gives values to.
	Sets []string
	// CLI asks for the page's values in a terminal, and sets them with
	// Context.SetParam.
	CLI func(ctx context.Context, c *Context, p Prompter) error
}

// Prompter is how a page asks on the command line.
type Prompter interface {
	// Ask reads one line; an empty answer is def.
	Ask(label, def string) (string, error)
	// AskSecret reads one line without showing it.
	AskSecret(label string) (string, error)
	// Confirm asks a yes-or-no question whose default is no.
	Confirm(question string) (bool, error)
	// Say prints a line: what the page found, before it asks.
	Say(text string)
}

// Level is how much a logged line matters.
type Level int

// The levels of Context.Log.
const (
	Info Level = iota
	Warn
)

// App is the program being installed.
type App struct{ ID, Name, Version string }

/*
Context is what an extension sees of an install or an uninstall: the
program, who it is for, where it goes once that is chosen, and the
parameters.
*/
type Context struct{ h Host }

/*
Host is what a fynstall front end gives a Context. It is for package
installer; an extension never makes one.
*/
type Host struct {
	App App
	// Scope returns who the install is for. The wizard's own page can
	// change it after an extension's page has been shown.
	Scope func() string
	// Root returns the install directory, or "" before it is chosen.
	Root func() string
	// Params holds the values; SetParam writes to it.
	Params map[string]string
	// Declared are the parameters of fynstall.yaml, and Secret those whose
	// values are never shown.
	Declared []string
	// Log receives Context.Log.
	Log func(Level, string)
	// Upgrade is true in an uninstall that an upgrade runs.
	Upgrade bool
}

// NewContext is a Context over h. It is for package installer.
func NewContext(h Host) *Context {
	if h.Params == nil {
		h.Params = map[string]string{}
	}
	return &Context{h: h}
}

// App is the program being installed or removed.
func (c *Context) App() App { return c.h.App }

// Scope is "user" for an install for one person, "system" for everyone.
func (c *Context) Scope() string {
	if c.h.Scope == nil {
		return ""
	}
	return c.h.Scope()
}

// Root is the install directory, or "" before the person has chosen it.
func (c *Context) Root() string {
	if c.h.Root == nil {
		return ""
	}
	return c.h.Root()
}

// Param is the value of a parameter, or "" when it has none.
func (c *Context) Param(name string) string { return c.h.Params[name] }

// SetParam gives a parameter of fynstall.yaml a value. A name that is not
// declared there is an error: an install uses only what its plan shows.
func (c *Context) SetParam(name, value string) error {
	if !slices.Contains(c.h.Declared, name) {
		return fmt.Errorf("%s is not a parameter of fynstall.yaml", name)
	}
	c.h.Params[name] = value
	return nil
}

// Upgrade reports whether this uninstall is the first half of an upgrade,
// a repair or a downgrade: the program is about to be installed again.
func (c *Context) Upgrade() bool { return c.h.Upgrade }

// Log writes a line to the installer's log: the log pane of the wizard, or
// the terminal.
func (c *Context) Log(level Level, text string) {
	if c.h.Log != nil {
		c.h.Log(level, text)
	}
}

// Logf is Log with a format.
func (c *Context) Logf(level Level, format string, a ...any) { c.Log(level, fmt.Sprintf(format, a...)) }

// HasInstallHooks reports whether e runs code during an install.
func (e *Extension) HasInstallHooks() bool {
	return e != nil && (e.BeforeInstall != nil || e.AfterInstall != nil)
}

// HasUninstallHooks reports whether e runs code during an uninstall.
func (e *Extension) HasUninstallHooks() bool {
	return e != nil && (e.BeforeUninstall != nil || e.AfterUninstall != nil)
}

// Sets are the parameters that e's pages give values to.
func (e *Extension) Sets() []string {
	if e == nil {
		return nil
	}
	var out []string
	for _, p := range e.Pages {
		out = append(out, p.Sets...)
	}
	return out
}

/*
Check holds e to the rules: every page has an ID of its own, a place beside
one of the wizard's pages, a command-line path, and sets only parameters
that fynstall.yaml declares, each by one page. An installer checks its
extension before it does anything else.
*/
func (e *Extension) Check(declared []string) error {
	if e == nil {
		return nil
	}
	var errs []error
	ids, owner := map[string]bool{}, map[string]string{}
	for i, p := range e.Pages {
		name := fmt.Sprintf("page %q", p.ID)
		switch {
		case !validID(p.ID):
			errs = append(errs, fmt.Errorf("page %d: the ID %q is not lowercase letters, digits and dashes", i+1, p.ID))
			continue
		case ids[p.ID]:
			errs = append(errs, fmt.Errorf("%s: two pages have this ID", name))
		}
		ids[p.ID] = true
		if msg := p.At.problem(); msg != "" {
			errs = append(errs, fmt.Errorf("%s: %s", name, msg))
		}
		if p.CLI == nil {
			errs = append(errs, fmt.Errorf("%s: it has no CLI function; every page has a command-line path", name))
		}
		for _, s := range p.Sets {
			if !slices.Contains(declared, s) {
				errs = append(errs, fmt.Errorf("%s: it sets %q, which is not a parameter of fynstall.yaml", name, s))
			}
			if other, taken := owner[s]; taken {
				errs = append(errs, fmt.Errorf("%s: it sets %q, which page %q sets too", name, s, other))
			}
			owner[s] = p.ID
		}
	}
	return errors.Join(errs...)
}

func validID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

/*
Rank orders a place among the wizard's own pages, whose ranks are 0, 1, 2 …
in the order the wizard shows them: a place before a page ranks a quarter
below it, a place after it a quarter above, so what goes after one page
comes ahead of what goes before the next. A page beside a wizard page that is not
shown (a config with no licence has no licence page) so goes where that
page would have been. The zero Position is the place before the summary.
*/
func (a Position) Rank() float64 {
	if a.Page == "" {
		a = Before(PageSummary)
	}
	r := Rank(a.Page)
	if a.Before {
		return r - 0.25
	}
	return r + 0.25
}

// Rank is the rank of one of the wizard's own pages, or of the summary for
// a name that is not one.
func Rank(page string) float64 {
	if i := slices.Index(standard, page); i >= 0 {
		return float64(i)
	}
	return float64(slices.Index(standard, PageSummary))
}
