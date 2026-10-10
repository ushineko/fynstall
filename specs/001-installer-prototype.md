# Spec 001: installer prototype

**Issue**: [#1](https://github.com/ushineko/fynstall/issues/1)
**Depends on**: [fynedesygn spec 061 / #172](https://github.com/ushineko/fynedesygn/issues/172) (the `wizard` package), phase 4 only
**Related**: [spec 002](002-replacing-a-production-installer.md) adds per-target payloads, parameters and install-time actions, and reshapes phases 4–7 here

## Status: INCOMPLETE

## Executive Summary

Populated before the first PR is opened.

## Context

The Go/Fyne programs in the fynedesygn "Used by" table install with
hand-written scripts. clockwork-orange's `install.sh` is typical. It builds
from a checkout, copies binaries into `~/.local/bin`, writes a `.desktop` entry
and an icon at seven hicolor sizes, supports `--dry-run` and `--no-gui`, and
leaves user data alone on uninstall. Each program carries its own copy of this
logic, and it needs Go and a C toolchain on the target machine.

fynstall replaces that with one tool. A developer writes `fynstall.yaml` and
runs `fynstall build`. The result is one installer binary per target platform
that holds the whole payload through Go embedding. The end user runs it, with
no toolchain needed.

The installer has two front ends over one install engine:

- **GUI**: a wizard built on the fynedesygn `wizard` package (spec 061).
- **CLI**: prompts in a terminal, or no prompts with `--yes` and flags.

It selects the front end from how it was started: from a desktop (double-click)
or from a terminal. `--gui` and `--cli` override the selection.

Platforms in order: Linux (this spec, phases 1–6), Windows (phase 7), macOS
(later, out of scope here). Install scope is per-user or system-wide, and both
are supported. An optional CLI-only installer, built without Fyne and cgo,
covers machines with no graphics libraries.

### Architecture

```
fynstall.yaml ──► fynstall build ──► generated main package (temp dir)
                    │                  ├─ main.go        → installer.Main(manifest, payload)
                    │                  ├─ manifest.json  (normalised config + per-file sha256 and mode)
                    │                  ├─ payload/…      (//go:embed all:payload)
                    │                  └─ uninstaller    (separate payload-free build; also written to dist/)
                    └─► go build (GOOS/GOARCH, CGO, -tags nogui for --cli-only)
                          └─► dist/<app>-<version>-<os>-<arch>-installer[.exe]

installer at run time:
  mode.Detect() ─► gui front end (wizard) ─┐
               └─► cli front end ──────────┼─► engine.Plan() ─► engine.Apply(plan, events)
                                           │        (privileged scope: Apply runs in a
                                           │         helper process started with pkexec/sudo/UAC)
                                           └─► receipt + journal written ─► uninstaller installed

installed program:  <install dir>/uninstall  ◄── the only thing that removes it
                    (Settings > Apps, CLI, a newer installer's Uninstall/Upgrade all call it)
```

Module layout (proposed):

| Path | Purpose |
|---|---|
| `cmd/fynstall` | The builder: `init`, `validate`, `build`. |
| `config` | YAML schema, defaults, validation with line numbers in errors. |
| `manifest` | The normalised form embedded in the installer. |
| `builder` | Stages the payload, generates the main package, runs `go build`. |
| `installer` | `Main()` and `UninstallMain()`: flag parsing, mode selection, front-end dispatch. Named `installer` rather than `runtime`, which would hide the standard library package. |
| `engine` | Plan, apply, receipt, uninstall, rollback. No UI imports. |
| `platform` | Paths and desktop integration per OS and scope, behind build tags. |
| `ui/cli`, `ui/gui` | The two front ends. `ui/gui` has the build tag `!nogui`. |
| `examples/hello` | A small Fyne program with an icon and a `fynstall.yaml`; the fixture for every desk check. |

### Config sketch

```yaml
app:
  id: io.ushineko.hello          # reverse-DNS; used for .desktop name and registry key
  name: Hello
  version: 0.1.0                 # or: version_from: "git describe --tags"
  publisher: ushineko
  icon: assets/hello.png         # one PNG ≥ 512 px; build resizes to hicolor sizes / .ico
  licence: LICENSE               # optional; adds the licence page

install:
  scopes: [user, system]         # which the user may choose; first is the default
  dir:
    user:   "{data}/{id}"        # ~/.local/share/io.ushineko.hello
    system: "/opt/{id}"

payload:
  - src: bin/hello               # file; mode detected (ELF → 0755) unless given
    dst: bin/hello
  - src: share/                  # directory, recursive
    dst: share/
    exclude: ["*.tmp"]

components:                      # optional; shown as checks on the options page
  - id: gui
    name: Desktop window
    default: true
    payload: [bin/hello-gui]

integration:
  path_links: [bin/hello]        # symlink into ~/.local/bin or /usr/local/bin
  desktop:
    - name: Hello
      exec: bin/hello-gui
      categories: [Utility]
      component: gui
  keep_on_uninstall:             # documented in the uninstaller output; never touched
    - "{config}/hello"

ui:
  welcome: docs/welcome.md
  finish:
    launch: bin/hello-gui        # adds a "Launch now" check

targets: [linux/amd64, windows/amd64]
```

Placeholders such as `{data}`, `{config}`, `{bin}` and `{id}` resolve per OS
and scope in `platform`, so one config serves every target.

## Requirements

### Builder

- R1 `fynstall init` writes a commented `fynstall.yaml` for the current
  directory. `fynstall validate` reports every error with file, line and
  field. An unknown key is an error.
- R2 `fynstall build [-c fynstall.yaml] [--target os/arch ...] [--cli-only]
  [-o dist/]` produces one installer per target. Builds are reproducible:
  the same inputs give the same file. That means `-trimpath`, a fixed
  mod time in the embedded tree, and sorted manifest entries.
- R3 The manifest records, for every payload file, its destination,
  sha256, size and mode. Mode is taken from the config, otherwise detected
  (ELF, PE or `#!` gives 0755, and anything else gives 0644). This is needed
  because `embed.FS` does not keep file modes.
- R4 The build resizes the icon to the hicolor sizes (16, 32, 48, 64, 128,
  256, 512) and to a multi-size `.ico` for Windows. The installer's own window
  icon is the same image. The `.ico` is phase 7 work and the window icon is
  phase 4 work; phase 2 delivers the hicolor sizes. The source must be a
  square PNG of at least 512 px, so every size is a reduction.
- R5 The generated module requires the fynstall runtime at the builder's own
  module version (from `debug.ReadBuildInfo`). For local development,
  `--runtime-path` adds a `replace` directive.

### Engine

- R6 `Plan(manifest, choices) → Plan` lists every file, directory, link,
  desktop entry and registry key the install will create or replace. It
  makes no changes. `--dry-run` prints the plan in the CLI, and the GUI
  summary page shows it.
- R7 `Apply(plan, events)` writes each file to a temporary name in the
  target directory, checks its sha256, then renames it into place. If a
  step fails, Apply removes what this run created and restores what it
  replaced. The previous state is then intact, or the error names the files
  it could not restore.
- R8 After a successful apply, the engine writes a receipt
  (`<install dir>/.fynstall/receipt.json`). The receipt holds the app id,
  version, scope, the fynstall runtime version, and a journal of every
  change: each path created, each file replaced (with its backup, see R9a),
  and each registry key or value created or changed (with its previous
  value, Windows). Apply also writes an install index entry,
  `{data}/fynstall/installs/<id>.json`, which points at the install
  directory and its uninstaller. A later installer finds an install through
  it, wherever the user put the install. The entry is in the journal, so the
  uninstaller removes it.
- R9 See the Uninstaller requirements below (R9a–R9f).
- R10 The engine reports events (step started, file written, warning)
  through a callback that each front end supplies. The engine never imports a UI package. Both
  front ends consume the same events.

### Uninstaller

The model is the one most installers use, and a fuller version of the
`install.sh` / `uninstall.sh` pair. The uninstaller is a separate artifact.
It is installed with the program, and it is the only code that removes that
program. A newer fynstall can change how it installs. The installed program
is always removed by the code that installed it, so the removal logic never
drifts from what is on disk.

- R9a Before Apply replaces or changes anything that existed before the
  install, it records the original state. A pre-existing file is copied to
  `<install dir>/.fynstall/backup/`. A pre-existing registry value is stored
  in the receipt journal. A pre-existing `.desktop` entry, link or icon at a
  planned path counts as a pre-existing file.
- R9b `fynstall build` produces the uninstaller as its own artifact: a
  payload-free build of the same runtime version, for the same target, with
  both front ends and the same mode selection (R11, R19). A `--cli-only`
  installer carries a `--cli-only` uninstaller. The uninstaller is written
  to `dist/` next to the installer, so it can be inspected, and is embedded
  in the installer.
- R9c Apply installs the uninstaller as `<install dir>/uninstall`
  (`uninstall.exe` on Windows). Every system entry point for removal points
  at that installed file. On Windows this is the `UninstallString` in the
  Uninstall registry key. On Linux the receipt and the install index record
  the path. An optional "Uninstall <name>" `.desktop` action that points at
  it is phase 4 work: launched from the menu it has no terminal, and the CLI
  uninstaller refuses to run without one unless given `--yes`.
- R9d The uninstaller reads the journal in reverse order. It removes what
  the install created, restores each backed-up file and previous registry
  value, removes registry keys the install created (Windows), and removes
  directories that are then empty. The result is the system as it was before
  the install, apart from paths in `keep_on_uninstall`. Those paths are never
  touched, and the uninstaller prints them so the user knows where their
  data is.
- R9e A newer installer never removes an installed program with its own
  logic. When it finds a receipt (R17), Uninstall and the remove step of an
  upgrade run the installed uninstaller (`uninstall --quiet --keep-data` for
  an upgrade) and wait for its exit code. If the installed uninstaller is
  missing or fails, the installer stops, reports the receipt path, and offers
  `--force-receipt-uninstall`. That option is an explicit, labelled fallback
  that removes the receipt journal with the current engine.
- R9f The uninstaller deletes itself last. On Windows it cannot delete its
  own running executable, so it copies itself to `%TEMP%`, runs from there,
  and schedules the copy for deletion with `MoveFileEx`
  (`MOVEFILE_DELAY_UNTIL_REBOOT`).

### Mode selection

- R11 Linux: CLI when stdin is a terminal. GUI when stdin is not a terminal
  and `WAYLAND_DISPLAY` or `DISPLAY` is set. Otherwise, CLI non-interactive,
  which fails with a message naming `--yes` if input is needed. `--gui` and
  `--cli` override this.
- R12 A `--cli-only` installer (`-tags nogui`) has no Fyne dependency, is
  built with `CGO_ENABLED=0`, and links no graphics libraries. `--gui` on
  it fails with a clear message.

### Scope and elevation

- R13 Per-user scope never asks for elevation. System scope (Linux) runs
  only `Apply` in a privileged helper, which is the same binary started with
  `--apply-plan <file>` under `pkexec` (GUI) or `sudo` (CLI). The helper
  streams events as JSON lines on stdout. The Fyne process never runs as
  root.
- R14 The plan file passed to the helper is written with mode 0600 in a
  directory only the user can write. The helper re-checks every payload
  sha256 from its own embedded copy. It does not trust hashes from the plan
  file.
  - Settled in phase 5 (2026-10-09): the plan file holds only the inputs
    (scope, directory, parameter values) and the sha256 of the plan the
    person approved. The helper makes the plan again, as root, from its own
    embedded payload, and refuses when its digest differs. No path or hash
    in the file is used.
  - The helper's stdin is its cancel: the person cannot signal a root
    process, so the parent closes the pipe and the helper cancels and
    undoes.
  - The uninstaller elevates itself the same way. The list for "Remove
    them too" passes to the second elevation through a root-owned file
    under `/run/fynstall`, never through one the person supplies.

### Desktop integration (Linux)

- R15 Per-user scope: `.desktop` in `~/.local/share/applications`, icons in
  `~/.local/share/icons/hicolor/<size>/apps`, and links in `~/.local/bin`.
  System scope: `/usr/local/share/applications`,
  `/usr/local/share/icons/hicolor` and `/usr/local/bin`; `/usr/share`
  belongs to the package manager (changed in phase 5, 2026-10-09). The
  system install index is `/var/lib/fynstall/installs`, and `{config}` is
  `/etc`. After a change, the installer and the uninstaller
  refresh the menu with `kbuildsycoca6` if it is present. A failure there is
  a warning and not an error. `update-desktop-database` is not run: it
  rebuilds only `mimeinfo.cache`, the entries declare no MIME types, and
  rewriting that file would be a change the install had no reason to make.
- R16 If `~/.local/bin` is not on `PATH`, the finish page and the CLI output
  say so.

### Upgrade

- R17 When a receipt for the same app id and scope exists, the installer
  offers Upgrade (newer version), Repair (same version) or Downgrade (older
  version, with a confirmation). It also offers Uninstall. Uninstall and
  the remove step of an upgrade, repair or downgrade go through the installed
  uninstaller (R9e). The installer then installs fresh, so files the old
  version had and the new one does not are removed by the code that put them
  there.

### Windows (phase 7)

- R18 Cross-built from Linux with `x86_64-w64-mingw32-gcc`, which is
  present on this machine. Per-user target is
  `%LOCALAPPDATA%\Programs\<name>`. System target is
  `%ProgramFiles%\<name>`. A Start Menu shortcut is created (`.lnk` through
  IShellLink). An Uninstall registry entry under HKCU or HKLM makes the app
  appear in Settings > Apps.
- R19 Mode selection: GUI when the process is the only one attached to its
  console (`GetConsoleProcessList` returns 1, which is how Explorer starts
  it), and the console is then released. Otherwise CLI. System scope runs the
  helper through `ShellExecuteEx` with `runas`.

## Phases

Each phase is one PR. Each phase ends with a **desk check**: a short
script of commands and observations on this machine, recorded in the PR. Each
phase leaves a working tool, so the work can stop after any phase.

### Phase 0: bootstrap

Go module `github.com/ushineko/fynstall`, Makefile (`setup` installs the
pinned golangci-lint; `build`, `test`, `lint`, `vuln` = `govulncheck`, with the
lint config in `config/` as in the sibling projects), README skeleton with a
changelog under `### Unreleased`, `docs/style.md`, MIT licence, and
`examples/hello` (a Fyne window on fynedesygn `shell` with an icon).
`.claude/CLAUDE.md`, `CONTRIBUTING.md`, `MAINTAINERS.md` and the PR template
already exist.

- [x] `make build test lint` passes on an empty skeleton.
- [x] Desk check: `go run ./examples/hello` opens a window. On Wayland the
      taskbar and title bar show a generic icon until a desktop entry named
      `io.ushineko.hello.desktop` exists; see Verification.

### Phase 1: config, build, CLI-only installer and uninstaller (R1–R3, R5–R10, R9a–R9e, R12)

Phase 1 refuses an existing install instead of layering over it. The
installer names `--uninstall`, which runs the installed uninstaller (R9e).
Upgrade, repair and downgrade stay in phase 6. `fynstall build` without
`--cli-only` stops with a message that names phase 4.

- [x] Config tests: valid, unknown key, missing required field and bad
      placeholder each give the expected error with line number (unit, table
      driven). `config/config_test.go`, which also covers a `dst` that
      leaves the install directory, a missing `src`, and all errors at once.
- [x] Manifest test: modes detected for an ELF, a script and a text file.
      Entries are sorted. The same input twice gives byte-identical
      `manifest.json` (unit). `manifest/manifest_test.go`,
      `builder/stage_test.go`.
- [x] Integration test: build the `hello` installer with `--cli-only`, run it
      with `HOME` set to `t.TempDir()`, `--cli --yes --scope user`. Check
      every file against the manifest hashes, run the uninstaller, and check
      that the temp home holds only what it held before. This uses real files
      and real processes, with no mocks of the filesystem.
      `TestInstallWritesThePayloadAndTheUninstallerRemovesIt`.
- [x] Uninstaller is a separate artifact: `build` writes
      `dist/hello-*-uninstaller` beside the installer. The installed
      `uninstall` is byte-identical to it (R9b, R9c).
- [x] Restore test: a temp home holds a pre-existing file at a planned
      destination inside the install directory. After install and
      uninstall, a recursive hash of the temp home matches the hash taken
      before install (R9a, R9d). `TestUninstallRestoresAFileTheInstallReplaced`.
      The pre-existing `~/.local/bin/hello` case moves to phase 2, which adds
      the links (R15).
- [x] Drift test: install with the v1 installer, then run a v2 installer
      with `--uninstall`. The v2 build differs in its stamped runtime
      version, and the output shows the v1 uninstaller's banner (R9e).
      `TestANewerInstallerRemovesThroughTheInstalledUninstaller`. A mutation
      that made `--uninstall` use the current engine failed this test.
- [x] Rollback test: a payload file whose content no longer matches its
      hash makes Apply fail. The target holds exactly what it held before,
      and a pre-existing file at a destination is restored. This runs the
      engine in-process against a real temporary filesystem, because an
      embedded payload cannot be corrupted after the build without editing
      the binary. It is install-time rollback, separate from the
      uninstaller's restore.
      `TestAFailedInstallUndoesItselfAndRestoresWhatItReplaced`.
- [x] Reproducibility: two builds of the same input have the same sha256.
      `TestBuildsAreReproducible`.
- [x] R12: the `--cli-only` installer and uninstaller have no dynamic
      loader and import no libraries (checked with `debug/elf`, which is
      what `ldd`'s "not a dynamic executable" reports).
      `TestCLIOnlyProgramsLinkNoLibraries`.
- [x] Desk check: `fynstall build --cli-only` in `examples/hello`, then
      `./dist/hello-*-installer --dry-run`, then `--cli` interactive install.
      Run `~/.local/share/io.ushineko.hello/bin/hello`, then run the
      installed `uninstall`. Nothing is left behind (the top-level listings
      of `~/.local/share`, `~/.config` and `~/.local/bin` match those taken
      before the install), and the uninstaller lists the
      `keep_on_uninstall` paths.

### Phase 2: Linux desktop integration, per-user (R4, R15, R16)

- [x] Icon resize produces the seven sizes. Each is a valid PNG of the
      stated size. Checked on the installed icons in
      `TestInstallWritesThePayloadAndTheUninstallerRemovesIt`; a non-square,
      too small or non-PNG icon is a config error with its line
      (`TestIntegrationKeysAreChecked`).
- [x] Integration test (temp `HOME`): `.desktop` passes
      `desktop-file-validate` with no warnings, or is logged as unvalidated
      where the tool is missing. Icons exist at every size. The
      `~/.local/bin/hello` link resolves. Uninstall removes all three.
      `Exec` quoting follows the specification for spaces, quotes, `$`,
      backticks, backslashes and `%` (`platform/desktop_test.go`).
- [x] Restore test, moved from phase 1: a pre-existing `~/.local/bin/hello`
      is replaced by the link on install and restored on uninstall
      (`TestUninstallRestoresTheFileALinkReplaced`). In the engine, a
      pre-existing symlink is restored as a symlink with its target
      (`TestALinkPutsBackTheFileOrLinkItReplaced`); a mutation that restored
      it as nothing failed that test.
- [x] Containment outside the install directory: an `icons` directory that
      is a symlink out of `{data}` is refused
      (`TestAnIconDirectoryLinkedOutsideDataIsRefused`).
- [x] The menu refresh runs once after the install and once after the
      uninstall (`TestTheMenuIsRefreshedAfterInstallAndUninstall`, with a
      fake `kbuildsycoca6` on `PATH`). The other real-process tests run with
      an empty `PATH`, so they never rebuild the real KDE cache.
- [x] Desk check (KDE Plasma 6): after install, "Hello" appears in the
      application launcher with its icon at menu and panel sizes, and it
      starts from there. On Wayland the running window shows the icon in the
      taskbar, which proves the desktop file's base name matches the app ID. After uninstall it is gone from the launcher without
      a logout.

### Phase 3: fynedesygn `wizard` (fynedesygn spec 061)

This phase is done in the fynedesygn repository and can run in parallel with
phases 1 and 2. Its acceptance criteria are in that spec.

- [x] fynedesygn spec 061 is COMPLETE and released, and fynstall `go.mod`
      requires that version. Spec 061 was first numbered 056; upstream took
      that number while the work was in flight. It landed in fynedesygn #182
      and was released as v0.1.91, which `go.mod` now requires.

### Phase 4: GUI front end and mode selection (R11, R12)

Lands after spec 002 phase 1 (per-target payloads), with spec 002 phase 2
(parameters).

- [x] The GUI builds the page sequence from the manifest: Welcome, Licence
      (if set), Settings (parameters, if any), Location, Ready (the plan,
      R6), Installing (engine events), Done (launch check if set). Scope
      and Components wait for phase 5 and for components. The GUI is in
      package `installer` behind the `!nogui` tag rather than in a
      `ui/gui` package, which avoids an import cycle and keeps the
      CLI-only build free of Fyne. `TestTheWizardInstallsWithItsParameters`
      drives a full install into a temp `HOME` with `wizard.Headless`;
      `TestTheWizardHoldsInstallWhenThePlanCannotBeMade` and
      `TestAnInstalledProgramGetsItsOwnUninstallerOffered` cover the rest.
- [x] Mode-selection unit tests over a table of (stdin is a terminal,
      `DISPLAY`, `WAYLAND_DISPLAY`, flags) → mode (`TestChooseMode`). A
      CLI-only flag such as `--yes` means the CLI; `--gui` without a
      display is an error, not a Fyne crash.
- [x] The full installer and the `--cli-only` installer come from one
      `fynstall build` call with both outputs requested (`--with-cli-only`;
      `TestOneBuildMakesTheFullAndTheCLIVariant`). The full variant builds
      only for this machine, because it needs cgo, and says so for another
      target.
- [x] Headless probe: the full installer run in a container with no
      libGL or X11 libraries. The result is recorded: it fails to start, or
      it starts in CLI mode. The README states which. The `--cli-only`
      installer installs successfully in the same container. In
      `debian:stable-slim` the full installer does not start
      (`libGL.so.1: cannot open shared object file`, exit 127, even for
      `--version`), and the CLI variant installs and uninstalls.
- [x] Desk check: double-click the installer in Dolphin and the wizard
      opens. Run it from Konsole and the CLI runs. `--gui` from Konsole
      opens the wizard. Cancel during the progress page leaves no files.
      Launch now on the finish page starts Hello.
- [x] When the config declares parameters (spec 002 D3a), the wizard has a
      page for them, generated with fynedesygn `forms`, and a secret is a
      password entry. A required one holds Next until it is filled
      (`TestTheWizardInstallsWithItsParameters`).
- [x] A full build's launcher entry has an Uninstall action that runs the
      installed uninstaller's wizard (R9c), and passes
      `desktop-file-validate` (`TestTheFullInstallerRunsAsTheCLIWithoutADisplay`).
      The full installer, run with no display, installs as the CLI.
- [x] The uninstaller asks once in a small window (fynedesygn v0.1.92,
      `wizard.RunConfirm`), and `--yes` skips the question. On the command
      line it does not ask (`TestTheUninstallerAsksOnceThenRemoves`,
      `TestTheCLIUninstallerRemovesWithoutAsking`). Changed at the desk:
      the first version was a three-page wizard and a y/N prompt.
- [x] An uninstaller that is not inside an install (the copy in `dist/`)
      hands over to the installed one through the index, or says the
      program is not installed. Started from the desktop, it shows problems
      in a window (`TestTheUninstallerInDistHandsOverToTheInstalledOne`).
      Found at the desk: a double-click on the `dist/` copy failed with an
      error on stderr only, and nothing on screen.
- [x] Installing shows a bar, the count of files and bytes and the file
      being written (fynedesygn v0.1.92, `WithBar`); a terminal gets one
      progress line, a pipe none. The engine reports every file, and inside
      a large file every MiB (`TestProgressCountsEveryFileAndByteAndMovesInsideALargeFile`,
      `TestTheProgressLineIsForTerminalsOnly`). A payload of 5,001 files
      builds in about 0.5 s, installs in about 0.3 s and uninstalls to an
      unchanged home (`TestThousandsOfFilesInstallAndUninstall`).

### Phase 5: system scope on Linux (R13, R14)

Lands with spec 002 phase 4b: the privileged helper applies the actions
that spec 002 phase 4a adds in per-user scope. Spec 002 phase 3 (symlinks
and leftovers) comes first.

Tests run the helper through a stand-in for `pkexec` (`FYNSTALL_ELEVATE`)
and with the system paths under a temporary directory
(`FYNSTALL_TEST_SYSTEM_ROOT`, ignored when running as root).

- [x] Helper protocol test: the parent starts the helper without elevation
      (test hook), receives JSON events, and handles a helper crash as a
      failed step (integration). (`TestASystemInstallRunsInTheHelperAndItsUninstallerElevatesToo`,
      `TestAHelperThatDiesIsAFailedInstall`,
      `TestARefusedAdministratorChangesNothing`,
      `TestASystemUninstallListsOrRemovesTheLeftovers`.)
- [x] R14 test: a plan file whose digest does not match the plan the
      helper makes is refused, and nothing changes (integration).
      (`TestAPlanFileThatWasEditedIsRefused`.)
- [x] The wizard offers "Just me" and "Everyone on this computer" when the
      config offers both scopes, and the choice sets the default
      directory (headless). (`TestTheWizardAsksWhoTheInstallIsFor`.)
- [x] Desk check: GUI install in system scope shows one pkexec prompt. Files
      are in `/opt/io.ushineko.hello`, `/usr/local/share/applications` and
      `/usr/local/bin`. `ps` during install shows the Fyne process running
      as the user. CLI system install uses `sudo`. Uninstall in each case
      asks for elevation once and removes everything.
- [x] The privileged helper applies actions as well as files (spec 002
      D2a): a `service` action as a systemd system unit, and the install
      lock (L3) in `/run/fynstall`. Permissions (L4) moved to phase 7.

### Phase 6: upgrade, repair, and a real consumer (R17)

Two PRs: 6a is upgrade, repair and downgrade; 6b is the first consumer.

Settled for 6a (2026-10-09):

- The installer reads the old install's receipt and shows the plan it
  predicts for after the old version is gone. It then runs the old
  uninstaller, plans again, and installs only when the new plan has the
  same content as the one shown.
- The old uninstaller runs with `--upgrade` when its `-h` lists that flag,
  else with `--quiet`, which removes the same files, so every uninstaller
  ever installed stays usable.
- The old version's uninstall hooks and `run` undos run during an upgrade
  as on any uninstall, with `FYNSTALL_UNINSTALL_REASON` set to `upgrade`
  or `uninstall` in their environment.
- A system upgrade runs whole in one root helper, after one prompt. The
  helper checks the predicted plan's digest before it removes anything.
  A file that holds a secret is in the digest by path and mode only, so
  the helper can read the old secret as root.
- The wizard's first page says what an existing install becomes, and has
  an "Uninstall instead" button that starts the installed uninstaller.
- Locks: the installer lets go of its lock while the old uninstaller,
  which takes the same lock, runs, and takes it again after.

Phase 6a:

- [x] Integration tests: install 0.1.0, then install 0.2.0, which drops one
      file. The new installer gathers the parameters (spec 002 D3a), stops
      the services, and runs the 0.1.0 uninstaller with `--upgrade` (R9e).
      The dropped file is removed, kept paths and parameter values survive,
      and the receipt is the 0.2.0 one. Repair restores a deleted file.
      Downgrade asks for confirmation. (`TestAnUpgradeRemovesWhatTheOldVersionHadAndKeepsKeptData`,
      `TestARepairPutsBackADeletedFile`, `TestADowngradeIsDoneOnlyWhenAskedFor`.)
- [x] A secret parameter survives an upgrade, read back from the existing
      config file; the person is not asked again.
      (`TestASecretSurvivesAnUpgradeWithoutBeingAskedFor`.)
- [x] An uninstaller without `--upgrade` is run with `--quiet`, and the
      upgrade works. (`TestAnUninstallerWithoutUpgradeIsRunQuietly`.)
- [x] Uninstall hooks see `FYNSTALL_UNINSTALL_REASON=upgrade` during an
      upgrade. (`TestUninstallHooksAreToldWhyTheyRun`.)
- [x] A system-scope upgrade runs in one helper (integration, with the
      stand-in for `pkexec`). (`TestASystemUpgradeRunsInOneHelper`.)
- [x] The wizard's first page for an installed app says upgrade, repair or
      downgrade; downgrade waits for its confirmation check (headless).
      (`TestTheWizardSaysWhatItDoesToAnInstalledVersion`.)
- [x] Desk check: upgrade the installed Hello from Dolphin through the
      wizard. The launcher entry stays, and the version in Hello's About
      section changes.

Phase 6b:

- [ ] First consumer: a `fynstall.yaml` for clockwork-orange that produces the
      same files as its `install.sh` (two binaries, `.desktop`, seven icon
      sizes, the `--no-gui` equivalent as a component). The two results are
      compared with `diff` of file lists in two temp homes.

### Phase 7: Windows (R18, R19, R9f, registry restore in R9a/R9d)

- [ ] `fynstall build --target windows/amd64` from Linux produces an `.exe`
      with the icon embedded as a resource.
- [ ] Desk check in the win11-kvm VM. Double-click opens the wizard with no
      console window left open. From `cmd` and PowerShell the CLI runs and
      prints. A per-user install appears in Start and in Settings > Apps.
      Uninstall from Settings > Apps runs the installed `uninstall.exe` and
      removes it. A registry export of HKCU (HKLM for system) before install
      and after uninstall is identical (`reg export` + `fc`). The install
      directory is gone after a reboot (R9f). A system install shows one UAC
      prompt.
- [ ] `--cli-only` Windows installer works over SSH or in a plain console.
- [ ] The same `examples/hello` config, unchanged, builds the Windows
      installer: per-target payloads (spec 002 D1a) and platform
      equivalents of existing keys (L10), with no `platform:` block.
- [ ] Windows backends for the actions (spec 002 D2a: `service` through the
      SCM, with recovery actions) and spec 002 L1, L2, L6, L7 and L8.

### Later (not this spec)

macOS `.app` bundle into `~/Applications` or `/Applications`, and
notarisation. Payload compression. Auto-update. A file association and
URL-scheme registration. (Authenticode signing and install-time actions,
listed here at first, moved to spec 002.)

## Test Strategy

- **Unit**: config parsing and validation, placeholder resolution,
  manifest generation, mode selection table, icon resize, plan computation.
- **Integration (load-bearing)**: every engine requirement is tested by
  building a real installer from `examples/hello` and running it as a real
  process against a temporary `HOME`. The filesystem, `go build` and the
  installer are not mocked. The only fake is the elevation step, where the
  helper is started without `pkexec`. Real elevation is covered by the
  phase 5 desk check.
- **Headless GUI**: `wizard.Headless` with `fynetest`, driving the real
  engine into a temporary `HOME`.
- **Desk checks**: one per phase, recorded in the PR, for what a headless test
  cannot see: the launcher, double-click behaviour, pkexec and UAC prompts.

## Risks & Assumptions

- **Fyne needs cgo.** Every GUI build needs a C toolchain at build time. The
  end user does not. The `--cli-only` build is the escape from cgo, not a
  replacement for the GUI.
- **Headless start-up (phase 4 probe).** A binary that links libGL
  dynamically may fail at load on a machine without it, before `main`
  runs. Mode selection cannot help in that case. The phase 4 probe records
  the actual behaviour. The `--cli-only` installer is the answer for servers.
- **Binary size.** `embed` stores files uncompressed, and the installer is
  about the size of the payload plus about 25 MB of runtime. This is
  acceptable for the prototype. Compression is listed under Later.
- **Runtime version skew.** The generated installer is built against the
  builder's runtime version (R5). A builder installed with `go install` from
  a dirty tree has no proper version. In that case `build` requires
  `--runtime-path`.
- **Dolphin and executables.** KDE asks whether to run or open an executable
  file, and a file downloaded through a browser has no execute bit. The
  README states `chmod +x` for downloads. A wrapper is out of scope.
- **pkexec and Wayland.** The helper design (R13) avoids running a GUI as
  root, which pkexec does not support on Wayland.
- **Windows console flash.** A console-subsystem binary started from Explorer
  shows a console briefly before R19 releases it. The alternative
  (GUI-subsystem binary with `AttachConsole`) gives broken prompt and output
  ordering in `cmd`. The flash is accepted, and phase 7 decides whether to
  revisit it.
- **Security.** The installer writes only inside the planned paths. It
  rejects any destination that resolves outside its root (`..`, absolute
  `dst`, symlink escape) both at `validate` time and in `Apply`. It runs
  payload content only through a declared `run` action (spec 002 D2a),
  which is shown on the summary page and in `--dry-run` and has an undo.
  Hashes are checked on extract.
- **A crash during Apply leaves no receipt.** The journal is held in
  memory and written into the receipt at the end. If the process is killed
  mid-install, nothing records what it wrote, and no uninstaller exists yet.
  An interrupt (Ctrl+C) is safe, because it cancels the context and Apply
  undoes its work. Writing the journal to disk as it grows would close the
  rest of the gap; that is not done in phase 1.
- **Plan-to-Apply race.** The containment check (no symlink may carry a
  write outside the install directory) runs when the plan is made. A
  directory swapped for a symlink between the plan and the write is not
  caught. For a per-user install, the only party able to do that already
  owns the files. For system scope, phase 5's helper must plan and check
  again as root (R14), and should write through `os.Root`.
- **The receipt is trusted.** The uninstaller removes and restores the paths
  its receipt lists. A receipt the user can edit can therefore make the
  uninstaller delete any file that user owns, which the user can do anyway.
  A system install keeps its receipt in a root-owned directory, so this
  holds there too.
- **Rollback of this project's own changes.** Every phase is additive. An
  installed app is removed by its uninstaller. If the uninstaller is lost,
  the receipt lists every path to remove by hand.

## Alternatives Considered

- Considered a prebuilt runtime stub with the payload archive appended to
  the binary, so no Go toolchain is needed at build time. Rejected because
  the requirement is Go embedding, and code signing tools reject trailing
  data.
- Considered running the whole installer as root under pkexec for system
  scope. Rejected because Fyne under pkexec on Wayland has no display access,
  and a GUI running as root is a larger attack surface.
- Considered NSIS or Inno Setup for Windows. Rejected because it would give
  two installer formats and two UIs for one product.

## Verification

Filled in as each phase lands, with the desk check results.

### Phase 0 (2026-10-08)

On CachyOS with KDE Plasma 6 on Wayland. `make build`, `make hello`,
`make test` (with `-race`), `make lint` (golangci-lint v2.12.2, 0 issues)
and `make vuln` (no vulnerabilities) pass.

- `bin/hello` opens a window titled "Hello 0.1.0" with the Hello and About
  sections. About shows the icon at 72 px.
- On Wayland the taskbar and title bar show a generic icon. KDE matches a
  Wayland window to a desktop entry by `app_id` and does not use the icon
  the program sets; fynedesygn records this in `docs/design-system.md`
  (Platform). No `io.ushineko.hello.desktop` exists before phase 2.
- With `FYNE_PLATFORM=x11` (XWayland) the title bar and taskbar show the
  icon, which confirms the program sets it correctly.

### Phase 1 (2026-10-08)

Same machine. `make test` (with `-race`), `make lint` (0 issues) and
`make vuln` (no vulnerabilities) pass. With a cold build cache the
integration tests take about 105 seconds, most of it compiling Fyne for
`examples/hello`; with a warm cache, about 2 seconds.

Two mutations were checked: with `restore` disabled, the two restore tests
fail; with `--uninstall` routed to the current engine, the drift test fails.

Desk check, in the user's real home directory:

- `--dry-run` listed 13 changes: 8 directories, 3 files with modes 755,
  644 and 755, the index entry and the receipt. It changed nothing.
- `--cli` under the shell's `!` prefix, which has no terminal, refused with
  "There is no terminal to ask questions on. Run with --yes". The first
  version of the terminal check treated `/dev/null` as a terminal; it now
  uses the `TCGETS` ioctl.
- In Konsole: an interactive install, Hello started from the install
  directory, then `uninstall`. The user reports both ran without problems.
- Afterwards, the top-level listings of `~/.local/share`, `~/.config` and
  `~/.local/bin` matched those taken before the install.
  `~/.local/share/io.ushineko.hello` and `~/.local/share/fynstall` did not
  exist. `~/.config/io.ushineko.hello/settings.json`, a kept path that Hello
  rewrote while it ran, was still present with its sha256 unchanged.

### Phase 6a (2026-10-09)

On CachyOS with KDE Plasma 6 on Wayland. `make test` (with `-race`),
`make lint` (0 issues) and `govulncheck` (no vulnerabilities) pass.

- From Dolphin, the Hello 0.1.0 installer installed it for the user, and
  Hello's About section said 0.1.0. A double-click on the 0.2.0 installer
  opened on "Upgrade" ("Upgrades Hello 0.1.0 to 0.2.0"), and installed.
  Hello started from the same launcher entry, and About said 0.2.0. The
  index then said 0.2.0; the uninstall removed the install, the link, the
  launcher entry and the index.
- Found on the way: the plan for an upgrade refused the old version's own
  `~/.local/bin/hello` link as resolving outside `{bin}`. A path the old
  uninstaller removes now has only its directory checked.
- A test from phase 1 expected a second install to be refused; under R17
  it is a repair through the installed uninstaller, and the test says so.

### Phase 5 (2026-10-09)

On CachyOS with KDE Plasma 6 on Wayland. `make test` (with `-race`),
`make lint` (0 issues) and `govulncheck` (no vulnerabilities) pass. A
listing of `/opt`, `/usr/local/bin`, `/usr/local/share`, `/etc`,
`/etc/systemd/system` and `/var/lib` taken before the desk check matched
the one taken after it.

- GUI install: the wizard asked "Install for", and with "Everyone on this
  computer" showed one pkexec prompt. `ps` showed the wizard running as
  the user. The install was root-owned in `/opt/io.ushineko.hello`, with
  `/usr/local/bin/hello`, the launcher entry in
  `/usr/local/share/applications` and the index in
  `/var/lib/fynstall/installs`; nothing went into the home.
- GUI uninstall from the launcher's "Uninstall Hello", with a file made as
  root in `bin/`: one pkexec prompt, then the leftovers question; "Remove
  them too" removed it with no second prompt, and the whole install was
  gone, `/usr/local/share/icons` included, which the install had created.
- Found on the way: the first run of that uninstall said the leftover was
  removed, and it was not. The window had started the helper with the
  confirm job's context, which the window cancels when the job returns;
  that closed the helper's input, which the helper reads as "keep them".
  The helper now outlives the job, and a removal counts only when the
  helper confirms it (`TestTheUninstallWindowKeepsItsHelperForTheAnswer`,
  which fails with the old code).
- CLI install and uninstall with `--scope system` went through `sudo`.
  This machine's sudoers has `NOPASSWD: ALL`, so `sudo` asked for nothing;
  the output said the change needed an administrator, and the files were
  root-owned.
- `examples/beacon` with `--scope system`: `beacon.service` in
  `/etc/systemd/system`, enabled and running as root in `system.slice`;
  the run action wrote `/etc/beacon/setup-done`. The uninstall ran the
  hook, then the teardown, removed the service, then the files.

### Phase 4 (2026-10-08)

Same machine. `make test` (with `-race`), `make lint` (0 issues) and
`govulncheck` pass. fynedesygn v0.1.94.

- In `debian:stable-slim` the full installer does not start
  (`libGL.so.1: cannot open shared object file`, exit 127); the CLI
  variant installs and uninstalls.
- The desk check found three problems, each fixed in this phase:
  - The uninstaller beside the installer in `dist/`, double-clicked, failed
    with an error on stderr only and showed nothing. It now hands over to
    the installed uninstaller, or says the program is not installed, in a
    window.
  - The uninstaller was a three-page wizard and the CLI asked y/N. The user
    asked for one question in the window and none on the command line:
    fynedesygn spec 062 added `wizard.RunConfirm`, and `--yes` skips the
    question.
  - Installing 20,000 files made the fixed-size window grow from 820 px to
    977 px in steps. A real-window probe traced it to the log pane's line
    counter ("1000 line(s), 19000 older dropped"). fynedesygn spec 065
    (v0.1.94) fixed it; the same probe then showed the window at 820 px for
    the whole job. A first explanation, the wrapped log rows, was measured
    and dropped.
- After the fixes, the user reports:
  - the Hello installer from Dolphin, with Launch now, starting Hello;
  - Uninstall Hello from the launcher, asking once and reporting;
  - the `dist/` uninstaller with nothing installed showing "not installed";
  - the 20,000-file installer counting to the end with no movement of the
    window;
  - its CLI uninstaller removing 20,001 files without asking, in 182 ms.
- Afterwards, the listings of `~/.local/share`, `~/.config`, `~/.local/bin`
  and `~/.local/share/applications` matched those taken before.
- A real CPython 3.14 runtime (10,505 files, 288 MB, 54 symlinks) could not
  be packaged because of its symlinks. A dereferenced copy built in 1.5 s
  into a 275 MB installer (the linker stores identical files once),
  installed in 1.5 s and ran. Its uninstaller left the `.pyc` files Python
  wrote at runtime. Symlinks and the leftovers policy go to spec 002.

### Phase 3 (2026-10-08)

Done in fynedesygn: spec 061, PR #182, release v0.1.91. Its desk check is
in that spec. Two crashes that only a real window shows were found there and
fixed: widgets touched before the app existed, and a nil scroller measured
by `SetContent`. Both bear on phase 4, where fynstall builds its pages from
a manifest before `Run` creates the app.

### Phase 2 (2026-10-08)

Same machine. `make test` (with `-race`) and `make lint` (0 issues) pass.
`govulncheck` first reported five advisories in `golang.org/x/net` v0.59.0,
an indirect dependency through Fyne that no fynstall code calls; it was
raised to v0.60.0, after which govulncheck reports none.

Desk check, in the user's real home directory, with an installer built
from this branch:

- The install printed `==> Linking` and no PATH note, since `~/.local/bin`
  is on the user's PATH.
- "Hello" appeared in the KDE launcher with its icon, without a logout.
- Started from the launcher, the window showed the icon in the taskbar on
  Wayland, which the phase 0 desk check could not.
- `hello` ran from a terminal through the `~/.local/bin` link.
- After `uninstall`, the launcher no longer listed Hello, without a logout.
- Afterwards, the listings of `~/.local/share`, `~/.config`, `~/.local/bin`,
  `~/.local/share/applications` and the seven hicolor `apps` directories
  matched those taken before the install. `mimeinfo.cache` kept its sha256.

### Plan

- Environment: this machine (CachyOS, KDE Plasma 6, Wayland), a container
  with no graphics libraries (phase 4), and the win11-kvm VM (phase 7).
- Steps: the desk check of each phase, in order, with `examples/hello`, then
  the clockwork-orange comparison in phase 6.
- Expected result: each desk check passes as written, and the file-list
  `diff` in phase 6 is empty.
- Coverage: each desk check lists the requirements it covers in its phase
  heading.
