## Validation Report: a service with an account of its own (spec 010)

**Date**: 2026-11-10 23:30
**Commit**: on top of 40ffef3, branch `feat/service-account`
**Status**: PASSED

### Phase 3: Tests

- Linux: `make test` passes after `make hello`; `make lint` 0 issues.
- Windows: every package but `builder` passes; its failing test fails on
  `main` too. `make lint`: the one finding `main` has. Both are #64.
- The real service manager tests, as an administrator: pass.
- Coverage: not measured.
- Status: PASSED

### Phase 4: Code Quality

- One key carried from config to manifest to both backends; no duplication.
- Status: PASSED

### Phase 5: Security Review

- The change lowers what a service may do. The account name is built from
  the service name, which validation limits to letters, digits, `_ . -`.
- No new input, file, subprocess or secret.
- Status: PASSED

### Phase 5.5: Release Safety

- Change type: an additive config key and an additive manifest field.
- A receipt written before this change uninstalls as before: removal does
  not read the account.
- Rollback: revert the PR. A config with the key then fails validation.
- Status: PASSED

### Overall

- All gates passed: YES.
