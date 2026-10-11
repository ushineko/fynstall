package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/extension"
	"github.com/ushineko/fynstall/manifest"
)

/*
A program's own installer code (spec 003) is linked into its installer and
its uninstaller, and both front ends run it the same way: its pages before
the plan is made, its hooks around the changes. This file is what the two
front ends share.
*/

// declared are the names of m's parameters.
func declared(m *manifest.Manifest) []string {
	out := make([]string, 0, len(m.Parameters))
	for _, p := range m.Parameters {
		out = append(out, p.Name)
	}
	return out
}

// installContext is the Context of an install of m: params holds the
// values, root returns the directory once it is chosen.
func installContext(m *manifest.Manifest, scope, root func() string, params map[string]string, log func(extension.Level, string)) *extension.Context {
	return extension.NewContext(extension.Host{
		App:   extension.App{ID: m.App.ID, Name: m.App.Name, Version: m.App.Version},
		Scope: scope, Root: root, Params: params, Declared: declared(m), Log: log,
	})
}

// fixed is a value that does not change, for a Context made once the
// choice has been made.
func fixed(s string) func() string { return func() string { return s } }

// uninstallContext is the Context of an uninstall of the install r. It has
// the parameters the receipt kept, which are the ones that are not secret.
func uninstallContext(r *engine.Receipt, why engine.Reason, log func(extension.Level, string)) *extension.Context {
	params := map[string]string{}
	var names []string
	for k, v := range r.Parameters {
		params[k] = v
		names = append(names, k)
	}
	return extension.NewContext(extension.Host{
		App:   extension.App{ID: r.App.ID, Name: r.App.Name, Version: r.App.Version},
		Scope: fixed(r.Scope), Root: fixed(r.Root), Params: params, Declared: names, Log: log,
		Upgrade: why == engine.ReasonUpgrade,
	})
}

// eventLog sends an extension's log lines to report, as the engine's own
// details and warnings go.
func eventLog(report engine.Reporter) func(extension.Level, string) {
	return func(l extension.Level, text string) {
		kind := engine.Detail
		if l == extension.Warn {
			kind = engine.Warn
		}
		report(engine.Event{Kind: kind, Text: text, Step: -1})
	}
}

// terminalLog prints an extension's log lines. Unlike a file's path they
// are shown without --verbose: a hook says only what its author chose to.
func terminalLog(e Env) func(extension.Level, string) {
	return func(l extension.Level, text string) {
		if l == extension.Warn {
			_, _ = fmt.Fprintf(e.Err, "warning: %s\n", text)
			return
		}
		_, _ = fmt.Fprintf(e.Out, "    %s\n", text)
	}
}

// installHookLines say which of ext's hooks an install runs, one line
// each, for the plan a person approves (spec 003 R7).
func installHookLines(ext *extension.Extension, app string) []string {
	var out []string
	if ext == nil {
		return nil
	}
	if ext.BeforeInstall != nil {
		out = append(out, "before the install, runs "+app+"'s own set-up code")
	}
	if ext.AfterInstall != nil {
		out = append(out, "after the install, runs "+app+"'s own set-up code")
	}
	return out
}

// uninstallHookLines say which of ext's hooks an uninstall runs.
func uninstallHookLines(ext *extension.Extension, app string) []string {
	var out []string
	if ext == nil {
		return nil
	}
	if ext.BeforeUninstall != nil {
		out = append(out, "Before removing anything, it runs "+app+"'s own clean-up code.")
	}
	if ext.AfterUninstall != nil {
		out = append(out, "After removing it, it runs "+app+"'s own clean-up code.")
	}
	return out
}

// errNothingChanged marks a failure of a Before* hook: fynstall changed
// nothing.
var errNothingChanged = errors.New("nothing was changed")

// beforeInstall runs ext's BeforeInstall. Its error stops the install
// before any change.
func beforeInstall(ctx context.Context, ext *extension.Extension, c *extension.Context) error {
	if ext == nil || ext.BeforeInstall == nil {
		return nil
	}
	if err := ext.BeforeInstall(ctx, c); err != nil {
		return fmt.Errorf("%s's own set-up code stopped the install, and %w: %w", c.App().Name, errNothingChanged, err)
	}
	return nil
}

// afterInstall runs ext's AfterInstall. The program is installed by then,
// and its error says so.
func afterInstall(ctx context.Context, ext *extension.Extension, c *extension.Context) error {
	if ext == nil || ext.AfterInstall == nil {
		return nil
	}
	if err := ext.AfterInstall(ctx, c); err != nil {
		return fmt.Errorf("%s %s is installed, but its own set-up code failed: %w", c.App().Name, c.App().Version, err)
	}
	return nil
}

// beforeUninstall runs ext's BeforeUninstall. Its error stops the
// uninstall before any file is removed.
func beforeUninstall(ctx context.Context, ext *extension.Extension, c *extension.Context) error {
	if ext == nil || ext.BeforeUninstall == nil {
		return nil
	}
	if err := ext.BeforeUninstall(ctx, c); err != nil {
		return fmt.Errorf("%s's own clean-up code stopped the uninstall, and nothing was removed: %w", c.App().Name, err)
	}
	return nil
}

// afterUninstall runs ext's AfterUninstall. The program is removed by then.
func afterUninstall(ctx context.Context, ext *extension.Extension, c *extension.Context) error {
	if ext == nil || ext.AfterUninstall == nil {
		return nil
	}
	if err := ext.AfterUninstall(ctx, c); err != nil {
		return fmt.Errorf("%s %s was removed, but its own clean-up code failed: %w", c.App().Name, c.App().Version, err)
	}
	return nil
}

// orderedPages are ext's pages in the order the wizard shows them, which is
// the order the command line passes them in.
func orderedPages(ext *extension.Extension) []extension.Page {
	if ext == nil {
		return nil
	}
	pages := slices.Clone(ext.Pages)
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].At.Rank() < pages[j].At.Rank() })
	return pages
}

// prompter is how an extension's page asks in a terminal.
type prompter struct {
	a *asker
}

func (p prompter) Ask(label, def string) (string, error) {
	prompt := label
	if def != "" {
		prompt += " [" + def + "]"
	}
	s, err := p.a.line(prompt + ": ")
	if err != nil {
		return "", err
	}
	if s == "" {
		s = def
	}
	return s, nil
}

func (p prompter) AskSecret(label string) (string, error) {
	if f, ok := p.a.e.In.(*os.File); ok && p.a.e.Interactive {
		_, _ = fmt.Fprint(p.a.e.Out, label+": ")
		v, err := readSecret(f)
		_, _ = fmt.Fprintln(p.a.e.Out)
		return v, err
	}
	return p.a.line(label + ": ")
}

func (p prompter) Confirm(question string) (bool, error) { return p.a.yes(question) }

func (p prompter) Say(text string) { _, _ = fmt.Fprintln(p.a.e.Out, text) }

/*
cliPages passes ext's pages on the command line (spec 003, the CLI rule). A
page whose parameters were all given, by a flag, the side file or the
version being replaced, is passed over. Otherwise its CLI function asks,
when ask is not nil. When the installer may not ask, each parameter keeps
its default, and one that is required and has none stops the install with
the flags that would answer the page.
*/
func cliPages(ctx context.Context, ext *extension.Extension, m *manifest.Manifest, c *extension.Context, given map[string]bool, ask *asker) error {
	for _, page := range orderedPages(ext) {
		all := true
		for _, s := range page.Sets {
			all = all && given[s]
		}
		if all && len(page.Sets) > 0 {
			continue
		}
		if ask != nil {
			if err := page.CLI(ctx, c, prompter{a: ask}); err != nil {
				return fmt.Errorf("page %s: %w", page.ID, err)
			}
			continue
		}
		if missing := unanswered(m, c, page.Sets, given); len(missing) > 0 {
			return fmt.Errorf("this install cannot ask, and its %s page needs an answer: pass %s", page.ID, strings.Join(missing, ", "))
		}
	}
	return nil
}

// unanswered are the flags of the parameters among names that are required
// and have no value.
func unanswered(m *manifest.Manifest, c *extension.Context, names []string, given map[string]bool) []string {
	var out []string
	for _, p := range m.Parameters {
		if slices.Contains(names, p.Name) && p.Required && !given[p.Name] && c.Param(p.Name) == "" {
			out = append(out, "--"+p.Name+"=…")
		}
	}
	return out
}

// requiredAfterPages checks the required parameters the pages were to set,
// once they have run.
func requiredAfterPages(m *manifest.Manifest, ext *extension.Extension, params map[string]string) error {
	paged := ext.Sets()
	for _, p := range m.Parameters {
		if slices.Contains(paged, p.Name) && p.Required && params[p.Name] == "" {
			return fmt.Errorf("%s is required: pass --%s=…, or set %s in %s", label(p), p.Name, p.Name, SideFile)
		}
	}
	return nil
}

// checkExtension is --check-extension, which the builder runs once on the
// installer it has just built: the extension keeps the rules, and in a full
// build every page has its window (spec 003 R4).
func checkExtension(m *manifest.Manifest, ext *extension.Extension, e Env) int {
	err := ext.Check(declared(m))
	if err == nil && guiAvailable {
		if missing := pagesWithoutWindow(ext); len(missing) > 0 {
			err = fmt.Errorf("no window is registered for the page %s: call gui.Page for it from a file built with //go:build !nogui",
				strings.Join(missing, ", "))
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(e.Err, "extension: %v\n", err)
		return exitFail
	}
	return exitOK
}
