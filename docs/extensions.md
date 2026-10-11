# Extensions

An extension is Go code of the program's own that fynstall links into the
program's installer and uninstaller. It adds pages to the wizard, and it
runs at four fixed points: before and after an install, and before and
after an uninstall.

Use one when what the installer must do cannot be declared in
`fynstall.yaml`: a sign-in, a check of something outside the program, an
offer to set up a second program. What can be declared (parameters,
configuration files, services, `run` actions) needs no extension, and works
for a program in any language.

## Contents

- [The package](#the-package)
- [The rules](#the-rules)
- [Pages](#pages)
- [Hooks](#hooks)
- [What an extension sees](#what-an-extension-sees)
- [Building](#building)

## The package

`fynstall.yaml` names a directory that holds a Go package, inside the
program's own Go module:

```yaml
extension: ./installer
```

The package exports a variable named `Extension`:

```go
package installer

import "github.com/ushineko/fynstall/extension"

var Extension = extension.Extension{
	Pages:         []extension.Page{ /* … */ },
	BeforeInstall: checkTheLicence,
	AfterInstall:  setUpTheSensors,
}
```

`examples/keyed` is a complete one: a sign-in page and a hook at each point.

## The rules

The builder and the installer check each of these. A build that breaks one
stops and says which.

**Every page has a command-line path.** A page names the parameters it
sets, and each is declared under `parameters` in `fynstall.yaml`. A page has
a `CLI` function that asks for them in a terminal.

**An extension builds without the wizard.** A `--cli-only` installer links
no Fyne. The package must build with `-tags nogui` and without cgo, so a
page's window goes in a file that starts with `//go:build !nogui`.

**The uninstaller runs the same extension.** It is linked into the
uninstaller too. An uninstall hook runs from the installed uninstaller,
which is the version that was installed.

**One config for every platform.** The package builds for every target the
config names. Code for one platform goes in a file with that platform's
build tag, with a file for the others beside it.

**A hook is not recorded.** fynstall cannot see what a hook changes, so the
uninstaller does not undo it. A hook that changes the system undoes that
change in the matching uninstall hook. The installer says that the
program's own code will run, in `--dry-run`, before its question and on the
summary page.

## Pages

```go
extension.Page{
	ID:   "sign-in",
	At:   extension.After(extension.PageWelcome),
	Sets: []string{"account"},
	CLI:  signInCLI,
}
```

| Field | Meaning |
|---|---|
| `ID` | The page's name: lowercase letters, digits and dashes. |
| `At` | Where the page goes: `After(page)` or `Before(page)`, beside one of the wizard's own pages. Without it the page goes before the summary. |
| `Sets` | The parameters the page gives values to. A parameter is set by one page. |
| `CLI` | The page on the command line. |

The wizard's own pages are `welcome`, `licence`, `settings`, `location`,
`summary`, `installing` and `finish`. A page goes after the welcome and
before the summary, because the summary makes the plan from what the pages
set. A page placed beside a wizard page that is not shown goes where that
page would be: `After(extension.PageLicence)` works for a config with no
licence.

A parameter that a page sets has no field on the Settings page.

**The window.** A file with `//go:build !nogui` registers the page's
window by its ID:

```go
//go:build !nogui

func init() {
	gui.Page("sign-in", func(c *extension.Context) wizard.Page {
		return &signInPage{c: c}
	})
}
```

The page is a `wizard.Page` of fynedesygn, like fynstall's own. It gives
its parameters their values with `c.SetParam`, and calls
`Wizard.Revalidate` when its state changes. Next waits while its `Valid`
method returns false. Build the widgets in `Build`, not in the constructor.

**The command line.** The installer passes each page in the wizard's order:

| The installer | What happens at the page |
|---|---|
| has a value for every parameter the page sets (a flag, `fynstall-params.yml`, or the version it replaces) | The page is passed over. |
| runs in a terminal, without `--yes` | `CLI` runs. It asks through the `Prompter` and sets the values. |
| may not ask (`--yes`, `--dry-run`, no terminal) | Each parameter keeps its default. A required parameter with no value stops the install, and the message names its flag. |

So a silent install never stops at a page: give the flag, or give the
parameter a default.

## Hooks

| Hook | When it runs | An error from it |
|---|---|---|
| `BeforeInstall` | Before fynstall changes anything. For an upgrade, before the old version is removed. | Stops the install. Nothing is changed. |
| `AfterInstall` | After the program is installed and recorded. | The program stays installed. The installer reports the error and exits with a failure. |
| `BeforeUninstall` | Before the uninstaller removes anything. | Stops the uninstall. Nothing is removed. |
| `AfterUninstall` | After the program is removed. | The uninstaller reports the error. |

Return an error only for a failure the person must act on. For a step that
is optional, write a line with `c.Log(extension.Warn, …)` and return nil.

**Where a hook runs.** An install hook runs in the process that makes the
changes. For a per-user install that is the person's own process. For an
install for everyone it is the administrator's helper, so the hook has an
administrator's rights and no window.

**An upgrade runs both.** An upgrade removes the installed version with its
own uninstaller, then installs. The old version's uninstall hooks run, with
`c.Upgrade()` true, and then the new version's install hooks.

## What an extension sees

A hook and a page get a `Context`:

| Method | Returns |
|---|---|
| `App()` | The program's ID, name and version. |
| `Scope()` | `user` or `system`. |
| `Root()` | The install directory, or `""` before it is chosen. A hook always has it. |
| `Param(name)` | A parameter's value. |
| `SetParam(name, value)` | Sets a declared parameter. Any other name is an error. |
| `Upgrade()` | True in an uninstall that an upgrade runs. |
| `Log(level, text)` | Writes a line to the wizard's log, or to the terminal. |

An uninstall hook has the parameters the install record keeps. A secret
parameter is not recorded, so an uninstall hook does not have it.

## Building

`fynstall build` does the rest. It finds the module the package is in,
requires it in the generated program with a `replace` to its directory,
and adds the module's `go.sum`.

**The fynstall versions must match.** The program's `go.mod` requires
`github.com/ushineko/fynstall` at the version of the builder. An installer
has one fynstall in it. The build stops when the two differ, and names
both. A `go.mod` that replaces fynstall with a directory, and a build with
`--runtime-path`, are not held to this.

**A `replace` in the program's `go.mod` is not used.** Go reads `replace`
only from the module that is being built, and that is the generated
program. A dependency that the program replaces must be a real version for
the installer's build.

After the build, the builder runs the new installer once with
`--check-extension`. The installer checks the rules above against its own
config, and a full build checks that every page has a window.
