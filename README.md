# fynstall

**Version**: 0.1.0

An installer framework for Go and [Fyne](https://fyne.io) programs. You
describe a program in `fynstall.yaml`, and `fynstall build` produces one
installer binary per target platform. The installer holds the whole payload
through Go embedding, so the person who runs it needs no toolchain.

## Status

Early development. The plan is [spec 001](specs/001-installer-prototype.md),
delivered in phases; this README changes as each phase lands.

Phases 1 and 2 build command-line installers for Linux, per-user, with
launcher entries, icons and links on `PATH`. The wizard, system-wide
installs, upgrades and Windows follow.

## Using it

```bash
fynstall init                  # writes a commented fynstall.yaml
fynstall validate              # lists every problem, with line numbers
fynstall build --cli-only      # dist/<name>-<version>-<os>-<arch>-installer
                               # and the matching -uninstaller
```

The installer asks where to install and asks for confirmation. `--yes`
accepts the defaults, `--dry-run` lists every change and makes none, and
`--dir` chooses the directory. Run `<install directory>/uninstall` to remove
the program. A newer installer's `--uninstall` runs that same file.

Until fynstall has a release, build against a checkout:
`fynstall build --cli-only --runtime-path <path to fynstall>`.

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
```

Go 1.26 or newer. Anything with a window needs cgo, OpenGL and X11/Wayland
headers. `make test` builds real installers and runs them in temporary
directories; it needs the Go module cache but not the network.

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## Licence

MIT. See [LICENSE](LICENSE).

## Changelog

### Unreleased

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
