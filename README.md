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
entries, icons, links on `PATH`, parameters and configuration files, and
system-wide installs and upgrades. On Windows: per-user and system-wide
installs from the wizard and the command line, with a Start Menu shortcut,
an entry in Settings > Apps and an entry on `PATH`. Services on Windows
follow.

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

The config format is in [docs/config.md](docs/config.md), and what an install
does on each platform, with the paths and elevation, in
[docs/platforms.md](docs/platforms.md).

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
make hello        # examples/hello/bin/hello (hello.exe on Windows), the program the tests install
make greet        # examples/greet for every target, the multi-target example
make beacon       # examples/beacon, the example of services and other actions
```

Go 1.26 or newer. Anything with a window needs cgo, OpenGL and X11/Wayland
headers. `make test` builds real installers and runs them in temporary
directories; it needs the Go module cache but not the network.

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## Licence

MIT. See [LICENSE](LICENSE).

## Changelog

### Unreleased

- Services on Windows (spec 001 phase 7d, spec 002 D2a): a `service`
  action registers a service with the Windows service manager, running as
  LocalSystem, started with the computer, with recovery actions for
  `restart`. Only an install for everyone has one. The uninstaller stops
  the service and waits for its process before it removes the program. A
  service of the same name that is there already stops the install.
  `examples/beacon` runs as a Windows service and builds for Windows from
  the same config ([#1](https://github.com/ushineko/fynstall/issues/1),
  [#6](https://github.com/ushineko/fynstall/issues/6)).

- The version and the manifest of a Windows installer (spec 001 phase 7d,
  spec 002 L7): an installer and an uninstaller for Windows carry a version
  resource made from `app` (name, version, publisher), which Properties >
  Details shows, and an application manifest that runs them with the rights
  of whoever starts them. No config keys
  ([#1](https://github.com/ushineko/fynstall/issues/1),
  [#6](https://github.com/ushineko/fynstall/issues/6)).

- System scope on Windows (spec 001 phase 7c): an install for everyone on
  the computer goes in `%ProgramFiles%\<id>`, with its record in
  `%ProgramData%`, its Settings > Apps entry under `HKLM`, its shortcut in
  the Start Menu of all users and its directory on the computer's `PATH`.
  Only the changes run as an administrator, in a helper started through one
  UAC prompt, which reports over named pipes; the uninstaller elevates the
  same way. The wizard asks who the install is for. The record of a system
  install is taken only when an administrator wrote it
  ([#1](https://github.com/ushineko/fynstall/issues/1)).

- The wizard on Windows (spec 001 phase 7c): `fynstall build` on Windows
  makes the full installer and uninstaller. Started from Explorer, the
  Start Menu or Settings > Apps they open their window and close the
  console Windows made for them; typed into a console they run on the
  command line (R19). An installer run as an administrator uses the command
  line. A problem found before the wizard opens is shown in a window, on
  Linux too. The wizard offers only the scopes the platform has
  ([#1](https://github.com/ushineko/fynstall/issues/1)).

- `{programs}` and the icon of the installer on Windows (spec 001 phase
  7b): the new placeholder `{programs}` is where the platform keeps
  programs, and `{programs}/{id}` is the default install directory for both
  scopes. On Linux that is the directory as before; on Windows it is
  `%LOCALAPPDATA%\Programs\<id>`. An installer and an uninstaller for
  Windows carry `app.icon` as a resource, so Explorer shows it.
  `examples/hello` uses `{exe}` and `{programs}`, and one config builds it
  for Linux and for Windows; `make hello` writes `hello.exe` on Windows
  ([#1](https://github.com/ushineko/fynstall/issues/1)).

- Windows desktop integration, per-user (spec 001 phase 7b): the keys that
  make a launcher entry, an icon and a link on Linux make a Start Menu
  shortcut, an `.ico` and a `PATH` entry on Windows, and every install has
  an entry in Settings > Apps that runs the installed `uninstall.exe`. The
  receipt journals each registry key and value, and the uninstaller puts
  the registry back. It takes its own entry out of `PATH` and leaves the
  rest. No new config keys
  ([#1](https://github.com/ushineko/fynstall/issues/1),
  [#6](https://github.com/ushineko/fynstall/issues/6)).

- Windows, per-user, from the command line (spec 001 phase 7a):
  `fynstall build --cli-only --target windows/amd64` builds an `.exe`
  installer and uninstaller, from Linux or from Windows. The install goes
  under `%LOCALAPPDATA%`, with configuration under `%APPDATA%`. The
  uninstaller is `uninstall.exe`; it moves its own running file to the
  temporary directory, so the install directory is gone when it exits. The
  install lock is a named mutex. Upgrade, repair and downgrade work as on
  Linux. The wizard and system scope are not on Windows yet.
  `examples/greet` builds for `windows/amd64`, and the tests
  run on Windows ([#1](https://github.com/ushineko/fynstall/issues/1)).

- Launcher entries take `generic_name`, `keywords`, `startup_notify` and
  `startup_wm_class`, which defaults to the entry's ID so X11 taskbars match
  a Fyne window to it. Found by comparing fynstall with clockwork-orange's
  `install.sh` (spec 001 phase 6b), which the spec records with the gaps
  still open ([#1](https://github.com/ushineko/fynstall/issues/1)).

- Upgrade, repair and downgrade (spec 001 phase 6a, R17): an installer run
  over an installed version replaces it through that version's own
  uninstaller, keeps the parameters it was given (secrets read back from
  its configuration file), and installs only the plan it showed. The wizard
  opens on what it will do, with "Uninstall it instead". A downgrade asks
  first, or needs `--downgrade` with `--yes`. A system upgrade asks for an
  administrator once ([#1](https://github.com/ushineko/fynstall/issues/1)).

- System scope on Linux (spec 001 phase 5, spec 002 phase 4b): an install
  for everyone on the computer goes in `/opt/<id>`, with launcher entries
  and icons under `/usr/local/share` and links in `/usr/local/bin`. Only
  the changes run as root, in a helper the installer starts under `pkexec`
  or `sudo`; the helper makes the plan again and refuses one that is not
  what the person approved. The uninstaller elevates the same way, once. A
  service in a system install is a system unit. The wizard asks who the
  install is for when the config offers both scopes
  ([#1](https://github.com/ushineko/fynstall/issues/1),
  [#6](https://github.com/ushineko/fynstall/issues/6)).

- Actions (spec 002 phase 4a): `service` runs a payload program as a
  systemd user unit, and the uninstaller stops and removes it and puts back
  a unit of that name that was there before. `run` runs a payload program
  with an undo, or as an uninstall hook (`on: uninstall`) before anything is
  removed. `migrate` moves an older version's data into a kept path. A
  failed install undoes its actions. Only one installer or uninstaller of
  an app runs at a time. `examples/beacon` shows them
  ([#6](https://github.com/ushineko/fynstall/issues/6)).

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
