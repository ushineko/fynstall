//go:build !nogui

/*
Package gui is the window side of an extension's pages (spec 003). It
imports Fyne and the fynedesygn wizard, so it is built only into the full
installer: a program registers its pages' windows from a file of its
extension that carries //go:build !nogui.

	func init() {
		gui.Page("sign-in", func(c *extension.Context) wizard.Page {
			return &signInPage{c: c}
		})
	}

The page is a wizard.Page like fynstall's own. It gives its parameters
their values with Context.SetParam, and calls Wizard.Revalidate when what
it holds changes.
*/
package gui

import (
	"github.com/ushineko/fynedesygn/wizard"

	"github.com/ushineko/fynstall/extension"
)

// Builder makes a page's window side from the install's context.
type Builder func(c *extension.Context) wizard.Page

var pages = map[string]Builder{} //nolint:gochecknoglobals // the registry init() functions fill

// Page gives the extension page with this ID its wizard page. It is called
// from init(), before the installer starts.
func Page(id string, build Builder) { pages[id] = build }

// Lookup is the builder registered for a page. It is for package installer.
func Lookup(id string) (Builder, bool) {
	b, ok := pages[id]
	return b, ok
}
