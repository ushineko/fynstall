# Spec 008: starting the program from the command line

**Issue**: [#48](https://github.com/ushineko/fynstall/issues/48)

## Status: COMPLETE

## Executive Summary

The command line can start the program an install has just put in place:
`--launch` starts it, and an install that asks questions asks this one last.
A new key, `ui.launch_default`, makes starting it the default in both front
ends: the wizard's check starts ticked and `--yes` starts the program. The
tests pass on Windows, and hayami's installer, built against this change,
started its panel after a headless upgrade.

## Context

`ui.launch` names a program for the installer to start when it is done.
Only the wizard did anything with it: its last page has a "Launch now"
check. The command line had no flag and no question, so an install with
`--cli` or `--yes` never started the program. A behaviour in one front end
and not the other is a bug by this repository's own rule.

The check also always started clear. For a program that is there whenever
the desktop is, such as a panel, a person who clicks through the wizard
ends with the panel gone.

It was found with hayami. Its installer stops the panel before the old
version is removed, and after a headless upgrade the panel stayed stopped
until somebody started it by hand: three upgrades in a row on one desk.

## Requirements

- R1 An installer whose manifest names a launch program takes `--launch`.
  `--launch` starts the program after a successful install, and
  `--launch=false` does not. An installer with nothing to launch has no such
  flag.
- R2 An install that asks questions on the terminal, and was given no
  `--launch`, asks "Start NAME now?" after the install is done.
- R3 `ui.launch_default: true` makes starting the program the default: the
  wizard's check starts ticked, the question of R2 is `[Y/n]`, and an
  install that asks nothing (`--yes`) starts the program. Without the key
  everything is as before. The key without `ui.launch` is refused by
  `validate`.
- R4 The program is started the way the wizard starts it: as the person at
  the desktop, through Explorer when the installer is elevated on Windows
  (spec 007). One function serves both front ends.
- R5 Two cases start nothing and say why. A session with no desktop passes
  over a launch that is only the default; one a person asked for, by flag
  or by answer, is tried, and the question's default answer there is no. A
  command line that runs as root, where no window runs as root, starts
  nothing whether asked or not.
- R6 A failed install, and one whose own after-install hook failed, starts
  nothing.
- R7 `--dry-run`, and the plan printed before an install that asks nothing,
  say "after the install, starts PATH" when the install would start the
  program.
- R8 A program the installer starts on Linux gets a session of its own, so
  it outlives the terminal the installer was typed into.
- R9 `launch` is a reserved parameter name, since each parameter is a flag.
- R10 `docs/config.md`, the README and `examples/hello`.

## Acceptance Criteria

- [x] R1 R2 R3 R5 The command line starts the program by the rule, in
      twelve cases (`TestTheCommandLineStartsTheProgramByTheRule`).
- [x] R1 R7 A real install with `--yes --launch` starts the installed
      program from where it was installed; the dry run names it and starts
      nothing (`TestAnInstallFromTheCommandLineStartsTheProgram`).
- [x] R1 The same install without the flag starts nothing and its plan does
      not say it would
      (`TestAnInstallFromTheCommandLineWithoutTheFlagStartsNothing`).
- [x] R1 An installer with nothing to launch starts nothing
      (`TestAnInstallerWithNothingToStartStartsNothing`).
- [x] R3 The wizard's check starts as the config says
      (`TestTheLaunchCheckStartsAsTheConfigSays`).
- [x] R3 R9 `validate` takes `launch_default` with `launch`, refuses it
      without, and refuses a parameter named `launch`
      (`TestParametersAndConfigFilesAreChecked`).
- [x] R4 The wizard's launch still goes through Explorer when elevated
      (`TestTheFinishPageLaunchesAfterAReplacement`, unchanged).
- [x] R5 A command line as root starts nothing
      (`TestACommandLineAsRootStartsNothing`; it runs off Windows only).
- [x] R3 R4 Desk check: hayami's command-line installer, built against this
      change with `launch_default: true` and run with `-cli -yes` from an
      elevated shell over an install whose panel was running: the panel is
      running afterwards, and not as an administrator.
- [ ] R8 Desk check on Linux: a program started by an installer typed into
      a terminal goes on after the terminal closes.

## Risks & Assumptions

- **A config with a parameter named `launch`** stops validating (R9). None
  is known. Its installer would otherwise fail to start, with two flags of
  one name.
- **`--yes` in a script now starts a program** where the config says
  `launch_default: true`. That is the key's meaning, and it is off unless a
  config turns it on. A script that must not start anything passes
  `--launch=false`.
- **No desktop is judged by the session**, with the test `--gui` already
  uses. On Linux that is `DISPLAY` or `WAYLAND_DISPLAY`, which an ssh
  session with forwarding also has.
- **Explorer passes no arguments** (spec 007), and the launch needs none.
- **Rollback.** Without `launch_default` and without `--launch` nothing
  changes. The manifest field is additive, and the receipt is not touched.

## Alternatives Considered

- **Starting the program again whenever the installer stopped a running
  copy.** fynstall does not stop programs; a program's own hook does
  (spec 003), and the installer cannot tell that it did. The default is a
  statement about the program, which the config can make.
- **Doing it in the program's own after-install hook.** The wizard would
  then start the program twice when its check is ticked.

## Verification

2026-10-13, Windows 11 Pro build 26200, in an elevated shell.

- `go test ./...` passes, apart from `TestACompilerOnPathIsAccepted`, which
  fails the same way on `main` on Windows: its stand-in compiler is a shell
  script. The module builds with and without `-tags nogui`.
- Falsified: with `--launch` ignored, six cases of the rule and the real
  install fail; with the check always clear,
  `TestTheLaunchCheckStartsAsTheConfigSays` fails.
- `make lint`'s linter reports nothing in the files this changes.
- hayami 0.10.2's command-line installer, built with `--runtime-path` at
  this change and `launch_default: true`, run as `-cli -yes` over an install
  whose panel was running. `--dry-run` printed "after the install, starts"
  and the panel's path. The install ended with "Started hayami.", and the
  panel was running again as a new process whose parent is Explorer.

The Linux desk check (R8) is open.
