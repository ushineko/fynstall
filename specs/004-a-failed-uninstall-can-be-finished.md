# Spec 004: a failed uninstall can be finished

**Issue**: [#33](https://github.com/ushineko/fynstall/issues/33)

## Status: INCOMPLETE

The code and the automated tests are done on Windows and Linux. The desk check
on Windows is open; see Acceptance Criteria.

## Executive Summary

An uninstall that cannot remove one file used to remove the index entry and
the uninstaller first. What stayed was a part of an install that no
supported command could find or remove. The uninstaller now removes the
index entry and its own file last, and only when everything else went well.
On Windows it tries a file that another program has open again for a few
seconds. The installer finds an install that an older uninstaller left
without its index entry, and removes it with `--force-receipt-uninstall`.

## Context

Issue #33 comes from a consuming project on Windows with fynstall 0.1.0. A
repair ran the installed uninstaller, as every upgrade, repair and downgrade
does (spec 001 R17). One program file could not be deleted, because another
process had it open. The uninstaller exited with 1 and the repair stopped.
After that:

- the message of the repair named `uninstall.exe`, which was gone;
- `--force-receipt-uninstall` printed "is not installed";
- the only way out was to delete the install directory by hand.

The cause is the order of the journal. `Apply` writes the index entry as the
last journal entry, and the uninstaller is an ordinary payload entry.
`Uninstall` replays the journal newest first and collects errors. So the
index entry goes first and the uninstaller goes with the other files,
whether or not a later entry fails. The receipt stays, as designed, but
nothing points at it: the installer finds an install through the index
entry only.

A test reproduces this on Windows by holding `bin\greet.exe` open during a
repair. In that run the receipt stays, the index entry and `uninstall.exe`
are gone. The issue says that `receipt.json` was gone too; the test does
not show that, and the missing index entry alone explains both messages in
the issue.

Linux has the same order. A failed uninstall there also loses the index
entry, and a later repair then installs over a part of an install.

The architecture rule is that only the installed uninstaller removes an
install. A fix in the uninstaller therefore reaches only installs made
after the fix. Installs made by 0.1.0 keep the 0.1.0 uninstaller, so the
installer needs a way to recover what that uninstaller leaves.

## Requirements

- **R1. The record of an install outlives a failed uninstall.** When an
  uninstall cannot undo an entry, the receipt, the index entry and the
  uninstaller stay. The uninstaller removes the index entry and its own
  file after every other journal entry is undone, and the receipt after
  those.
  - **R1.1.** The other entries are still all tried. One file that cannot
    be removed does not keep the rest.
  - **R1.2.** A file that was not removed is not reported as removed.
- **R2. A failed uninstall is finished by running it again.** The
  uninstaller, the installer's `--uninstall`, and a second repair, upgrade
  or downgrade each find the install and finish the removal. The message
  of a failed repair names both the uninstaller and "run this installer
  again".
- **R3. A file in use is tried again (Windows).** When Windows refuses a
  removal because another program has the file open, the uninstaller tries
  again after 100 ms, then doubles the wait. One uninstall waits about
  5 seconds in total for all files together, then reports the file. The
  uninstaller's own file is not waited for; it moves aside as before.
  Linux does not hold a file against removal, so it does not wait.
- **R4. An install with no index entry can be removed.** With
  `--force-receipt-uninstall` and no index entry, the installer reads the
  receipt in the default install directory of each scope. When the receipt
  is for the same app id, the same scope and the same directory, the
  installer removes that install with its own engine.
  - **R4.1.** Only the default directory is read. The index entry is what
    says that an administrator wrote an install for everyone; a receipt in
    a directory that somebody names has no such proof.
  - **R4.2.** When the installed uninstaller fails and its file is gone,
    the message of the repair names `--force-receipt-uninstall` and not
    the missing file.

Not in this spec:

- Rolling a failed uninstall back to the installed state, as Windows
  Installer does. See Alternatives Considered.
- The entry in Settings > Apps. It is a registry entry of the journal and
  is undone with the others, so after a failed uninstall the program is
  not listed in Settings until it is repaired. The uninstaller in the
  install directory still runs.

## Acceptance Criteria

- [x] An uninstall that cannot remove one file returns an error, leaves the
  receipt, the index entry and the uninstaller, removes the other files,
  and does not report the stuck file as removed. A second run finishes, and
  the home is as it was before the install
  (`engine`: `TestAnUninstallThatFailsLeavesItsRecordItsIndexEntryAndItsUninstaller`).
- [x] On Windows, a repair over an install with a held program file exits
  1, names the uninstaller and "run this installer again", and leaves the
  receipt, the index entry, `uninstall.exe` and the held file. After the
  file is let go, the same repair succeeds, and the uninstall leaves the
  profile and the registry as they were
  (`builder`: `TestARepairThatCannotRemoveAFileLeavesAnInstallThatCanBeRepaired`).
- [x] On Windows, a file that is let go after one second is removed, and
  the uninstall exits 0 (`builder`: `TestAFileInUseForAMomentIsRemovedOnALaterTry`).
- [x] An install with its receipt, no index entry and no uninstaller is
  removed by `--force-receipt-uninstall`, and the profile is as it was
  (`builder`: `TestAnInstallWithNoIndexEntryIsRemovedByItsRecord`).
- [x] `go test ./...` and the linter pass on Windows.
- [x] `make test` and `make lint` pass on Linux.
- [x] On Linux, a repair whose old uninstaller cannot remove one file (a
  non-empty directory where a payload file was) exits non-zero and leaves
  the receipt, the index entry and the uninstaller; once the obstacle is
  gone, the same repair succeeds. An install whose index entry was deleted
  is removed by `--force-receipt-uninstall`, and the home is as it was
  (`builder`: `TestAFailedRepairCanBeRunAgainAndFinishes`,
  `TestForceReceiptUninstallFindsAnInstallWithNoIndexEntry`; both fail
  with the engine and installer of `dc0aa5a`).
- [ ] Desk check on Windows: the repair of the consuming project that
  failed in issue #33 succeeds, or fails and can be run again.

## Risks & Assumptions

- **Installs made by 0.1.0.** Their uninstaller keeps the old order. R4 is
  the recovery for them. It needs the install to be in the default
  directory; an install put elsewhere with `--dir` that an 0.1.0
  uninstaller left in parts is still removed by hand.
- **The receipt is not changed.** No field is added, and the journal has
  the same entries in the same order. An older uninstaller reads a receipt
  that this version writes, and this version reads an older one.
- **Access denied is treated as in use.** Windows gives the same error for
  a running program's file and for a file that the user may not delete.
  The second case costs the 5 seconds once and then fails as before.
- **The wait is a guess.** 5 seconds is enough for a scanner in the cases
  seen. It is a constant in `engine/remove_windows.go`, not a config key.
- **Rollback.** Code only. Revert the commit; installs made in between
  carry an uninstaller with the new order, which is safe to keep.

## Alternatives Considered

- Considered rolling a failed uninstall back, so that the install is whole
  again, as Windows Installer does. Rejected for now: the uninstaller would
  have to keep every removed file until the end, and to redo actions and
  registry entries. It is the better behaviour and a larger change; R1 and
  R2 make a failure recoverable without it.
- Considered stopping at the first entry that fails. Rejected: the entries
  after it are then left for no reason, and an uninstall that is run again
  has more to do.
- Considered having the installer write the index entry again after an old
  uninstaller fails. Rejected: it is a write outside `engine.Apply`, and
  for an install for everyone the installer is not an administrator at
  that point.
- Considered marking a stuck file for deletion at the next start of the
  computer. Rejected: Windows accepts that only from an administrator, and
  a repair would then install a new file that the restart deletes.

## Verification

### Windows (2026-10-11)

Windows 11, Go from `go.mod`, on the branch before the commit.

```
$ go test ./... -count=1
ok  	github.com/ushineko/fynstall/builder	115.466s
ok  	github.com/ushineko/fynstall/cmd/fynstall	2.975s
ok  	github.com/ushineko/fynstall/config	2.326s
ok  	github.com/ushineko/fynstall/engine	4.993s
ok  	github.com/ushineko/fynstall/examples/beacon	0.644s
ok  	github.com/ushineko/fynstall/examples/greet	2.764s
ok  	github.com/ushineko/fynstall/examples/hello	4.986s
ok  	github.com/ushineko/fynstall/installer	11.665s
ok  	github.com/ushineko/fynstall/manifest	2.386s
ok  	github.com/ushineko/fynstall/platform	2.120s

$ golangci-lint run --config config/.golangci-v2.12.2.yml ./...
0 issues.
```

Before the fix, the new repair test failed with the output of issue #33:
the uninstaller printed "removed" for the index entry, the config file,
`uninstall.exe` and the held `greet.exe`, then "The process cannot access
the file because it is being used by another process", and `uninstall.exe`
was gone.

The real `HKCU` Uninstall key and `Path` were read after the tests and
hold nothing from them.

### Linux (2026-10-11)

CachyOS amd64, Go 1.26.0, in a worktree of the branch at `ac1bd1f`.

- `go build ./...` with cgo, `go build -tags nogui ./...` and `go vet ./...`
  report nothing.
- `make test` (`go test -race ./...`) passes in all 10 packages, with
  `TestAnUninstallThatFailsLeavesItsRecordItsIndexEntryAndItsUninstaller`
  among them.
- `make lint` (golangci-lint v2.12.2) reports 0 issues.
- `make vuln` (govulncheck) reports no vulnerabilities.

### Gates

No dependency changed. The security review found one point, R4.1,
which the design answers. The change is code only; see Rollback.
