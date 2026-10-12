# Spec 007: the wizard as an administrator

**Issue**: [#43](https://github.com/ushineko/fynstall/issues/43)

## Status: INCOMPLETE

## Executive Summary

On Windows the wizard now opens for an installer that runs as an
administrator. Before, such an installer fell back to a command line with
no terminal, and one built `--windowed` showed nothing at all. The program
started from the finish page is started as the person at the desktop, not
as an administrator. Linux is unchanged: no window runs as root. The tests
pass on Windows. The desk checks are open.

## Context

Spec 001 R13 kept the wizard from running with an administrator's rights:
the installer runs as the person, and asks for an administrator through a
helper when a system install needs one. An installer started with "Run as
administrator" therefore chose the command line, found no terminal, and
stopped with "There is no terminal to ask questions on". The uninstaller's
window was already let through on Windows, because Settings > Apps starts
it as an administrator.

People start installers as an administrator, out of habit and because other
installers want it. With `--windowed` (spec 006) the message has nowhere to
go, and the installer exits with code 2 and shows nothing. It was found
starting hayami 0.9.10's installer from an elevated shell.

## Requirements

- R1 On Windows the installer's wizard opens when the process is elevated,
  by the same rule as when it is not: `--gui`, or no terminal and a
  display. `--gui` is no longer refused there. The command line is chosen
  as before by `--yes`, `--dry-run`, `--cli` and a terminal.
- R2 Linux and the other platforms are unchanged. No window runs as root:
  as root the installer runs on the command line, and `--gui` is refused.
- R3 An elevated installer makes its changes itself, with no helper and no
  second prompt, as an elevated command line already does. The welcome page
  does not say that an install for everyone "asks for an administrator".
- R4 "Launch now" starts the program as the person at the desktop. An
  elevated installer asks Explorer to start it (`explorer.exe <program>`),
  which Explorer does with its own rights.
- R5 Where the installer runs as another account than the desktop's (a
  person without an administrator's rights gave another account's name and
  password), a per-user install goes to that account. The summary page says
  so, in a row that names the account.
- R6 `README.md` and `docs/platforms.md`.

## Acceptance Criteria

- [x] R1 The mode for an elevated installer on Windows is the wizard, and
      the command line with `--yes` (`TestChooseMode`).
- [x] R1 hayami's installer, built `--windowed` against this change and
      started from an elevated shell on Windows 11: its wizard opens. The
      0.9.10 release's installer, started the same way, exited with code 2.
- [x] R3 The welcome page of an elevated installer does not say that it
      asks for an administrator (`TestTheWizardAsksWhoTheInstallIsFor`).
- [x] R4 An elevated installer starts the program through Explorer, and one
      that is not elevated starts it directly
      (`TestTheFinishPageLaunchesAfterAReplacement`).
- [x] R4 Desk check: an installer run as an administrator, "Launch now"
      ticked: the program is running, and not as an administrator (hayami,
      2026-10-12: the panel's parent is Explorer and its token is not
      elevated; the install is per-user, by a wizard that was).
- [ ] R1/R3 Desk check: an install for everyone from a wizard run as an
      administrator: no second prompt, and the install is there.
- [ ] R5 Desk check: a person without an administrator's rights, giving
      another account's name and password: the summary names that account.
      No test covers the row, and no such account is at hand.
- [x] R2 The tests on Linux, where the mode for root is the command line
      and `--gui` is refused (`TestChooseMode`).

## Risks & Assumptions

- **Per-user as another account (R5).** The install goes where the summary
  says, which is not where the person at the desktop will look, and
  "Launch now" then asks Explorer to start a program in a home directory
  that person may not be able to read. The installer says so and does not
  stop it. An installer meant for such desks offers the scope for everyone.
- **Explorer as the way down (R4).** `explorer.exe <program>` starts the
  program with the shell's rights. It passes no arguments, which the finish
  page does not need. Where Explorer is not the shell, or itself runs as an
  administrator, the program starts with whatever rights Explorer has.
- **A program's own hooks run elevated** when the installer does (spec
  003). They ran in the helper, elevated, for an install for everyone
  already; for a per-user install this is new.
- **Rollback.** `windowAsAdmin` is one constant per platform: false on
  Windows puts back the refusal, for the installer and the uninstaller
  both.

## Verification

2026-10-11, Windows 11 Pro build 26200, in an elevated shell:
`go test ./installer/` passes with the tests above. hayami's installer
built with `--runtime-path` at this change opened its wizard when started
from that shell. On CachyOS (kernel 6.18) `go test ./...` passes and
`make lint` reports 0 issues. The desk checks are open.
