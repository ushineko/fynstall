# Spec 003: a program's own pages and hooks

**Issue**: [#11](https://github.com/ushineko/fynstall/issues/11)

## Status: INCOMPLETE

The design was agreed on 2026-10-09. Implementation follows spec 002
phase 3 and spec 001 phase 5, unless it is brought forward.

## Executive Summary

Populated before the first PR is opened.

## Context

An installer framework cannot know everything a program needs at install
time. Some needs seen so far:

- a licence fetched over the network, or a licensing scheme the program
  has itself;
- a sign-in, an authentication or another set-up step before the program
  can run;
- clean-up of what the program created while it ran, such as a service it
  registered itself.

Spec 002 covers what can be declared: parameters (D3), configuration
files, services and `run` actions, including uninstall hooks (D2a). Those
work for a program in any language. A custom wizard page with its own
behaviour cannot be declared in YAML without inventing a weaker programming
language.

fynstall is built for Go programs, and every installer it makes is
compiled from generated Go code by the Go toolchain. So a program can bring
Go code of its own. The config names a Go package in the program's
repository, and the builder links it into the installer and the
uninstaller. That code can add wizard pages and hook the install's phases,
in process, with the type checking and the libraries the program already
uses.

## The rules

These come before the API, because the API exists to keep them.

- **Every page has a command-line path.** A page names the parameters it
  sets. On the command line, a page whose parameters all have values (from
  a flag or `fynstall-params.yml`) is skipped. Otherwise its CLI function
  asks, in a terminal, or the install stops and names the flags that would
  answer it. A silent install (`--yes`) never meets a page it cannot pass.
- **An extension builds without the GUI.** A `--cli-only` installer links
  no Fyne (spec 001, R12). An extension's core package must build under the
  `nogui` tag, and its wizard pages live in files with `//go:build !nogui`.
  The builder builds both variants and names the file that breaks either.
- **The uninstaller runs the same extension.** It is linked into the
  uninstaller too, so an uninstall hook runs from the installed copy, the
  version that was installed (spec 001, R9e).
- **One config, every platform.** An extension builds for every target the
  config names, with its own build tags where it must (spec 002).
- **MSI, not NSIS.** A hook is code the engine cannot see into, so it is not
  journalled. The summary page and `--dry-run` list every hook that will
  run. A hook that changes the system is expected to undo it in the
  matching uninstall hook, and the docs say so plainly.

## Requirements

- R1 **Config.** `extension: ./installer` names a Go package directory,
  relative to `fynstall.yaml`, inside a Go module. The builder finds the
  module root and path (`go.mod`), adds the module to the generated
  `go.mod` with a `replace` to its directory, merges its `go.sum`, and
  passes the extension to `installer.Main` and `installer.UninstallMain`.
- R2 **Version agreement.** The extension imports
  `github.com/ushineko/fynstall/extension`. The fynstall version that the
  generated build selects must be the builder's runtime version (spec 001,
  R5); a mismatch stops the build and names both versions.
- R3 **The core API**, in package `extension`, with no Fyne import:

  ```go
  // Extension is what a program's installer package exports, as a variable
  // named Extension.
  type Extension struct {
      // Pages are added among the standard wizard pages.
      Pages []Page
      // Licence, when set, supplies the licence text in place of
      // app.licence: fetched over the network, or generated.
      Licence func(ctx context.Context, c *Context) (string, error)
      // Hooks. Before* runs before the engine changes anything; After*
      // runs once it has finished. A Before* error stops the install or
      // the uninstall with nothing changed by fynstall.
      BeforeInstall, AfterInstall     func(ctx context.Context, c *Context) error
      BeforeUninstall, AfterUninstall func(ctx context.Context, c *Context) error
  }

  type Page struct {
      ID string
      // At places the page: After("licence"), Before("location"). The
      // standard pages are welcome, licence, settings, location, summary,
      // installing and finish.
      At Position
      // Sets are the parameters the page gives values to. They must be
      // declared in fynstall.yaml.
      Sets []string
      // CLI asks for the page's values in a terminal.
      CLI func(ctx context.Context, c *Context, p Prompter) error
  }

  // Context is what an extension sees: the app, the scope, the install
  // directory once it is chosen, and the parameters.
  type Context struct{ /* … */ }
  func (c *Context) Param(name string) string
  func (c *Context) SetParam(name, value string) error // declared parameters only
  func (c *Context) Root() string                      // "" before the location page
  func (c *Context) Log(level Level, text string)      // the log pane, or the CLI

  // Prompter is how a CLI path asks. A secret is read without echo.
  type Prompter interface {
      Ask(label, def string) (string, error)
      AskSecret(label string) (string, error)
      Confirm(question string) (bool, error)
  }
  ```

- R4 **The GUI side**, in package `extension/gui`, which imports Fyne and
  the fynedesygn `wizard` and is only ever built with `!nogui`:

  ```go
  // Page gives the page with this ID its wizard page. Called from init()
  // in a file of the program's extension with //go:build !nogui.
  func Page(id string, build func(c *extension.Context) wizard.Page)
  ```

  A full build checks, when the installer starts with an internal flag
  that the builder passes once, that every page has a GUI; a page without
  one stops the build.
- R5 **Secrets.** `SetParam` on a secret parameter keeps the value out of
  every log, the receipt and the plan output, as in spec 002 D3a. An
  uninstall hook does not see secret values: they are not recorded.
- R6 **Where code runs.** Pages and `Licence` run in the front end's
  process, as the user. Install hooks run in the process that applies the
  plan: the privileged helper for a system install (spec 001, R13).
- R7 **Shown first.** The summary page and `--dry-run` list the extension's
  hooks by name ("before install: hello's own set-up").
- R8 **Docs and example.** `docs/extensions.md` with the rules above;
  `examples/hello` gets an extension with a licence from a URL and a
  sign-in page (a fake service in the tests).

## Phases

1. **Linking, the licence and the hooks.** R1, R2, R3 without pages, R5,
   R6 for user scope, R7. Example: `greet` gets an extension whose `Licence`
   reads from an HTTP server, and whose hooks write and remove a marker
   file. Desk check: the CLI installer shows the fetched licence, and the
   uninstall hook runs from the installed uninstaller.
2. **Pages.** R3 pages, R4, the CLI rule, the nogui rule. Example: a
   sign-in page on Hello that sets a token parameter. Desk check: the page
   in the wizard, the same values by prompt in a terminal, and a silent
   install with `--token=…`.

## Acceptance Criteria

Phase 1:

- [ ] R1 A config with `extension:` builds both variants; the generated
      `go.mod` has the module and its `replace`; a missing package or
      module is a config error with its line.
- [ ] R2 An extension that requires another fynstall version stops the
      build with both versions named.
- [ ] R3 `Licence` replaces `app.licence` in the wizard and the CLI; its
      error stops the install with the error shown.
- [ ] R3/R6 `BeforeInstall` runs before the first change and its error
      leaves the home unchanged; `AfterInstall` runs after the receipt is
      written; `BeforeUninstall` runs from the *installed* uninstaller
      before any file is removed, and its error stops the uninstall.
- [ ] R5 A secret set by an extension appears in no output and no file
      but its config file (the leak test of spec 002, extended).
- [ ] R7 `--dry-run` lists the hooks.
- [ ] Desk check as in Phases.

Phase 2:

- [ ] R3 A page placed `After("licence")` appears there in the wizard and
      is passed on the command line in the same order.
- [ ] The CLI rule: a page whose parameters are all given is skipped; a
      silent install missing one stops and names the flag.
- [ ] The nogui rule: an extension whose core package imports Fyne fails
      the `--cli-only` build, naming the file.
- [ ] R4 A page with no GUI fails the full build, naming the page.
- [ ] Desk check as in Phases.

## Risks & Assumptions

- **A public API.** Extension authors depend on `extension` and
  `extension/gui`. They start at v0, and the changelog names every change.
  An installed uninstaller always runs the extension it was built with, so
  an API change never reaches an existing install.
- **Dependencies.** An extension's dependencies are linked into both
  programs. Merging `go.sum` is the most fragile part of the builder work;
  a build that needs the network says so.
- **Network at install time.** A licence or a sign-in over the network
  fails offline. The CLI rule gives a way around it (the parameters), and
  the docs say to offer one.
- **Hooks are not journalled.** The engine cannot undo what a hook did. A
  helper such as `c.Record(name, undo)` could journal a hook's own changes
  later; it is not in this spec.
- **Privileged code.** In a system install, install hooks run as root. It
  is the program author's own code, as the payload is, but it is code that
  runs with more rights than the program itself may.
- **Rollback.** Additive. A config without `extension:` builds as it does
  now.

## Alternatives Considered

- Considered Go plugins (`plugin` package, `.so` files). Rejected: they need
  cgo, an exact toolchain match and do not exist on Windows.
- Considered an embedded scripting language (Starlark, Lua). Rejected: a
  second language for authors whose program is Go, weaker than Go, and the
  sandboxing it offers is not needed for the author's own code.
- Considered pages declared in YAML. Rejected as the main way: it becomes a
  small, weak programming language. Parameters already cover the simple
  form case.
- Considered hook programs (a payload executable with a JSON protocol) as
  the main way. They are the way for programs not written in Go, through
  spec 002's `run` actions; for Go programs, linked code is simpler and
  stronger.

## Verification

Filled in as each phase lands.
