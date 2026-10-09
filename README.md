# fynstall

**Version**: 0.1.0

An installer framework for Go and [Fyne](https://fyne.io) programs. You
describe a program in `fynstall.yaml`, and `fynstall build` produces one
installer binary per target platform. The installer holds the whole payload
through Go embedding, so the person who runs it needs no toolchain.

## Status

Early development. The plan is [spec 001](specs/001-installer-prototype.md),
delivered in phases; this README changes as each phase lands.

Linux installers, per-user, with a wizard and a command line, launcher
entries, icons, links on `PATH`, parameters and configuration files.
System-wide installs, upgrades and Windows follow.

## Using it

```bash
fynstall init                    # writes a commented fynstall.yaml
fynstall validate                # lists every problem, with line numbers
fynstall build                   # dist/<name>-<version>-<os>-<arch>-installer, with the wizard
fynstall build --with-cli-only   # and dist/…-cli-installer, without it
fynstall build --cli-only        # the CLI variant only, for any target
```

**The installer picks its front end.** Started from the desktop (no
terminal, a display), it opens the wizard. Started from a terminal, it runs
on the command line: it asks where to install, asks for its parameters, and
asks for confirmation. `--gui` and `--cli` override the choice. `--yes`
accepts the defaults, `--dry-run` lists every change and makes none, and
`--dir` chooses the directory.

**Two variants.** The full installer has the wizard, so it needs cgo to
build, builds only for the machine that builds it, and needs the graphics
libraries to start. On a machine without them, such as a server or a slim
container, it does not start at all: the dynamic loader stops before the
installer can say anything (`libGL.so.1: cannot open shared object file`).
Ship the `-cli-installer` for those machines. It has no Fyne and no cgo,
builds for every target, and runs anywhere.

**Progress.** The wizard shows a bar, the count of files and bytes, and the
file being written; on a terminal the command line keeps one progress line
up to date. An install of thousands of files reports each one without
slowing down.

**Removing it.** Run `<install directory>/uninstall`, or use the Uninstall
action of the launcher entry. From the desktop it asks once ("Uninstall
Hello 0.1.0?") and reports; `--yes` skips the question. On the command line
it does not ask: running it is the decision. A newer installer's
`--uninstall`, and the uninstaller beside the installer in `dist/`, hand
over to that same installed file.

Until fynstall has a release, build against a checkout:
`fynstall build --runtime-path <path to fynstall>`.

The config format is in [docs/config.md](docs/config.md).

## What it will do

- **One binary, two front ends.** The installer runs as a wizard when it is
  started from a desktop, and as a command-line program when it is started
  from a terminal. `--gui` and `--cli` override the choice.
- **An uninstaller that puts things back.** Each install puts a separate
  uninstaller beside the program. The uninstaller removes what the install
  created and restores what it replaced, including registry values on
  Windows. Only the installed uninstaller removes a program, so removal
  always matches what was installed.
- **Per-user and system-wide installs.** A system install asks for
  elevation once, for the step that writes files. The window never runs as
  root.
- **Desktop integration.** Launcher entries, icons at every size, and links
  on `PATH` on Linux; Start Menu shortcuts and an entry in Settings > Apps on
  Windows.
- **A command-line-only build** with no Fyne and no cgo, for machines with
  no graphics libraries.

Linux comes first, then Windows. macOS is later.

## Development

```bash
make setup        # installs the pinned golangci-lint
make test
make lint
make build        # bin/fynstall
make hello        # examples/hello/bin/hello, the program the tests install
make greet        # examples/greet for every target, the multi-target example
```

Go 1.26 or newer. Anything with a window needs cgo, OpenGL and X11/Wayland
headers. `make test` builds real installers and runs them in temporary
directories; it needs the Go module cache but not the network.

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## Licence

MIT. See [LICENSE](LICENSE).

## Changelog

### Unreleased

- Real runtimes (spec 002 phase 3): a symlink inside a payload directory is
  installed as a link, or as a copy for a Windows target, and one that
  leaves its entry is refused. `uninstall.remove` names the files a program
  makes, such as a bytecode cache, for the uninstaller to remove. Other files
  the program left in the install directory are listed, not deleted; the
  uninstall window offers "Remove them too", and `--remove-leftovers` does
  the same on the command line. Needs fynedesygn v0.1.95
  ([#6](https://github.com/ushineko/fynstall/issues/6)).

- Design: spec 002 takes MSI as its reference model, adds uninstall hooks
  (`run` with `on: uninstall`), keeps symlinks that stay inside a payload
  (D4), and lists rather than deletes the files a program creates (D5),
  each found by packaging a real CPython runtime. Spec 003 designs Go
  extensions: a program's own wizard pages and install hooks, linked into
  its installer and uninstaller ([#6](https://github.com/ushineko/fynstall/issues/6),
  [#11](https://github.com/ushineko/fynstall/issues/11)).

- The wizard: `fynstall build` makes a full installer and uninstaller with
  the fynedesygn wizard (welcome, licence, settings, location, summary,
  progress, finish), and `--with-cli-only` the CLI variant beside it. The
  installer opens the wizard when started from the desktop and runs the
  command line from a terminal; `--gui` and `--cli` override it. A full
  build's launcher entry has an Uninstall action. The uninstaller asks once
  in a small window (fynedesygn `RunConfirm`), and does not ask on the
  command line; a copy outside an install hands over to the installed one.
  Installing shows a bar with the count of files and bytes. New keys:
  `app.licence`, `ui.welcome`, `ui.launch` (spec 001 phase 4 and spec 002
  phase 2, [#1](https://github.com/ushineko/fynstall/issues/1)).

- Parameters and configuration files: a config declares `parameters`, given
  by flag, by `fynstall-params.yml` beside the installer, at a prompt or by
  default, and a `config_file` action writes them into a YAML or JSON file
  that the uninstaller reverses. Secrets are never printed, logged or
  recorded, and a test passes one through every path to check (spec 002
  phase 2, [#6](https://github.com/ushineko/fynstall/issues/6)).

- One config for every target: payload entries can name their `targets`,
  and `{os}`, `{arch}` and `{exe}` resolve per target in payload paths,
  links and desktop entries. Each installer carries the payload for its own
  target. `examples/greet` builds for Linux amd64 and arm64 from one config
  (spec 002 phase 1, [#6](https://github.com/ushineko/fynstall/issues/6)).

- Design for what replacing a production installer requires (spec 002,
  [#6](https://github.com/ushineko/fynstall/issues/6)): one config for every
  platform, payloads per target, install-time actions with an undo, and
  declared parameters with configuration kept outside the install directory.
  Spec 001's later phases are updated to match.

- Desktop integration on Linux: the build resizes `app.icon` to the seven
  hicolor sizes, and the installer adds launcher entries, icons and links in
  `~/.local/bin`, then refreshes the KDE menu. The uninstaller puts back a
  file or link that a link replaced (spec 001 phase 2,
  [#1](https://github.com/ushineko/fynstall/issues/1)).

- Command-line installers for Linux, per-user: `fynstall init`, `validate`
  and `build --cli-only`. The installer checks each file's sha256, undoes a
  failed install, and installs a separate uninstaller that restores what the
  install replaced. A newer installer's `--uninstall` runs the installed
  uninstaller (spec 001 phase 1,
  [#1](https://github.com/ushineko/fynstall/issues/1)).

- Project skeleton: module, Makefile, lint configuration, the `fynstall
  version` command and the `examples/hello` program (spec 001 phase 0,
  [#1](https://github.com/ushineko/fynstall/issues/1)).
