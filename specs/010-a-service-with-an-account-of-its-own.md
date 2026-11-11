# Spec 010: a service with an account of its own

**Issue**: [#63](https://github.com/ushineko/fynstall/issues/63)

## Status: COMPLETE

## Executive Summary

A `service` action gains one key, `account: own`. The service then runs as
an identity of its own with the rights any user has. On Windows that is the
virtual account `NT SERVICE\<name>`, in place of LocalSystem. On Linux a
system unit gets `DynamicUser=yes` and a state directory. A per-user unit
already runs as its user, so the key changes nothing there. Without the key,
every service runs as it does today.

## Context

A Windows service from fynstall runs as LocalSystem, and a Linux system unit
runs as root. A program that needs no administrator rights still has them.
Anything a client of that program can make it read or write is then read or
written with every right on the machine.

The first program to need this is hotaru, a lighting service (hotaru #202).
It needs HID devices, a TCP port on localhost and a directory of its own.

Measured on Windows 11 (26200), as a service under `NT SERVICE\<name>`, with
the service SID type unrestricted:

- The program opened a vendor HID collection of a keyboard, wrote a report
  and read the answer, as the same program does for a signed-in user.
- It connected to a TCP port on `127.0.0.1`.
- It made a directory in `%ProgramData%`, which any user may.
- Its profile is `C:\Windows\ServiceProfiles\<name>`. Windows makes it the
  first time the service loads, and keeps its registry hive loaded after the
  service is removed. The directory could not be deleted until a restart.

## Requirements

- R1 A `service` action takes `account`. Its one value is `own`. Without the
  key the service runs as today. Any other value is a validation error.
- R2 On Windows, `account: own` registers the service with the account
  `NT SERVICE\<name>` and no password, and the service SID type
  unrestricted, so the account's SID can be named in an access list.
- R3 On Linux, in a system install, `account: own` writes
  `DynamicUser=yes` and `StateDirectory=<name>` in the unit. The service runs
  as a user that systemd makes for it, and may write in `/var/lib/<name>`.
- R4 On Linux, in a per-user install, the unit is written as without the key:
  a user unit already runs as its user. The plan says so.
- R5 The plan line of the service names its account: `as NT SERVICE\<name>`
  on Windows, `as a user of its own` for a Linux system unit.
- R6 The manifest carries the account as an additive field. A receipt
  written before this change still uninstalls: removing a service does not
  depend on its account.
- R7 The uninstaller removes the service as today. It does not remove the
  account's profile under `C:\Windows\ServiceProfiles`: Windows makes it and
  holds it open. `docs/platforms.md` says so.
- R8 Docs: `docs/config.md` (the key), `docs/platforms.md` (both systems),
  the README changelog.

## Acceptance Criteria

- [x] `account: own` validates; `account: root` fails with the key's line.
- [x] A system unit with `account: own` has `DynamicUser=yes` and
      `StateDirectory=<name>`; a user unit and a unit without the key do not.
- [x] The Windows stand-in records `ObjectName` as `NT SERVICE\<name>` with
      the key and `LocalSystem` without it.
- [x] Against the real service manager, as an administrator: a service with
      the key runs as `NT SERVICE\<name>` with an unrestricted SID type, and
      is removed cleanly.
- [x] The plan line names the account on both systems.
- [x] `make test` and `make lint` pass on Linux and on Windows, apart from
      what fails on `main` as well (#64).
- [x] Docs per R8.

## Risks & Assumptions

- **A device's access list could refuse the account.** The keyboard measured
  allows it. A device whose driver limits access to administrators would not
  work under `account: own`; such a program leaves the key out.
- **The profile left behind.** One small directory per removed service, kept
  by Windows. Deleting it would mean unloading a hive Windows holds, which
  is not the uninstaller's to do.
- **Rollback**: revert the PR. Configs without the key are unaffected; a
  config with it fails validation in an older builder, which is the honest
  answer.

## Alternatives Considered

- **A named account and password.** Needs a password kept somewhere, and a
  user made and removed. A virtual account needs neither.
- **`NT AUTHORITY\LocalService`.** One shared identity for every such
  service, so one service's files are open to the others. The virtual
  account is per service.
- **A key per system (`windows_account`, `user:`).** Breaks the rule of one
  config for every platform. `own` means the same on both: an identity of
  the service's own, with a user's rights.

## Verification

2026-11-10.

- Linux (CachyOS, Go 1.27.3, lint under go1.26.0), a fresh clone with the
  change: `make test` passes once `make hello` has run; before it,
  `TestAFullBuildForAnotherTargetNeedsACompilerForIt` fails on `main` too
  (#64). `make lint`: 0 issues.
- Windows 11 (26200), Go 1.27.2: every package passes but `builder`, whose
  `TestACompilerOnPathIsAccepted` fails on `main` too (#64). `make lint`
  reports the one finding `main` has (#64).
- New tests: the key validates and a named account fails on its line; the
  unit has `DynamicUser=yes` and `StateDirectory=<name>` for a system unit
  of its own and for nothing else; the stand-in records
  `NT SERVICE\hello`.
- The real service manager, as an administrator
  (`FYNSTALL_TEST_REAL_SERVICES=1`): the service ran as
  `NT SERVICE\<name>` with SID type unrestricted, and was removed. The
  existing LocalSystem test passes too. Windows kept the account's profile
  under `C:\Windows\ServiceProfiles`, as R7 says.
- The measurement in Context was a separate program run as such a service
  on the same machine, with a keyboard's receiver attached.

Not run: a Linux system install with a real systemd. The unit's text is
tested; what `DynamicUser=` does is systemd's.
