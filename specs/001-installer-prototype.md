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

Settled for 6b (2026-10-09): the comparison is the point; the gaps it
finds are listed, and the one the maintainer chose is closed here (the
launcher entry's keys). Optional components (`--no-gui`) and a version set
by the build are listed under Later. The `fynstall.yaml` stays with this
spec's Verification; moving it into clockwork-orange is that project's
decision.

- [x] First consumer: a `fynstall.yaml` for clockwork-orange that produces the
      same files as its `install.sh` (two binaries, `.desktop`, seven icon
      sizes), compared with `diff` of file lists in two temp homes, each
      difference fixed, accepted or listed under Later. The `--no-gui`
      equivalent needs optional components, listed under Later.
- [x] The launcher entry takes `generic_name`, `keywords`,
      `startup_notify` and `startup_wm_class` (default: the entry's ID),
      and clockwork-orange's entry is reproduced key for key
      (`TestTheLauncherKeysAreWrittenAsGiven`,
      `TestADesktopEntryIsMatchedToItsWindowByItsID`, and
      `desktop-file-validate`).

### Phase 7: Windows (R18, R19, R9f, registry restore in R9a/R9d)

Several PRs, as phases 4 and 6 were. 7a is a per-user install and uninstall
from the command line, and the tests running on Windows. 7b is the registry
journal (the Uninstall entry, `PATH`), the Start Menu shortcut and the
`.ico`, with the icon as a resource of the installer's own `.exe`. Proposed
for the rest: 7c the wizard, mode selection (R19) and the helper under UAC
with system scope; 7d services, permissions, the leaf requirements, and the
version resource and manifest of the `.exe` (L7), which go through the
writer of resource objects that the icon uses.

Chosen for 7a (2026-10-10). Each is for the maintainer to confirm in the
PR; none changes the config format or the receipt.

- Windows work is done on a Windows 11 machine, not cross-built and run in
  a VM, so the integration tests run a real installer natively. A Windows
  target still builds from Linux with `--cli-only`, which needs no C
  compiler.
- Per-user paths: `{home}` is `%USERPROFILE%`, `{data}` is `%LOCALAPPDATA%`
  and `{config}` is `%APPDATA%`, each falling back to the profile as the
  XDG variables fall back to `HOME`. The index is
  `{data}\fynstall\installs`, by the same rule as on Linux.
- There is no `{bin}` on Windows: a link becomes a `PATH` entry (L10), so
  no directory of links exists to name.
- R9f: the uninstaller moves its own running file to `%TEMP%` (Windows
  allows a rename of a running program on the same volume), removes the
  now empty install directory in the same run, and asks for the moved file
  to be deleted at the next start, which only an administrator is allowed.
  The install directory is gone when the uninstaller exits, not after a
  reboot, and an upgrade needs no wait: the old uninstaller has finished
  when it exits. For a normal user the moved file stays in `%TEMP%`, as
  Inno Setup's does. Not chosen: a copy run from `%TEMP%` that removes the
  original after it exits, because the caller would see the exit before
  the directory is gone.
- L3: the lock is held by the mutex existing, not by a thread owning it,
  so a goroutine that changes thread does not lose it.
- What 7a leaves out is said, not silent: system scope is refused with a
  message, and an installer whose config has launcher entries, icons or
  links says they are not applied on this platform yet (spec 002).

Open after 7a, and decided by the maintainer on 2026-10-10 (see "Decided
after 7b" below):

- The default install directory. `{data}/{id}` gives
  `%LOCALAPPDATA%\io.ushineko.hello`, and R18 says
  `%LOCALAPPDATA%\Programs\<name>`. `/opt/{id}`, the system default, is not
  a path on Windows. One option is a `{programs}` placeholder that is
  `{data}` and `/opt` on Linux, so today's defaults keep their meaning, and
  `%LOCALAPPDATA%\Programs` and `%ProgramFiles%` on Windows.
- `examples/hello` names `bin/hello`, which is `bin/hello.exe` on Windows:
  "the same config, unchanged" needs `{exe}` in that config.

Phase 7a:

- [x] `fynstall build --cli-only --target windows/amd64` produces an `.exe`
      installer and uninstaller, and one `examples/greet` config builds all
      three of its targets from one machine
      (`TestOneConfigBuildsEveryTargetFromThisMachine` on Windows; the
      Linux half, `TestOneConfigInstallsEachTargetsOwnPayload`, is extended
      and compiles, but has not run: see Verification).
- [x] Install, then uninstall, leaves the profile as it was, and the
      installed `uninstall.exe` is the artifact in `dist`
      (`TestInstallWritesThePayloadAndTheUninstallerRemovesIt`).
- [x] R9f for a per-user install: the install directory is gone when the
      uninstaller exits (the same test, and every uninstall test's
      snapshot).
- [x] The uninstaller puts back a file the install replaced
      (`TestUninstallRestoresAFileTheInstallReplaced`).
- [x] Upgrade, repair and downgrade per-user: the old `uninstall.exe` is
      probed and run, and the lock passes to it and back
      (`TestANewerInstallerRemovesThroughTheInstalledUninstaller`,
      `TestAnUpgradeKeepsTheParametersItWasGiven`).
- [x] Leftovers are listed, or removed with `--remove-leftovers`
      (`TestLeftoversAreListedOrRemoved`).
- [x] The install lock is a named mutex
      (`TestTheLockRefusesASecondHolderUntilReleased`, which now runs on
      both platforms).
- [x] Builds are reproducible on Windows (`TestBuildsAreReproducible`).
- [x] System scope is refused, and a config that offers it still installs
      per-user (`TestSystemScopeIsRefusedAndPerUserStillInstalls`).
- [x] `go test ./...` passes on Windows. Tests of Linux behaviour (launcher
      entries, systemd, `pkexec`, the wizard) are built for Linux only.
- [ ] Desk check on Windows 11, in a console: the installer asks its
      questions and reads a secret without showing it; `--dry-run` lists the
      plan; the uninstaller, run from another directory, leaves no install
      directory.

Chosen for 7b (2026-10-10), for the maintainer to confirm in the PR. The
config format does not change. The receipt gains three journal operations
(`reg_key`, `reg_value`, `path`) and one field (`reg`), all additive; a
receipt written on Linux is unchanged.

- The plan holds the shortcuts, the registry keys with their values, and
  the `PATH` directories. `--dry-run` and the install both list them.
  Apply writes them in a step of their own, "Registering with Windows",
  after the files and before the actions.
- The registry journal: each key that Apply makes is an entry, outermost
  first, and the uninstaller removes it only when it is empty. Each value
  is an entry with the kind and content it had before, or a mark that it
  was not there. A value of a kind other than a string or a 32-bit number
  stops the install: the journal could not put it back.
- `PATH` is not restored to its earlier content. Other installers change
  it between this install and its uninstall, and writing the old content
  back would remove their entries, which is the NSIS hazard spec 002 names.
  The journal records the one directory added; the uninstaller takes that
  entry out and leaves the rest, as Windows Installer does. A directory
  already on `PATH` is not added and so never removed. This reads the
  carried criterion "the uninstall restores `PATH` as it was" as "as it
  would be without this install".
- The shortcut is written through IShellLink, as R18 says, by calling the
  object's method table from Go. That needs no C compiler, so the CLI-only
  build keeps `CGO_ENABLED=0`, and no new dependency. Not chosen: writing
  the `.lnk` format by hand, which would make the content known at plan
  time but would be this project's own reading of a shell format.
- The icon is one `.ico` (16, 32, 48 and 64 as bitmaps, 256 as a PNG),
  made by the builder and carried as a payload file at
  `.fynstall/app.ico`, so it is hashed, journalled and removed like any
  file. A Windows manifest has no hicolor icons.
- A shortcut is named for the entry's `name`. Launcher keys with no meaning
  in a shortcut (`categories`, `keywords`, `terminal` and the rest) are not
  used on Windows.
- `UninstallString` runs the installed `uninstall.exe` (R9e), with `--gui`
  in a full build, as the Linux launcher action does;
  `QuietUninstallString` adds `--quiet`.
- Tests write the registry under a root of their own
  (`FYNSTALL_TEST_REGISTRY_ROOT`, a key below `HKCU`), as system paths go
  under `FYNSTALL_TEST_SYSTEM_ROOT` on Linux. Unlike that variable it is
  honoured in an elevated process: see Verification for why. The helper of
  a system install must not take it from the process that starts it (7c).

Decided after 7b (2026-10-10). The maintainer answered the open points:
`PATH` is edited, as chosen above; the installer's `.exe` needs the icon
now, not in 7d; `{programs}` and `{exe}` in `examples/hello` go ahead.

- `{programs}` is a new placeholder: where the platform keeps installed
  programs. It is `{data}` for a per-user install on Linux and `/opt` for a
  system install, and `%LOCALAPPDATA%\Programs` for a per-user install on
  Windows (`%ProgramFiles%` comes with system scope in 7c). The default of
  `install.dir` is `{programs}/{id}` for both scopes. On Linux that is the
  directory the old defaults gave, and a config that names `{data}/{id}` or
  `/opt/{id}` means what it did. The change to the config format is
  additive.
- The directory is named for the ID, not for the name as R18 says: the
  default is one template for every platform, and the ID is what names the
  directory on Linux. A config that wants `Programs\Hello` says
  `{programs}/{name}`. For the maintainer to confirm.
- `examples/hello` names `bin/hello{exe}` and `{programs}/{id}`, and
  `make hello` writes `hello.exe` on Windows. The one config builds the
  Linux installer and the Windows one.
- The icon of the `.exe`: the builder writes a COFF object with a `.rsrc`
  section (each image of the `.ico`, and the group that lists them) into
  the directory of each generated program, and the Go linker puts it in the
  `.exe`. The builder writes the object itself, so a build from Linux needs
  no resource compiler and the CLI-only build still needs no C compiler.
  Both the installer and the uninstaller carry it. The version resource and
  the manifest (L7) are more entries for the same writer, in 7d.

Phase 7b:

- [x] Every install has an Uninstall registry entry with the L8 fields,
      and its two commands run the installed `uninstall.exe`
      (`TestAnInstallIsInTheStartMenuInSettingsAndOnPath`,
      `TestAnInstallRegistersWithWindowsAndTheUninstallTakesItBack`).
- [x] `integration.desktop` makes a Start Menu shortcut, which the Windows
      shell reads back with the target, arguments, directory, description
      and icon it was given (the first of those tests, through
      `WScript.Shell`).
- [x] `integration.path_links` adds the program's directory to the user's
      `PATH`; the uninstall takes out that entry and leaves the rest, and a
      directory already there is neither added nor removed
      (`TestUninstallTakesItsOwnEntryOutOfPathAndLeavesTheRest`,
      `TestADirectoryAlreadyOnPathIsNotAddedOrRemoved`).
- [x] After an uninstall the registry and the files match their state
      before the install: every Windows integration test compares both
      (`regtest.Snapshot`), including with a value and a shortcut that were
      there before (`TestRegistryValuesThatWereThereArePutBack`,
      `TestUninstallPutsBackTheShortcutAndLeavesTheRestOfPath`).
- [x] A failed install undoes its registry changes
      (`TestAFailedInstallUndoesItsRegistryChanges`).
- [x] A repair leaves one set of entries
      (`TestARepairLeavesOneSetOfRegistryEntries`).
- [x] `app.icon` becomes an `.ico` with five sizes, the same bytes for the
      same source (`TestAWindowsTargetGetsTheIconAsAnIcoFile`).
- [x] The installer and the uninstaller for a Windows target carry the
      icon as a resource: the images of the `.ico` and one group, read back
      from the built `.exe` files and from the installed `uninstall.exe`
      (`TestTheIconResourcesAreOneSectionTheLinkerCanPlace`,
      `TestAnInstallIsInTheStartMenuInSettingsAndOnPath`). Built on Windows;
      the build from Linux uses the same code and has not run.
- [x] `{programs}` resolves per platform and scope, and is the default
      install directory (`TestUserVarsComeFromTheProfile`, and on Linux
      `TestUserScopeFollowsXDGAndIgnoresRelativeValues`,
      `TestSystemScopeIsUnderUsrLocalAndHasNoHome` and
      `TestTheTestSystemRootMovesEverySystemPathButNeverForRoot`, which
      compile and have not run).
- [x] `examples/hello`, unchanged, builds a CLI-only Windows installer on
      Windows. The full installer waits for the wizard (7c).
- [ ] Desk check on Windows 11, in the real profile: the program is in
      Start with its icon and starts from there; it is in Settings > Apps
      with its name, version, publisher, size and icon, and Uninstall there
      removes it; a new console finds the program by name; `reg export` of
      `HKCU\Environment` and of the Uninstall key before the install and
      after the uninstall are identical (`fc`).

Phase 7c comes in two parts: the wizard and mode selection (this part),
then the helper under UAC with system scope.

Chosen for the first part of 7c (2026-10-10), for the maintainer to confirm
in the PR. Neither the config format nor the receipt changes.

- The installer stays a console program, as the risk "Windows console
  flash" accepts. Mode selection is R19: a process that is alone on its
  console was started from the desktop, opens the wizard and closes the
  console. The console's existence no longer means a person typed the
  command, so `Env.OwnConsole` takes it out of "interactive" for the
  choice, and leaves it in for a command line that was asked for with
  `--cli`.
- "A display" on Windows is a window station with a visible desktop, which
  a service and a session over SSH do not have.
- "Root" on Windows is an elevated process. An elevated installer uses the
  command line and refuses `--gui`, as the carried criterion says. On a
  machine where every process of the person is elevated (UAC off), the
  wizard is never offered; that is the cost of the rule.
- A program that the installer starts and waits for (the installed
  uninstaller, a `run` action) gets no console window of its own when the
  installer has none (`CREATE_NO_WINDOW`). With a console it shares the
  installer's, as before.
- The wizard offers the scopes this platform's backend has. Until the
  second part there is one on Windows, so the wizard does not ask who the
  install is for. A desk run found the alternative: with `examples/hello`,
  which offers both scopes, the wizard failed to resolve the system paths
  and the installer exited with nothing shown.
- A failure before the wizard opens is shown in a window on every
  platform. The same run found that it went only to stderr, which a program
  started from the desktop does not have.
- A full installer for Windows builds on Windows only, with `gcc` on
  `PATH`. A full build from Linux with `x86_64-w64-mingw32-gcc` (R18) is not
  wired in: the rule "a full installer builds only for this machine" still
  refuses it, and nothing here was able to try it.

Phase 7c, first part:

- [x] `fynstall build` on Windows makes the full installer and uninstaller,
      which link the graphics library, carry the icon through the C linker
      as through the Go linker, and install from a script with `--yes`
      (`TestAFullInstallerInstallsFromTheCommandLineToo`).
- [x] `UninstallString` of a full build opens the uninstaller's window
      (`--gui`), and `QuietUninstallString` does not (the same test, and
      `TestTheWizardInstallsWithItsParameters`).
- [x] The wizard installs, uninstalls, lists leftovers and replaces an
      installed version on Windows, against the real engine and a registry
      root of the test's own: the tests of `installer/gui_test.go` run on
      both platforms, and compare the registry with the files.
- [x] The wizard offers only the scopes the platform has
      (`TestTheWizardAsksWhoTheInstallIsFor`).
- [x] Mode selection for an administrator (`TestChooseMode`).
- [ ] Desk check on Windows 11: a double-click on the full installer opens
      the wizard and leaves no console window; the wizard installs and the
      finish page starts the program; Uninstall in Settings > Apps opens
      the uninstaller's window and removes the program; from `cmd` and
      PowerShell the installer runs on the command line and asks its
      questions; an elevated console gets the command line and a refusal of
      `--gui`.

Chosen for the second part of 7c (2026-10-10). The maintainer agreed the
first three before the work started; the rest are for the PR. Neither the
config format nor the receipt changes.

- The helper reports over named pipes that the person's process makes
  before it starts the helper through `ShellExecuteEx` with `runas`. There
  are two, one for each direction, because the parent stops the helper by
  closing its input and goes on reading its events; one pipe cannot be half
  closed. Not chosen: a file that the helper appends to, which has no way
  to cancel and none to hold the helper while the window asks about
  leftovers.
- The system install index is `%ProgramData%\fynstall\installs`.
- System paths: `{programs}` is `%ProgramFiles%`, `{data}` and `{config}`
  are `%ProgramData%`, the registry entries are under `HKLM`, and the
  directory goes on the machine's `PATH`. The folders are asked of Windows
  (known folders), not read from the environment.
- Any user can make files in `%ProgramData%`, and a later installer runs
  the uninstaller that the index names, as an administrator. So the helper
  makes the index file the administrators' own with a closed access list,
  and an installer takes an index file only when the administrators or the
  system own it. A user cannot give a file away to them.
- The tests run a system install under `FYNSTALL_TEST_SYSTEM_ROOT`, with
  the registry under the test's root and the helper started as a plain
  child (`FYNSTALL_ELEVATE=direct`). Both roots are honoured in an elevated
  process, because the tests run in elevated consoles. The caution written
  for 7b, that a real helper must not take these variables from the process
  that starts it, is met this way: a helper that is elevated when the
  process on the other end of its pipes is not drops all three. A system
  root without a registry root is refused, so no test can write `HKLM`.
- A process that is already elevated makes the changes itself. With
  `FYNSTALL_ELEVATE` set, it uses a helper all the same, so the tests cover
  the helper wherever they run.
- R9f for system scope is the per-user way: the running uninstaller moves
  itself to `%TEMP%`. The helper is an administrator, so the delete at the
  next start is accepted.
- Open, for the maintainer: a system install in a directory that users can
  write (`--dir` outside `%ProgramFiles%`) leaves `uninstall.exe` where a
  user can replace it, and an administrator later runs it. The docs say to
  keep system installs under `%ProgramFiles%`; nothing enforces it.

Phase 7c, second part:

- [x] A system install runs through the helper and its pipes: files under
      the machine's folders, the Uninstall entry and `PATH` under `HKLM`,
      the shortcut in the Start Menu of all users; a later installer finds
      it without `--scope` and repairs it; the installed uninstaller
      removes it through a helper of its own, and files and registry match
      their state before
      (`TestASystemInstallGoesThroughTheHelperAndComesOutAgain`).
- [x] System paths come from Windows, and the test root moves all of them
      and needs a registry root (`TestSystemVarsAreTheMachinesFolders`,
      `TestTheTestSystemRootNeedsARegistryRootAndMovesEverything`).
- [x] The index of a system install is trusted only from an administrator
      (`TestTheRecordOfASystemInstallIsTrustedOnlyFromAnAdministrator`).
- [x] The wizard asks who the install is for, with a location page for
      each scope (`TestTheWizardAsksWhoTheInstallIsFor`, now on Windows).
- [ ] Desk check on Windows 11, from a console that is not elevated:
      `--scope system` shows one UAC prompt, installs into
      `%ProgramFiles%`, and the program is in Start for another user and in
      Settings > Apps; closing the prompt changes nothing and says so; the
      uninstaller shows one prompt and leaves `HKLM` and the folders as
      they were (`reg export` and `fc`); the wizard does the same with
      "Everyone on this computer".
- [ ] A helper started through a real prompt ignores the test variables.
      Nothing here could make that case: it needs a process that is not
      elevated to start one that is.

The whole of phase 7:

A
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

Carried from the Linux phases (written 2026-10-09, before phase 7 starts).
Phases 4b to 6b built these on Linux; each needs its Windows half, and some
need a decision first.

- [ ] Spec 002 L4 permissions (moved here from phase 5): one `permissions`
      concept on a directory, with the roles `admins`, `service` and
      `users`, turned into owner, group and mode on Linux and into an ACL on
      Windows, journalled and restored. Decide first which account the
      `service` role is (LocalSystem, a virtual service account, or a
      `user:` on the service action) on both platforms.
- [x] Spec 002 L3 on Windows: the install lock is a named mutex per app ID
      and scope (`Local\` for per-user, `Global\` for system), with the same
      refusal message as on Linux. (Phase 7a; the `Global\` name is written
      and runs first with system scope.)
- [ ] The privileged helper under UAC (R13, R14): the installer and the
      uninstaller stay the person's own processes and run themselves with
      `--apply-plan` or `--apply-uninstall` through `ShellExecuteEx` with
      `runas`. Decide first how events come back: `ShellExecuteEx` gives no
      pipes, so the helper needs a named pipe (or a file) that only the
      person and the helper can open, and a way for the parent to cancel it,
      as stdin does on Linux. The plan file and its digest check are as on
      Linux, in a directory only the person can write.
- [ ] The window never runs elevated: an installer started as an
      administrator uses the command line, or refuses `--gui`, as on Linux.
- [ ] System paths and records: the install index for system scope (Linux
      uses `/var/lib/fynstall/installs`; decide `%ProgramData%\fynstall\installs`
      or the registry), the lock and the run directory, and what `{data}`,
      `{config}` and `{bin}` are per scope. `docs/platforms.md` gets a
      Windows section.
- [ ] R9f, the uninstaller removing itself: a running `.exe` cannot delete
      itself. Decide between `MoveFileEx` with `MOVEFILE_DELAY_UNTIL_REBOOT`
      (needs administrator rights for system scope) and a copy of the
      uninstaller run from `%TEMP%` that removes the original and then
      itself at reboot. The install directory must be gone after a reboot.
      (Phase 7a chose a third way for per-user scope, above: the running
      file is moved to `%TEMP%`. System scope, where the helper is an
      administrator and the delete at the next start is allowed, is still
      to do.)
- [ ] Upgrade, repair and downgrade (phase 6a) on Windows: the old
      `uninstall.exe` is probed with `-h` and run with `--upgrade` or
      `--quiet`, a running service is stopped through the SCM before its
      files are replaced, and a system upgrade asks for UAC once.
- [ ] The leftovers question and `--remove-leftovers` (spec 002 D5) on
      Windows, including the system case, where the elevated helper waits for
      the answer as it does on Linux.
- [ ] The Uninstall registry entry's `UninstallString` and
      `QuietUninstallString` (L8) run the installed `uninstall.exe`, so
      Settings > Apps goes through the installed uninstaller (R9e).
      (Per-user in phase 7b; HKLM comes with system scope.)
- [ ] Spec 002 L10 on Windows: `integration.path_links` adds the install's
      `bin` to the person's or the machine's `PATH`, recorded and restored
      like any registry value, and `integration.desktop` makes a Start Menu
      shortcut; the uninstall restores `PATH` as it was.
      (Per-user in phase 7b, with `PATH` edited rather than restored; the
      machine's `PATH` comes with system scope.)
- [ ] Payload symlinks become copies on a Windows target (spec 002 D4a,
      done in the builder): a desk check installs a payload that has links
      and runs the copied files.

### Later (not this spec)

macOS `.app` bundle into `~/Applications` or `/Applications`, and
notarisation. Payload compression. Auto-update. A file association and
URL-scheme registration. (Authenticode signing and install-time actions,
listed here at first, moved to spec 002.)

Found by the first consumer (phase 6b): optional components, so a person
can leave out part of the payload, such as clockwork-orange's window
(`install.sh --no-gui`); and an app version set by the build
(`fynstall build --app-version`), so a Makefile that derives its version
from git can pass it.

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

### Phase 7c, second part (2026-10-10)

Same machine. `go test -race ./...` passes, and golangci-lint v2.12.2
reports 0 issues. The CLI-only code and tests compile for Linux. The Linux
helper moved to a file of its own with its behaviour unchanged, and the
shared code now goes through `launchHelper`; nothing ran on Linux, so
`make test` there is the check that the move changed nothing.

By hand, with the CLI installer of `examples/hello`, a system root and a
registry root of the check's own, from an elevated console:

- With the helper started as a plain child: `--scope system --yes` exited
  0 and wrote the install under `<root>\Program Files`, the index and the
  shortcut under `<root>\ProgramData`, and the Uninstall key and `PATH`
  under the registry root's `HKLM`. The installed uninstaller exited 0 and
  left no file under the root and no key.
- The same through `ShellExecuteEx` with `runas`
  (`FYNSTALL_ELEVATE=prompt`): the same result. Windows shows no prompt to
  a process that is already elevated, so this ran the code that starts the
  helper and connects the pipes, not the prompt.
- After both, and after the test runs: no `HKLM` Uninstall key, no
  `%ProgramFiles%\io.ushineko.hello`, no `%ProgramData%\fynstall`, and the
  machine's and the user's `PATH` unchanged.

Not done, and not possible from an elevated console: a real UAC prompt,
its refusal, a helper running as a different administrator, and the rule
that such a helper drops the test variables. Nothing has been installed
into the real `%ProgramFiles%` or `HKLM`. No mutation checks were run.

### Phase 7c, first part (2026-10-10)

Same machine as 7a and 7b. `go test -race ./...` passes, and golangci-lint
v2.12.2 reports 0 issues. The CLI-only code and tests compile for Linux.
The wizard's code and `installer/gui_test.go` need cgo and were not
compiled for Linux here; that file changed (paths through `platform.Vars`,
a registry snapshot that is empty on Linux), so `make test` on Linux is the
first check of it.

By hand, with the full installer of `examples/hello`:

- Started as Explorer starts a program (through the desktop's shell, not
  elevated, with a console of its own): one process, one visible window
  titled "Hello 0.1.0", no console host left under it, and the window
  closed on request. Nothing was installed. Before the fix described in
  the phase list, the same start exited at once with nothing shown.
- From an elevated console: `--dry-run` listed the plan, and `--gui`
  exited 2 with "the window does not run as an administrator".

Not done: nobody has looked at the wizard's pages on Windows or clicked
through an install. The desk check in the phase list covers that.

### Phase 7b (2026-10-10)

Same machine as 7a. `go test -race ./...` passes, and golangci-lint
v2.12.2 reports 0 issues. The code and the tests compile for Linux; nothing
ran on Linux. `govulncheck` was not run; `golang.org/x/sys/windows/registry`
is a package of a module this project already requires.

A fault found while writing the tests, and what it changed. The registry
root for tests was first ignored in an elevated process, copying the rule
`FYNSTALL_TEST_SYSTEM_ROOT` has for root. The console the tests ran in was
elevated, so the first run of the new engine tests wrote to the real
registry of the person running them: two test directories were added to
the user `PATH`, and one `Uninstall\io.example.hello` key was left. Both
were removed by hand, and the `PATH` value was checked against a copy taken
before the repair. Three things changed: the root is honoured in an
elevated process; the engine fixture refuses to run an install unless its
registry keys resolve under the test root; and the integration tests refuse
to start a program without one.

Mutations were not run for this phase.

By hand, with an installer that has an icon, a launcher entry and a link,
against a scratch profile and a registry root of its own: `--dry-run` listed
the shortcut, the registry key with 10 values, the `PATH` entry and
`app.ico`; the install wrote them; Windows (`System.Drawing.Icon`) loaded
the `.ico` at 16, 32, 48 and 64 px with opaque centre pixels; the
uninstaller left the profile and the registry root empty. `System.Drawing`
does not read the 256 px PNG image of an icon file, so that size was checked
only by the builder test, which decodes it.

The desk check in the real profile, in the phase 7b list, is still to do:
it is the first run that writes the real Start Menu, Settings and `PATH`.

`{programs}`, `{exe}` in `examples/hello`, and the icon resource
(2026-10-10, same machine). `go test -race ./...` passes and the linter
reports 0 issues. The code and the tests compile for Linux; nothing ran on
Linux, and this change touches Linux behaviour in two places that only a
Linux run checks: the default install directory is now made from
`{programs}`, and `platform.Rooted` leaves a path that is already under the
test system root where it is. `make test` on Linux must pass before this
merges.

By hand: `fynstall build --cli-only` on `examples/hello`, unchanged, made
`hello-0.1.0-windows-amd64-cli-installer.exe` and its uninstaller. Windows
(`System.Drawing.Icon.ExtractAssociatedIcon`) returned the Hello icon for
both files. `--dry-run` listed the install under
`%LOCALAPPDATA%\Programs\io.ushineko.hello` with the shortcut, the registry
key and the `PATH` entry, and changed nothing.

The desk check in the real profile (2026-10-10, Windows 11 Pro, that
installer). The install exited 0 and wrote the install directory, the
shortcut `Hello.lnk`, the Uninstall key with its 10 values and one entry at
the end of the user `PATH` (14 entries became 15, the kind of the value
unchanged). The maintainer looked at the Start Menu entry and its icon,
the entry in Settings > Apps, the program by name in a new console and the
icon of the `.exe` files in Explorer, and reported all of them good.

The installed `uninstall.exe`, run from another directory, exited 0. After
it: `fc` of the `reg export` of `HKCU\Environment` taken before the install
and after the uninstall found no differences; the Uninstall key, the
install directory, the shortcut and `%LOCALAPPDATA%\fynstall` were gone;
the moved uninstaller was in `%TEMP%` as `fynstall-removed-<number>.exe`,
as R9f for per-user scope says.

Not exercised: Uninstall from Settings > Apps (the uninstall was run from a
console, so the criterion stays open), and the questions and the hidden
secret of the phase 7a console check.

### Phase 7a (2026-10-10)

On Windows 11 Pro, Go 1.27.0, with the MSYS2 UCRT64 gcc for the packages
that have a window. `go test -race ./...` passes, and the pinned
golangci-lint v2.12.2 reports 0 issues. `govulncheck` is not installed on
this machine and was not run; this phase changes no dependency.

Not run: anything on Linux. This machine has no Linux. The code and the
tests compile for Linux (`go vet` and `go test -run` nothing, with
`GOOS=linux`, `CGO_ENABLED=0` and `-tags nogui`), and the Linux
integration tests changed in one place, the greet build, which now has a
third target. Run `make test` on Linux before this merges.

One mutation was checked: with the uninstaller's move of its own file
disabled, seven of the Windows integration tests fail.

A Windows checkout with `core.autocrlf` gave every Go file CRLF line ends,
which `gofmt` and the linter report as unformatted. `.gitattributes` now
keeps Go sources LF.

By hand, with the greet installer against a scratch profile (no console,
so no prompts): `--dry-run` listed 10 directories, 3 files, the index
entry and the receipt; `--yes --verbose` installed them; the installed
`greet.exe` ran; `uninstall.exe --verbose` removed 4 files and left the
profile empty, with one `fynstall-removed-<number>.exe` of the
uninstaller's size in the temporary directory.

The console desk check in the phase 7a list is still to do.

### Phase 6b (2026-10-09)

clockwork-orange at `df6acba`, in a local clone (its own checkouts were
not touched). `install.sh` built both programs; the same two binaries were
the payload of this config:

```yaml
app:
  id: io.ushineko.clockwork-orange
  name: Clockwork Orange
  version: 0.0.0
  publisher: ushineko
  icon: packaging/icons/clockwork-orange-512x512.png
payload:
  - src: bin/clockwork-orange
    dst: bin/clockwork-orange
  - src: bin/clockwork-orange-gui
    dst: bin/clockwork-orange-gui
integration:
  path_links: [bin/clockwork-orange, bin/clockwork-orange-gui]
  desktop:
    - name: Clockwork Orange
      comment: Wallpaper and lock screen manager for KDE Plasma 6
      exec: bin/clockwork-orange-gui
      generic_name: Wallpaper manager
      categories: [Graphics]
      keywords: [wallpaper, desktop, background, lockscreen, kde, plasma]
      startup_notify: true
```

Each was installed into its own temporary home, and the file lists were
compared:

| | `install.sh` | fynstall | Verdict |
|---|---|---|---|
| Programs | copied into `~/.local/bin` | in `{data}/{id}/bin`, linked from `~/.local/bin` | accepted: the install directory holds what the uninstaller removes |
| Launcher entry | `io.ushineko.clockwork-orange.desktop` | the same name | the same |
| Entry keys | `GenericName`, `Keywords`, `StartupNotify`, `StartupWMClass` | none of them | fixed: the four keys; the entries now match key for key, apart from the next three rows |
| `Exec` | `clockwork-orange-gui` | the absolute path | accepted: works without `~/.local/bin` on `PATH` |
| `Icon` | `clockwork-orange` | the app ID | accepted: one name for the icon, the entry and the Wayland `app_id` |
| Uninstall | `uninstall.sh` in the checkout | uninstaller, record and index installed, an Uninstall action in the entry | fynstall's model |
| Icons | 7 sizes, 16 to 512 | the same 7 sizes | the same |
| Caches | `update-desktop-database` and `gtk-update-icon-cache` rewrite `mimeinfo.cache` and `icon-theme.cache` | `kbuildsycoca6` | accepted (R15): the entry declares no MIME types |
| Legacy entry | removes a Python-era `clockwork-orange.desktop` when it points at `clockwork-orange.py` | nothing | accepted: a conditional delete of a file the install did not make is what MSI does not do |
| `--no-gui` | installs the command line only | not possible | Later: optional components |
| Version | from git, by the Makefile | fixed in the config | Later: a version set by the build |

`desktop-file-validate` passed on fynstall's entry. The uninstall left the
home as it was, apart from KDE's menu cache in `.cache`.

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
