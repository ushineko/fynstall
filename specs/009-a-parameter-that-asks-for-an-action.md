# Spec 009: a parameter that asks for an action

**Issue**: [#61](https://github.com/ushineko/fynstall/issues/61)

## Status: COMPLETE

## Executive Summary

A parameter can be marked `once`: it asks for an action this install takes,
not a setting, so an upgrade starts it from its default instead of carrying
the value the installed version was given. hayami's "install the network
helper" was carried into every later upgrade, and an unattended `--yes`
upgrade stopped at an administrator prompt nobody had asked for. The change
is one filter in `engine.Previous`.

## Context

An upgrade does not ask again for what the installed version was given
(spec 002 D3a): the receipt keeps every non-secret parameter's value, and
`engine.Previous` reads them back. That is right for a setting (a server
address, a name) and wrong for a parameter that is a choice to do something
now. hayami's extension sets `network=yes` when the person ticks "Install the
network helper"; the receipt kept it, and the next `--yes` upgrade, where
no page asks, took it and ran the helper's install, which asks the system for
an administrator (ushineko/hayami#259).

## Requirements

- R1 A parameter in the config may say `once: true`. It is carried into the
  manifest.
- R2 An upgrade does not take a `once` parameter's value from the installed
  version: the value comes from a flag, the side file, a person or the
  default, as for a fresh install.
- R3 Every other parameter is carried as before.
- R4 `docs/config.md` lists the key, and its list of where a value comes from
  names the installed version's value, which it had left out.

## Acceptance Criteria

- [x] A real `greet` installer, installed with `--wave=yes` and then run again
      with `--yes`, records `wave` as its default, `no`, and still carries
      `name`. (`TestAnUpgradeDoesNotCarryAParameterMarkedOnce`, against the
      built installer; falsified by taking the filter out.)
- [x] The existing upgrade tests pass: what the old version had is removed,
      a secret survives an upgrade unasked.
- [x] `docs/config.md` documents `once` and the full order of sources.
- [x] `make test` and `make lint` pass.

## Risks & Assumptions

- A `once` parameter that is also `required` must be given again on every
  upgrade, by a flag, the side file or a person; with `--yes` and neither,
  the upgrade stops and names the flag, as a fresh install would. That is
  what `once` means, and is documented.
- The receipt still records a `once` parameter's value, so a person can see
  what an install did; it is only not read back.
- Rollback: revert; a config that uses `once` is refused by an older
  fynstall as an unknown key, so an app that adopts it needs this version.

## Alternatives Considered

- The app's own after-install code ignoring a carried value: it cannot tell
  a carried value from a flag given on the command line, which is a
  documented route. Rejected.
- Acting only when the action is not already done: still acts, unasked, on
  every upgrade that changes the version. Rejected.

## Verification

- `go test ./builder -run 'TestAnUpgradeDoesNotCarryAParameterMarkedOnce|TestASecretSurvivesAnUpgrade|TestAnUpgradeRemoves'`
  passes against installers built from `examples/greet`.
- With `declared[p.Name] = !p.Once` put back to `true`, the new test fails:
  "the upgrade carried an action".
