//go:build !nogui

package installer

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	fd "github.com/ushineko/fynedesygn"
	"github.com/ushineko/fynedesygn/widgets"
	"github.com/ushineko/fynedesygn/wizard"

	"github.com/ushineko/fynstall/extension"
	"github.com/ushineko/fynstall/extension/gui"
)

func init() { //nolint:gochecknoinits // how a page's window is registered
	gui.Page("sign-in", func(c *extension.Context) wizard.Page { return &signInPage{c: c} })
}

// AccountPlaceholder is the entry's hint, by which a test finds it.
const AccountPlaceholder = "your account"

// signInPage is the sign-in page's window: one entry, held to
// CheckAccount, whose value is the account parameter.
type signInPage struct {
	c     *extension.Context
	entry *widget.Entry
}

func (p *signInPage) Title() string { return "Sign in" }

// Valid holds Next until the entry is an account.
func (p *signInPage) Valid() bool { return CheckAccount(p.c.Param("account")) == nil }

func (p *signInPage) Build(w *wizard.Wizard) fyne.CanvasObject {
	p.entry = widget.NewEntry()
	p.entry.SetPlaceHolder(AccountPlaceholder)
	p.entry.SetText(p.c.Param("account"))
	p.entry.OnChanged = func(s string) {
		_ = p.c.SetParam("account", s) // declared in fynstall.yaml
		if err := CheckAccount(s); err != nil && s != "" {
			w.SetMessage(err.Error(), fd.StatusBad)
		} else {
			w.SetMessage("", fd.StatusInfo)
		}
		w.Revalidate()
	}
	return container.NewVBox(widgets.Wrapped("Keyed signs in before it installs."), p.entry)
}
