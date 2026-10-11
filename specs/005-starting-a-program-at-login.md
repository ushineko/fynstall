# Spec 005: starting a program at login

**Issue**: [#35](https://github.com/ushineko/fynstall/issues/35)

## Status: INCOMPLETE

## Executive Summary

A launcher entry takes `autostart: true`. The installer then also puts the
entry where the desktop looks for what to start when its user logs in: the
XDG autostart directory on Linux, the Startup folder on Windows. It is in
the plan, in the receipt's journal and in the uninstall, as the launcher
entry is. Linux is written and unrun: the checks on a Linux machine are
open.

## Context

A desktop program that runs all the time, such as a panel or a tray tool,
starts when its user logs in. fynstall has services, which are for
programs with no window: a systemd user unit has no display on some
sessions, and a Windows service has none at all.

hayami is the first consumer. Its own installers add a Startup-folder
shortcut on Windows, and neither that nor an XDG autostart entry could be
declared in `fynstall.yaml`.

## Requirements

- R1 `integration.desktop[].autostart`, a boolean, default false. One key,
  with the same meaning on every platform (spec 002).
- R2 Linux: the entry is written a second time, without its Uninstall
  action, to `{config}/autostart/<id>.desktop` for a per-user install and
  `/etc/xdg/autostart/<id>.desktop` for an install for everyone. It is a
  planned file like the launcher entry: checked against its base,
  journalled, and put back on uninstall when it replaced one.
- R3 Windows: a second shortcut, the same as the launcher's, in the
  `Startup` folder of the Start Menu the launcher's shortcut is in: the
  person's, or everyone's for system scope. It is a planned shortcut like
  the first.
- R4 The plan says so: `--dry-run` and the question on the command line,
  and a row on the wizard's summary page.
- R5 An entry with `terminal: true` cannot have it: there is no terminal at
  login. `fynstall validate` says so at the key.
- R6 `docs/config.md` and `docs/platforms.md`.

## Acceptance Criteria

- [x] R1/R5 A config with the key loads; with `terminal: true` beside it,
      it is an error (`TestAnEntryInATerminalCannotStartAtLogin`).
- [x] R3 On Windows an install has the shortcut in the Startup folder and
      the launcher's in the Start Menu, and the uninstall leaves the home
      and the registry as they were
      (`TestAnEntryThatStartsAtLoginIsInThePlaceTheDesktopLooks`).
- [ ] R2 The same test on Linux: the entry is in `{config}/autostart`,
      with the program's path and no Uninstall action.
- [ ] Desk check, Windows: after an install and a new login, the program
      is running.
- [ ] Desk check, Linux (KDE Plasma): the same.

## Risks & Assumptions

- **A person's own choice.** Windows and the Linux desktops let a person
  turn a start-at-login entry off without removing it. fynstall does not
  read that setting and does not turn it back on: an upgrade writes the
  entry again, which on Windows leaves the person's "disabled" mark in
  place, and on a Linux desktop that marks the file itself replaces it.
- **Not optional at install time.** The key is in the config, so every
  install of the program starts at login. An offer on a wizard page needs
  an action that depends on a parameter, which fynstall does not have.
- **Rollback.** Additive. A config without the key installs as before.

## Verification

2026-10-11, Windows 11: `go test ./installer ./config` pass, with the two
tests above. The Linux run and both desk checks are open.
