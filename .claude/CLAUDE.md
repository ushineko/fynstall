# fynstall Project Guidelines

How this repository is worked on. It is written for any contributor and any
coding agent; nothing here assumes a particular workflow tool. Where a rule
is enforced by a test, the test is named.

---

## Project overview

- **What it is**: an installer framework for Go and Fyne programs. A
  developer describes a program in `fynstall.yaml`; `fynstall build` produces
  one self-contained installer binary per target, with the payload held by Go
  embedding, and a separate uninstaller beside it.
- **Module**: `github.com/ushineko/fynstall`. Public repository, MIT.
- **Two front ends, one engine.** The installer runs as a GUI wizard when
  started from a desktop and as a CLI when started from a terminal; `--gui`
  and `--cli` override the choice. Both front ends drive the same engine.
- **Platforms**: Linux first, then Windows. macOS is later and not yet
  specified.
- **Window**: built on `github.com/ushineko/fynedesygn`, whose
  `docs/design-system.md` governs the window's shell, theme and widgets. The
  wizard is fynedesygn's `wizard` package (fynedesygn spec 056). A shape the
  library lacks is a library change, not a local copy.
- **Source of record for decisions**: the specs in `specs/`, starting with
  `specs/001-installer-prototype.md`, and `docs/architecture.md` once it
  exists.

---

## Architecture rules

- **The engine has no UI.** `engine` never imports a UI package or Fyne. It
  plans, applies and reports progress as events; `ui/cli` and `ui/gui`
  consume the same events. A behaviour that exists in one front end and not
  the other is a bug.
- **Plan, then apply.** Every change the installer makes is in the plan
  before any change is made. `--dry-run` prints the plan and changes nothing.
  Code that writes to the system outside `engine.Apply` is a bug.
- **The installed uninstaller is the only remover.** An installed program
  is removed by the uninstaller that was installed with it, never by a newer
  installer's own logic. Uninstall, upgrade, repair and downgrade all run the
  installed uninstaller first. This keeps removal in step with what is
  actually on disk when fynstall changes upstream.
- **Uninstall restores the original state.** Apply records what existed
  before it replaced or changed anything: files are backed up, previous
  registry values are kept in the receipt journal. The uninstaller replays the
  journal in reverse. After an uninstall the system matches its state before
  the install, apart from the paths the config keeps on uninstall.
- **The receipt and the config are public interfaces.** An installed
  program depends on its receipt for as long as it stays installed, and
  every consuming project depends on the config format. Changes to either are
  additive or versioned; never break a receipt an older uninstaller wrote.
- **No GUI as root.** System scope runs only the apply step in a privileged
  helper (`pkexec` or `sudo` on Linux, UAC on Windows). The Fyne process
  always runs as the user.
- **Nothing outside the plan's roots.** A destination that resolves outside
  its root (`..`, an absolute `dst`, a symlink escape) is refused by
  `validate` and again by `Apply`. Payload content is never run during an
  install.
- **The CLI-only build carries no Fyne.** `-tags nogui` with
  `CGO_ENABLED=0` must build and link no graphics libraries.

---

## Issue tracking

GitHub Issues on this repository is the tracker. Labels: `bug`,
`enhancement`, `chore`, `docs`.

- Anything worth a spec gets an issue first. The issue says what is wrong or
  wanted; the spec says what will be done.
- Specs are `specs/0NN-short-lowercase-title.md`, numbered in sequence. The
  first lines are:

  ```
  # Spec 0NN: <lowercase title>

  **Issue**: [#NN](https://github.com/ushineko/fynstall/issues/NN)

  ## Status: INCOMPLETE
  ```

- A spec carries: Executive Summary (written last), Context, Requirements
  (R1, R1.1, ...), Acceptance Criteria (`- [ ]` checkboxes), Risks &
  Assumptions (including rollback), Alternatives Considered when a choice
  was not obvious, and Verification. Work in phases puts each phase's
  criteria and its desk check under its own heading (spec 001).
- A spec is COMPLETE only when every criterion is checked, and the spec is
  updated in the same branch as the code.
- Work that needs a fynedesygn change gets an issue and a spec in
  fynedesygn, linked from both sides. fynstall is in fynedesygn's "Used by"
  table.
- The issue links the spec once it exists; the PR says `Closes #NN`. GitHub
  does not always close the issue from the PR body, so check after merging.

---

## Tests

- `make test` (`go test ./...`) writes only inside temporary directories,
  never asks for elevation, and passes on a machine with no display.
- Engine behaviour is tested by building a real installer from
  `examples/hello` and running it as a real process against a temporary
  `HOME` (and `XDG_*` directories). The filesystem, `go build` and the
  installer process are not mocked. The one fake is elevation: the helper
  is started without `pkexec` in tests.
- Every uninstall test compares a recursive listing with hashes of the
  temporary home taken before the install against one taken after the
  uninstall. They must match.
- GUI tests drive the wizard with `wizard.Headless` and fynedesygn's
  `fynetest`, against the real engine.
- Desk checks (a launcher entry appearing, a double-click opening the
  wizard, a pkexec or UAC prompt, a Windows VM run) are manual and are
  pasted into the spec's Verification section. Each phase of spec 001 lists
  its desk check.
- Reproducibility: two builds of the same input produce byte-identical
  installers; a test checks this.

---

## Documentation changes with the code

In the same branch as the change, never as a follow-up:

- `docs/config.md` for any change to `fynstall.yaml` (new key, new default,
  new placeholder). `fynstall init` output stays in step with it.
- `docs/architecture.md` when the shape of the system changes.
- `docs/platforms.md` when what an install does on a platform changes:
  paths, desktop integration, registry keys, elevation.
- README: the changelog entry under `### Unreleased` with the spec number
  and issue link.

Prose follows `docs/style.md`: plain verbs, active voice, simple present, no
marketing language.

---

## Environment

- Go from `go.mod`. `make setup` installs the pinned golangci-lint;
  `make lint` runs it with the config in `config/`.
- `make build` builds the builder. Building an installer with the GUI
  needs cgo, OpenGL and X11/Wayland headers; `--cli-only` installers do not.
- Windows installers are cross-built from Linux with
  `x86_64-w64-mingw32-gcc`. Windows desk checks run in a Windows 11 VM.
- Run desk checks on a scratch account, in a VM, or with a temporary `HOME`;
  a system-scope desk check writes to `/opt` and `/usr`.

---

## Public-repository rules

- No credentials, tokens or personal data in code, fixtures, docs or logs.
- No personal paths, hostnames or usernames in committed files, including
  pasted listings from desk checks. Fixtures use made-up identities.
- No third-party assets committed without a licence that allows it; the
  `examples/hello` icon is made for this repository.

---

## Git

None of this is enforced by GitHub; it is the convention across the
ushineko repositories.

- Work happens on a branch (`feat/`, `fix/`, `chore/`, `docs/` and a short
  slug) and lands on `main` through a PR. A worktree per branch is
  preferred when several pieces of work are in flight.
- Commit subjects: lowercase conventional prefix, imperative,
  sentence-like (`feat(engine): restore replaced files on uninstall`). The
  body says why; the diff says what.
- Stage files by name and check the staged list; never commit build output
  (`bin/`, `dist/`, generated installer sources).
- A PR body says what changed, why, what a reviewer should look at first,
  and how it was verified, and links the spec.
- **No AI attribution** in commits or PRs: no `Co-Authored-By` trailers for
  tools, no "Generated with" footers.
- `CONTRIBUTING.md`, `MAINTAINERS.md` and `.github/PULL_REQUEST_TEMPLATE.md`
  are generated from a shared template; do not hand-edit them.
- fynstall has no CI yet. Run `make test` and `make lint` before pushing.

---

## Releases

The version of record is `.tag`, stamped into the builder by the Makefile.
The builder's version is also the runtime version every installer it builds
is pinned to, so a release is the point at which installers built from it
become reproducible by others. `.tag`, the `**Version**` line in
`README.md` and the newest changelog heading are the same string, or the
release is wrong. Ask the maintainer before bumping.

1. A commit `chore: release X.Y.Z` that sets `.tag`, the README Version
   line, and turns `### Unreleased` into `### X.Y.Z (YYYY-MM-DD)`.
2. An annotated tag `vX.Y.Z` with the message `fynstall X.Y.Z`, pushed.
3. A GitHub Release for the tag whose notes are that version's changelog
   entry. Every tag gets one.

Run each step only if the one before it succeeded.
