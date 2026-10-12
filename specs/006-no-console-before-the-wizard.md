# Spec 006: no console before the wizard

**Issue**: [#40](https://github.com/ushineko/fynstall/issues/40)

## Status: INCOMPLETE

## Executive Summary

On Windows the installer with the wizard, and its uninstaller, showed a
console window for a moment when started from the desktop. Two changes
remove it. Every such program now asks Windows for no console of its own,
which Windows 11 24H2 and later honour. And `fynstall build --windowed`
makes them programs with a window, which have no console on any Windows,
at the cost of the questions on a terminal. The tests pass on Windows. The
desk checks are open.

## Context

The installer is one file that is both the wizard and the command line. It
is a console program so that a terminal waits for it and shows what it
prints. Windows makes a console for a console program started from
Explorer, the Start Menu or Settings, and the installer closes it when it
opens the wizard (spec 001 R19). Spec 001 recorded the flash as an accepted
risk.

hayami's installer, the first one a person other than its author runs, was
reported for it. An installer that replaces a production one
([#6](https://github.com/ushineko/fynstall/issues/6)) cannot have it: the
NSIS installers it would replace are programs with a window, driven from a
command line with `/S`, and they print nothing.

## Requirements

- R1 The application manifest of a program with the wizard carries
  `consoleAllocationPolicy` `detached`. Windows 11 24H2 and later then make
  no console for it when it is started from the desktop, and it still
  shares the terminal it is typed into. An older Windows ignores the line.
  A command-line-only program does not carry it: started from the desktop,
  its console is the only place it can say anything.
- R2 `fynstall build --windowed` links the installer with the wizard and
  its uninstaller as programs with a window (`-H=windowsgui`). It changes
  nothing on another platform, and never the command-line-only variant.
  With `--cli-only` it is an error.
- R3 A windowed program started from a console borrows that console's
  output, where it has none of its own, so `--dry-run`, `--help`,
  `--version` and an error are seen. Output sent to a file or a pipe goes
  there. It does not take the keyboard: the shell does not wait for a
  program with a window, so it asks nothing, and a question it would need
  answered stops it with the message that names `--yes`.
- R4 Mode selection is unchanged (spec 001 R11, R19). A windowed program
  has no terminal on its input, so without a flag that asks for the command
  line it opens the wizard, wherever it was started.
- R5 `README.md` and `docs/platforms.md`.

## Acceptance Criteria

- [x] R1 The manifest of a program with the wizard has the policy, and one
      without the wizard does not
      (`TestAProgramSaysWhatItIsAndAsksForNoMoreRights`).
- [x] R1 Two programs, one with the manifest line and one without, started
      as Explorer starts them on Windows 11 build 26200: the first had no
      console, the second had one.
- [x] R2 The linker's flags have `-H=windowsgui` for the wizard's variant on
      Windows with `--windowed`, and in no other case (the same test).
- [x] R2 hayami's installer built `--windowed --with-cli-only`: the
      installer and uninstaller are programs with a window, the
      command-line pair are console programs (read from their headers).
- [x] R3 Through a pipe the windowed installer prints `--version` and the
      plan of `--dry-run --yes`; waited for, it returns its exit code (0,
      and 2 for a flag it does not have).
- [ ] Desk check: the default installer, double-clicked on Windows 11 24H2
      or later: no console is seen.
- [ ] Desk check: a `--windowed` installer, double-clicked: no console is
      seen. From `cmd` and from PowerShell, `--dry-run --yes` prints the
      plan in that console.
- [ ] Desk check: the uninstaller of a `--windowed` install, from
      Settings > Apps: no console is seen.
- [ ] A `--windowed` installer on a Windows older than 11 24H2. No such
      machine is at hand.

## Risks & Assumptions

- **What `--windowed` gives up.** The shell does not wait for the program:
  what it prints arrives after the prompt has come back, and a script needs
  `start /wait` or `Start-Process -Wait` for its exit code. It asks nothing
  on a terminal. The command-line-only installer is the one for a terminal,
  for SSH and for anything that reads the output. This is how an NSIS
  installer behaves, and it is why the option is not the default.
- **The default still flashes on an older Windows.** The manifest line is
  all the default has, and Windows 10 and Windows 11 before 24H2 ignore it.
- **A program with the wizard, started with `CREATE_NEW_CONSOLE` on
  purpose,** was not tried with the manifest line.
- **Rollback.** Remove the flag from a build, and the installer is the
  console program it was. The manifest line can be taken out of
  `exeManifest` on its own.

## Verification

2026-10-11, Windows 11 Pro build 26200: `go test ./builder/` passes with the
test above. The probes and hayami's `--windowed` build are as the criteria
say. `go test ./...` on both platforms, `make lint` and the desk checks are
recorded here when they are run.
