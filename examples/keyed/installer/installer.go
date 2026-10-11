/*
Package installer is Keyed's own installer code (spec 003): a sign-in page,
and a hook at each of the four points fynstall offers.

This file builds without the wizard. The page's window is in window.go,
which carries //go:build !nogui, so a --cli-only installer links this
package without Fyne.
*/
package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ushineko/fynstall/extension"
)

// Extension is what fynstall links: the name is fixed.
var Extension = extension.Extension{ //nolint:gochecknoglobals // the variable fynstall links
	Pages: []extension.Page{{
		ID:   "sign-in",
		At:   extension.After(extension.PageWelcome),
		Sets: []string{"account"},
		CLI:  signInCLI,
	}},
	BeforeInstall:   mark("before-install"),
	AfterInstall:    mark("after-install"),
	BeforeUninstall: mark("before-uninstall"),
	AfterUninstall:  mark("after-uninstall"),
}

// CheckAccount is the page's rule, shared by the window and the terminal.
func CheckAccount(account string) error {
	if strings.TrimSpace(account) == "" {
		return errors.New("an account is needed")
	}
	if strings.ContainsAny(account, " \t") {
		return errors.New("an account has no spaces")
	}
	return nil
}

// signInCLI is the page on the command line: it asks until the answer is
// an account.
func signInCLI(_ context.Context, c *extension.Context, p extension.Prompter) error {
	p.Say("Keyed signs in before it installs.")
	for {
		account, err := p.Ask("Account", c.Param("account"))
		if err != nil {
			return fmt.Errorf("read the account: %w", err)
		}
		if err := CheckAccount(account); err != nil {
			p.Say(err.Error() + ".")
			continue
		}
		return c.SetParam("account", account) //nolint:wrapcheck // names the parameter
	}
}

/*
mark is a hook that leaves a line in <marker-dir>/hooks.log, when the
marker-dir parameter names a directory, and says so in the installer's log.
BeforeInstall refuses the account "refused", so a test can see an install
stopped with nothing changed.
*/
func mark(name string) func(context.Context, *extension.Context) error {
	return func(_ context.Context, c *extension.Context) error {
		if name == "before-install" && c.Param("account") == "refused" {
			return errors.New("the account refused is not allowed in")
		}
		c.Logf(extension.Info, "Keyed's %s hook ran for %s", name, c.Param("account"))
		dir := c.Param("marker-dir")
		if dir == "" {
			return nil
		}
		f, err := os.OpenFile(filepath.Join(dir, "hooks.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- the directory the person named
		if err != nil {
			return fmt.Errorf("mark %s: %w", name, err)
		}
		defer func() { _ = f.Close() }()
		line := fmt.Sprintf("%s account=%s root=%s scope=%s upgrade=%v\n", name, c.Param("account"), filepath.Base(c.Root()), c.Scope(), c.Upgrade())
		if _, err := f.WriteString(line); err != nil {
			return fmt.Errorf("mark %s: %w", name, err)
		}
		return nil
	}
}
