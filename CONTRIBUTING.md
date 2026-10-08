<!-- Generated from the shared contributing policy. Edit the template and the project fragments, then re-render; do not hand-edit this file. -->

# Contributing to fynstall

Thanks for your interest. This is a personal project, and external pull requests
are welcome. This guide states the policy a PR is held to, and what the
maintainers will do with it.

## TL;DR

- Only elected maintainers merge, and a contributor PR needs a maintainer's
  approving review. See [MAINTAINERS.md](MAINTAINERS.md).
- AI-written and hand-written PRs are both accepted, and both get the same
  review. Either may be rejected if it does not meet the standards below.
- Every PR needs tests **and**, for anything touching hardware or device
  support, an end-to-end reading from a real system — pasted into the PR.
  A PR without that collateral may be rejected.
- Keep the diff scoped: one logical change, no drive-by reformatting.
- No `Co-Authored-By` trailers and no AI-attribution footers in commits or the
  PR description.

## Who merges

Merge rights belong to the maintainers listed in
[MAINTAINERS.md](MAINTAINERS.md), and to nobody else. A PR from anyone who is
not a maintainer — human or agent — needs an approving review from a maintainer
before it lands, and the maintainer does the merging.

Maintainers merge their own work. These are small projects with a short
maintainer list, and a rule that forced a second maintainer's sign-off on every
change would simply stop work. The review requirement is there to gate
contributions coming in, not to make maintainers wait on each other.

Maintainers are elected, not self-appointed. The process, the current roster,
and how to be considered are all in [MAINTAINERS.md](MAINTAINERS.md).

A maintainer may merge a PR as-is, modify it before merging, hold it pending
changes, or close it. Closing is not a judgement of the contributor; it most
often means the change does not fit the project's direction, and that is the
maintainer's call to make.

## AI-written contributions

Code written with an AI assistant is fine. It is reviewed exactly like
hand-written code, held to the same standards, and modified by maintainers where
needed.

One rule makes that workable: **you must understand what you submitted.** If you
cannot explain what the change does, how it behaves at the edges, and why it
fits the existing design, the PR will be closed. Review questions go to the
person who opened the PR, not back to a model.

Specifically, a PR is likely to be rejected if it:

- adds a plausible-looking abstraction the project did not ask for,
- restates existing behaviour in new words without changing it,
- carries generated commentary, diagnosis dumps, or implementation-plan prose
  in the diff or the PR body,
- reformats or "improves" code outside the change,
- or claims a test or an e2e reading that was not actually run.

Do not add `Co-Authored-By` trailers or "Generated with …" footers to commits or
to the PR description. A PR containing them will be asked to amend.

## Tests and e2e collateral

Two things are required, and they are separate requirements.

**1. Tests.** Any behaviour change adds or updates tests. Tests encode
behavioural contracts — what the code does — not internals. A test that breaks
on a pure refactor with no behaviour change is testing the wrong thing. Paste
the test run summary into the PR.

**2. An e2e reading from a real system.**

fynstall writes to real systems. It copies files, creates launcher entries and
links, edits the registry on Windows, and asks for elevation. Its promise is
that the uninstaller puts the system back the way it was. A unit test can prove
a plan was computed correctly. It cannot prove that the launcher shows the
entry, that a double-click opens the wizard, or that nothing was left behind.
Any change to the builder, the engine, the uninstaller, desktop integration,
mode selection or elevation needs a reading from a real install.

Paste, at minimum:

- the `fynstall.yaml` you built from (the `examples/hello` one is fine), the
  `fynstall build` command, and the installer and uninstaller file names it
  produced;
- the OS and version, the desktop and display server on Linux (for example
  KDE Plasma 6 on Wayland), and the install scope you used (user or system);
- how you started the installer (double-click, terminal, `--gui`, `--cli`)
  and which front end ran;
- a listing of every path the install touched, taken before the install,
  after the install, and after the uninstall. On Linux, use `find` over the
  affected directories with a hash of each file. On Windows, also include
  `reg export` of the affected key before the install and after the uninstall.
  The before and after-uninstall listings must match, apart from the paths the
  config keeps on uninstall;
- for desktop integration: confirmation that the entry appeared in the
  launcher with its icon, and that it is gone after the uninstall;
- for elevation: how many prompts appeared, and which process ran elevated.

Test on a scratch account, a VM or a temporary `HOME`, not on the system you
depend on. If the change affects a platform you do not have, say which
platform you tested on and which you could not.

Report what you actually observed. Describe the hardware, the OS and the
software versions involved, the command you ran, and its real output. "Works on
my machine" is not a reading. If a reading cannot be taken — no access to the
device, platform not available to you — say so plainly in the PR and say what
*was* verified; a maintainer will decide whether to take the reading or hold the
PR. Claiming a reading that was not taken is the one thing that will get a
contributor's future PRs declined on sight.

## Scope

An installer framework for Go and Fyne programs. A developer describes a
program in `fynstall.yaml`, and `fynstall build` produces one installer binary
per target that holds the whole payload through Go embedding. The installer
runs as a GUI wizard when started from a desktop and as a CLI when started
from a terminal. It installs a separate uninstaller beside the program, and
that uninstaller restores the system to its state before the install.

In scope: the config format, the builder, the install engine and its
uninstaller, per-user and system install scopes, desktop integration on Linux
and Windows (macOS later), and the two front ends. Out of scope: package
manager formats (deb, rpm, MSI, AUR), auto-update, running payload code during
the install, and programs that are not Go programs.

The config format and the receipt that an uninstaller reads are public
interfaces: an installed program depends on them for as long as it stays
installed. A change to either is a direction change: start a Discussion, not
a PR.

Changes that alter the project's direction — the interaction model, the
architecture, persistence formats, or the public interface — start as a GitHub
Discussion, not as a PR. A PR that changes direction without prior alignment
will likely be closed regardless of its quality.

## Development setup

```bash
git clone git@github.com:ushineko/fynstall.git
cd fynstall
make setup        # installs the pinned golangci-lint
make test         # unit and integration tests, into temporary directories
make lint
make build        # the fynstall builder
```

Go 1.26 or newer. The GUI front end and any installer with the wizard need
CGO, OpenGL and X11/Wayland headers; `fynstall build --cli-only` and
`make test` do not. Windows installers are cross-built from Linux with
`x86_64-w64-mingw32-gcc`. `make test` never writes outside temporary
directories and never asks for elevation.

## What gets checked on your PR

| Required from you | Not required from you |
| --- | --- |
| `make test` passes | Spec files in `specs/` |
| `make lint` is clean | Validation reports |
| Tests for the behaviour you changed | Version bump or release tag |
| An e2e install and uninstall reading, pasted in the PR | |
| Before and after-uninstall listings that match | |
| No secrets in code, logs, or pasted output | |
| No `Co-Authored-By` / AI-attribution trailers | |

The maintainer's own workflow (spec files under `specs/`, validation reports,
release tagging) is internal cadence. **External contributors are not expected
to write specs or validation reports, bump versions, or tag releases.** Bring a
clean, tested, in-scope change with its e2e reading and the maintainers handle
the bookkeeping on merge.

## Security and dependencies

- No hardcoded secrets or credentials, and none in logs or error messages.
- No `eval`/`exec` of dynamic input. Spawn subprocesses with explicit argument
  lists, never by interpolating into a shell string.
- No new network calls without prior discussion.
- Prefer the standard library. Open a Discussion before adding a dependency;
  a new third-party module in a PR is a decision for the maintainers, not a
  detail of the change.

## Commit and PR conventions

- Conventional-style subjects: `feat(...)`, `fix(...)`, `refactor(...)`,
  `docs(...)`, `test(...)`.
- One logical change per PR. Split unrelated work.
- No secrets or credentials, in code, in logs, in error messages, or in pasted
  e2e output. Redact serial numbers and hostnames if you would rather not
  publish them.
- Reference a related issue in the commit body with `refs #<number>`.
- Describe what changed, why, and how it was verified.

## Questions

Open a GitHub Discussion. Issues are for reproducible bug reports and
maintainer-created work items.

---

This policy is shared across the project author's public repositories; the
canonical copy lives in a private sysadmin repository and is rendered into each
project. Project-specific sections (scope, setup, checks, e2e) differ per repo;
the governance sections do not.
