# Spec 002: replacing a production installer

**Issue**: [#6](https://github.com/ushineko/fynstall/issues/6)

## Status: INCOMPLETE

The design was agreed on 2026-10-08: D1a, D2a and D3a, and the one-config
rule. Implementation follows in the phases below.

## Executive Summary

Populated before the first PR is opened.

## Context

Spec 001 delivers an installer that copies a payload, integrates it with the
desktop, and restores the system on uninstall. The test of whether fynstall
is ready for real use is harder: it must be able to replace a production
installer made with a commercial tool. The reference for this spec is a
Windows NSIS installer for a background agent. It is described here only by
what it does.

**What the reference installer does.**

- It installs per machine, with elevation, into `Program Files`. It also
  writes to a shared data directory (`ProgramData`) and migrates an older
  install's directory and data into the new ones.
- It registers a Windows service, starts it, and sets the service's recovery
  actions. Before an upgrade or an uninstall, it stops the service and stops
  the program's processes.
- It takes its settings from three places: defaults, a `config.yml` placed
  beside the installer, and command-line switches of the form `/Name=value`.
  A secret (an access token) is one of the settings. The installer then has
  the installed program write its own configuration file from the settings.
- It installs silently with `/S`. Automation uses that, an MSI wrapper uses
  it, and the installed program uses it to update itself: it places the new
  installer in its install directory and runs it with `/S`.
- It detects an existing install through its Uninstall registry key. To
  upgrade, it runs the old uninstaller and then installs. It keeps the
  configuration across that only by copying the file to a temporary
  directory first.
- It sets permissions (ACLs) on its directories. It refuses unsupported
  Windows versions and 32-bit Windows. It allows one installer at a time
  (a mutex).
- The installer, the uninstaller and the program's executables are
  Authenticode-signed, and the installer has a version resource.
- The uninstaller removes the service, the registry keys and the install
  directory. In the shared data directory it removes caches and keeps logs.

**What it does not do that fynstall does.** It has no rollback, so a failed
step is logged and the install carries on. It writes the access token to its
detail log. It leaves a registry key behind on uninstall. It has no way to
restore what it replaced.

The reference's packaging tool selects files per platform. A list for every
target of an OS is joined with a list for one OS and architecture, and each
entry is a source glob and a destination.

**The architectural difference.** The reference is an NSIS script, and
much of it is Windows mechanism written out by hand: registry keys, `sc`
and `icacls` calls, `$PROGRAMFILES64`, `/S` parsing. It works only on
Windows, and a Linux or macOS package of the same program is a second,
unrelated piece of work. fynstall is one description that installs on
every platform. That is the property this spec must not lose, so it is
stated as a rule below and the decisions are checked against it.

**Three gaps change fynstall's structure.** They are below, in
[Decisions](#decisions), because each changes the config schema, the
manifest, the engine or the receipt. Changing those before phases 4–7 build
on them is cheaper than changing them after. The other gaps are features
that slot into existing phases; they are listed in
[Leaf requirements](#leaf-requirements).

## The rule: one config for every platform

- **Every key in `fynstall.yaml` means the same thing on every platform.**
  The config says what the program needs: a service that restarts on
  failure, a launcher entry, a link on the user's `PATH`, data that is kept.
  How each is done on an OS is the job of that OS's backend in `platform`,
  not of the config.
- **What an OS needs and the config can derive, the backend derives.** The
  Uninstall registry key comes from `app` and the receipt. The version
  resource comes from `app.version`. A Start Menu shortcut comes from
  `integration.desktop`, as a `.desktop` file does on Linux. None of them
  has a key of its own.
- **Paths are placeholders, never platform paths.** `{data}`, `{config}`,
  `{bin}` and the install directory resolve per OS and scope. A config that
  names `Program Files` or `/opt` in a path other than an explicit
  `install.dir` override is a validation warning.
- **A concept that exists on only some platforms is a no-op elsewhere, and
  says so.** Directory ACLs and file modes are one `permissions` concept
  with a backend for each. Where a concept truly has no equivalent, the
  `--dry-run` and summary output say "not applicable on this platform"
  rather than the config growing an OS-specific branch.
- **An OS-specific escape hatch exists, but is a last resort.** A
  `platform: { windows: …, linux: … }` block is allowed only for what the
  rule above cannot express, and validation lists every such use. If the
  reference needs one, that is a finding of the experiment, not a design
  goal.

Each decision below follows this rule.

## Decisions

Each decision gives the options and a recommendation. The user chooses.

### D1: payload per target

Today one `payload` list serves every target. A Windows build needs
`hello.exe` where a Linux build needs `hello`, and some files exist for one
architecture only.

- **D1a: a `targets` filter on each entry** (recommended). An entry applies
  to every target unless it names some. Patterns match `os/arch`, with `*`
  for either part.

  ```yaml
  payload:
    - src: build/{os}-{arch}/hello{exe}   # {exe} is ".exe" on Windows, else ""
      dst: bin/hello{exe}
    - src: assets/
      dst: share/
    - src: build/windows/helper.dll
      dst: bin/helper.dll
      targets: [windows/*]
  ```

  `{os}`, `{arch}` and `{exe}` are build-time placeholders in `src` and
  `dst`, resolved per target. The builder already builds once per target,
  so it stages a manifest per target. Validation checks every target in
  `targets:` and reports an entry that matches none.

- **D1b: sections per target**, in the reference tool's style:
  `payload: {common: [...], windows/amd64: [...]}`. It is familiar from the
  reference. But a file needed on two of three targets is either listed
  twice or needs a third section, and the list for one target is spread
  over several places.

- **D1c: build-time placeholders only**, with no filter. This handles the
  `.exe` case but cannot leave a file out.

**Recommendation: D1a.** It keeps one list and puts the condition on the
entry it applies to. The placeholders cover the common case without a
filter at all.

**Decided: D1a** (2026-10-08).

Also from the reference tool: a short form `src::dst` for an entry is not
adopted. YAML maps are already short, and two forms of one thing is one more
to explain. A recursive copy that must drop a source prefix uses `dst`
directly; the reference's three-part form for that is not needed.

### D2: install-time actions

Spec 001 says the installer "runs no payload content during install". The
reference cannot be replaced under that rule. It registers a service and
generates the program's configuration file, and the second of those is done
by running the installed program.

The engine's model today is a journal of file operations, each with an undo.
The proposal extends the journal to **actions**: each action is a typed
change that the engine applies in order, records with what it needs to undo
it, and undoes in reverse, like a file.

- **D2a: built-in action types, and a hook as the last resort**
  (recommended).

  ```yaml
  actions:
    - service:                       # systemd unit on Linux, SCM on Windows
        name: hello
        exec: bin/hello{exe}
        args: [serve]
        start: true
        restart: on-failure          # Windows: recovery actions; Linux: Restart=
    - config_file:                   # written by the engine, from parameters (D3)
        path: "{config}/hello/config.yml"
        format: yaml
        values: { server: "{param:server}", token: "{param:token}" }
    - run:                           # last resort: a payload program
        exec: bin/hello{exe}
        args: [setup, --server, "{param:server}"]
        undo: [teardown]             # required; "none" must be said explicitly
  ```

  - A `service` action is undone by stopping and removing the service. A
    service that existed before the install is backed up (its definition)
    and restored, as a file is.
  - A `config_file` action is a file write, so it is undone like one. The
    engine writes it from parameters, so the installer does not need to run
    the program to produce it.
  - A `run` action runs a payload program by its path inside the install
    directory, never a shell. Its `undo` is required, and is run by the
    uninstaller. The installer cannot know what a `run` changed, so it says
    so on the summary page and in `--dry-run`.
  - Before the payload is replaced (upgrade, repair) and before an
    uninstall, the engine stops the services the receipt lists. The
    reference's "kill the program's processes" is an option on the service
    action (`stop_processes: [bin/hello{exe}]`), limited to processes whose
    executable is inside the install directory.

- **D2b: hooks only.** `pre_install`, `post_install` and `pre_uninstall`
  commands, as most installer tools have. It is simple to implement, but
  every installer then writes its own service registration, nothing can be
  undone automatically, and the journal no longer describes the install.

- **D2c: no actions.** fynstall stays a file installer, and services and
  configuration are the program's own job at first start. This does not
  replace the reference.

**Recommendation: D2a.** It keeps the property that matters most in
fynstall: the receipt describes everything the install changed, and the
uninstaller can undo it.

**Decided: D2a** (2026-10-08).

### D3: parameters, upgrades and kept data

The reference takes settings from three places. Its upgrade loses the
install directory's contents unless it copies them aside first.

- **D3a: declared parameters, and configuration outside the install
  directory** (recommended).

  ```yaml
  parameters:
    - name: server
      label: Server address
      default: "https://example.invalid"
    - name: token
      label: Access token
      secret: true                   # never logged, never in the receipt
      required: true
  ```

  - **Sources, highest first:** a command-line switch (`--token=…`, and
    `/Token=…` in NSIS compatibility mode, see L1); a file beside the
    installer (`fynstall-params.yml` by default, or a name the config sets);
    the value the existing install was given (upgrade); the default.
  - **The wizard** gets a generated page of the parameters, built with
    fynedesygn `forms`. A secret is a password entry.
  - **The values** reach the system only through actions (`config_file`,
    `run` args, `service` args). The receipt records which parameters were
    set, with secret values left out. On upgrade, the new installer reads
    non-secret values from the receipt and secret values from the existing
    config file, through the same `config_file` definition.
  - **Kept data** is anything outside the install directory that the config
    lists in `keep_on_uninstall`: configuration, logs, state. The install
    directory holds only what the installer can write again. With that rule,
    "run the old uninstaller, then install fresh" loses nothing, and the
    reference's temporary copy is not needed.
  - **An upgrade** stops the services, runs the old uninstaller with
    `--upgrade` (which removes what it installed, keeps kept paths, and
    does not remove the services' data), then installs with the gathered
    parameters.

- **D3b: preserve paths inside the install directory.** A `preserve:` list
  of install-relative paths that the uninstaller leaves during an upgrade.
  This matches the reference's layout more closely, but it makes the
  uninstaller's behaviour depend on why it was run, and an upgraded install
  then holds files that no receipt created.

**Recommendation: D3a.** The receipt keeps describing exactly what is in the
install directory.

**Decided: D3a** (2026-10-08).

## Leaf requirements

These fit into existing phases and do not change the structure. Each names
where it goes.

- **L1 NSIS-compatible switches.** The native switches are the same on
  every platform (`--yes`, `--dir`, `--<parameter>=…`). With
  `cli: { compat: nsis }`, the installer also accepts `/S` (silent),
  `/D=<dir>` (last argument, as NSIS requires), and `/<Name>=<value>` for
  each parameter, so existing automation, an MSI wrapper and a
  self-updating program run the new installer unchanged. The compatibility
  layer translates to the native switches and adds no behaviour of its own.
  It is accepted on every platform, but means something only where a caller
  uses NSIS syntax. Phase 7.
- **L2 Prerequisites.** Minimum OS version and allowed architectures,
  checked before the plan. Phase 7, and Linux where it applies.
- **L3 One installer at a time.** A lock per app ID and scope: a lock file
  on Linux, a named mutex on Windows. Phase 5.
- **L4 Permissions.** One `permissions` concept on a directory: who may
  read, who may write, as roles (`admins`, `service`, `users`). The Linux
  backend turns it into owner, group and mode; the Windows backend into an
  ACL. Not `icacls` arguments in the config. System scope only. Phase 5
  (Linux) and phase 7 (Windows).
- **L5 Migration from an older install.** A `migrate` action that moves a
  known old path into a new one, journalled. The new install's uninstaller
  does not put the old layout back; the summary page says so. Spec 002
  phase 3.
- **L6 Signing.** A `sign` command template in the config, run by the
  builder on the uninstaller, then on the installer, and on listed payload
  files before they are embedded. Secrets come from the environment, never
  from the config. Phase 7.
- **L7 Version resource and manifest.** Windows `VERSIONINFO` and an
  application manifest that requests elevation for system-only installers,
  derived from `app` and `install.scopes`. No config keys. Phase 7.
- **L8 Uninstall registry fields.** `DisplayName`, `DisplayVersion`,
  `Publisher`, `InstallLocation`, `DisplayIcon`, `EstimatedSize`,
  `UninstallString`, `QuietUninstallString`, `NoModify` and `NoRepair`,
  derived from `app`, the receipt and the install. No config keys. Phase 7
  (R18).
- **L10 Platform equivalents of existing keys.** `integration.desktop`
  makes a Start Menu shortcut on Windows. `integration.path_links` puts the
  program on `PATH`: a link in `{bin}` on Linux, and on Windows the user's
  or the machine's `PATH`, recorded and restored like any other change.
  Phase 7.
- **L9 MSI.** Out of scope. An MSI wrapper of the installer, as the
  reference uses, works through L1.

## How this changes spec 001

- **Phase 4 (GUI and mode selection)** gains the parameters page (D3). The
  mode selection is unchanged.
- **Phase 5 (system scope)** becomes the first phase with actions: a
  `service` action as a systemd system unit, and L3 and L4. The privileged
  helper (R13) applies actions as well as files.
- **Phase 6 (upgrade)** follows D3a: parameters are gathered, the old
  uninstaller runs with `--upgrade`, and kept paths survive.
- **Phase 7 (Windows)** gains D1 (it needs per-target payloads), the Windows
  backends for the actions, and L1, L2, L6, L7 and L8.
- **The Risks section's rule "runs no payload content during install"** is
  replaced by: "runs payload content only through a declared `run` action,
  shown on the summary page and in `--dry-run`, with an undo".

D1 is needed by phase 7 at the latest. It costs least before then, so it is
proposed as the first piece of work below.

## Phases

Each phase is one PR with its own desk check, as in spec 001.

1. **Per-target payloads (D1)**, before spec 001 phase 4. Config, manifest
   per target, build-time placeholders, validation. Desk check: one config
   that builds installers for two targets with different payloads. Windows
   builds wait for spec 001 phase 7, so the two targets are Linux amd64 and
   arm64, and the arm64 installer is run under `qemu-aarch64`. The Windows
   half is phase 7's "same config, unchanged" criterion.
2. **Parameters (D3a)**, with spec 001 phase 4: the schema, the sources, the
   wizard page and the CLI switches, and the `config_file` action. Desk
   check: an install that takes a value from each source and shows it in
   the written config file.
3. **Actions (D2a)**, with spec 001 phase 5: the action journal, `service`
   (systemd), `run` with `undo`, `migrate`, and stopping services before a
   replace. Desk check: a systemd service that runs after install and is
   gone after uninstall, with a pre-existing unit restored.
4. **The experiment**, after spec 001 phase 7: a fynstall config that
   reproduces the reference installer on Windows, compared in a VM against
   a checklist made from [Context](#context). The comparison covers
   behaviour, not file layout. Differences are written down, each as fixed,
   accepted or out of scope.

## Acceptance Criteria

Design:

- [x] D1, D2 and D3 are decided, and this spec records the choice and why.
- [x] Spec 001's phases 4–7 and its Risks section are updated to match.
- [x] The one-config rule is in `docs/config.md` and `.claude/CLAUDE.md`.

Platform independence (every phase):

- [ ] The examples' configs contain no OS-specific path and no `platform:`
      block, and build installers for Linux and Windows from the same file.
- [ ] Validation reports every `platform:` block and every OS-specific path
      outside `install.dir`.

Phase 1, per-target payloads:

- [x] `targets` on a payload entry, with `os/arch` patterns and `*`;
      `{os}`, `{arch}` and `{exe}` in `src`, `dst`, links and desktop
      `exec`. A malformed pattern, a filter that matches no target and an
      unknown placeholder are errors with their line
      (`TestTargetPatterns`).
- [x] A placeholder `src` is checked for each target it applies to, and the
      error names the target (`TestPerTargetSourcesAreCheckedPerTarget`).
- [x] One config stages a different payload for `linux/amd64`,
      `linux/arm64` and `windows/amd64`, with `.exe` names on Windows
      (`TestOneConfigStagesADifferentPayloadPerTarget`).
- [x] The builder makes one installer per target, each with its own
      manifest and its `target` recorded. The amd64 installer installs and
      uninstalls with the home unchanged, and its program prints
      `greet from linux/amd64`; the arm64-only file is not in it
      (`TestOneConfigInstallsEachTargetsOwnPayload`). The arm64 installer
      installs and uninstalls under `qemu-aarch64`, with an AArch64 program
      and the arm64-only file (`TestTheArm64InstallerInstallsUnderEmulation`).
      A mutation that ignored `targets` failed both.
- [x] An installer refuses a manifest staged for a target other than the
      one it was built for. The builder never produces one, so this guards a
      build fault; it cannot tell which machine it runs on, because under
      emulation `GOARCH` is the binary's (`TestAnInstallerRefusesAPayloadForAnotherTarget`).
- [x] Desk check: `make greet`, then `fynstall build --cli-only` in
      `examples/greet`. Install the amd64 installer, run `greet`, and
      uninstall it. Run the arm64 installer's `--dry-run` under
      `qemu-aarch64` and see the arm64-only file in its plan.

Experiment (phase 4):

- [ ] A fynstall config installs the reference program on Windows 11 per
      machine: files, service started with recovery actions, configuration
      written from a sidecar file and from switches, Uninstall registry
      entry, Start Menu entry where the reference has one.
- [ ] The same installer runs silently with the reference's switches, and
      the reference's self-update path (`/S` from the install directory)
      upgrades an install and keeps its configuration.
- [ ] Uninstall removes the service and everything the install created,
      keeps the data the reference keeps, and leaves no registry key
      behind. A registry export before the install and after the uninstall
      differs only in the kept data.
- [ ] Each difference from the reference is listed as fixed, accepted or
      out of scope.
- [ ] The same config, unchanged, builds a Linux installer that installs
      the program as a systemd service with the same parameters, and its
      uninstaller restores the system. The reference has no equivalent of
      this; it is the point of the experiment.

## Risks & Assumptions

- **Scope.** This turns fynstall from a file installer into a system
  configuration tool. The journal and the receipt are what keep that
  manageable: every action must have an undo, or it is not an action.
- **`run` actions are opaque.** The engine cannot see what a program does.
  An `undo` that does not undo leaves something behind. This is shown to
  the user, and the built-in action types are there so `run` is rare.
- **Secrets.** A secret parameter must never reach a log, the receipt, the
  plan output or an error message. A test must pass a known secret through
  every path and search all output for it.
- **Service managers differ.** systemd and the Windows SCM have different
  models for recovery, accounts and dependencies. The `service` action
  covers the common part (start, stop, restart on failure, an account), and
  anything more needs a `run`.
- **The reference is private.** This public spec describes it by behaviour
  only. The comparison checklist for phase 4 is kept with the reference,
  not in this repository.
- **Rollback.** Every phase is additive to the config format: a config
  without `actions`, `parameters` or `targets` keeps working as it does
  now.

## Alternatives Considered

- Considered generating an NSIS script from `fynstall.yaml` and building
  with `makensis` on Windows. Rejected because it gives two installers with
  two behaviours, and loses the journal, rollback and the wizard.
- Considered making fynstall an MSI generator (WiX). Rejected for the same
  reason, and because MSI does not exist on Linux.

## Verification

Filled in as each phase lands.

### Phase 1 (2026-10-08)

On CachyOS (amd64) with KDE Plasma 6. `make test` (with `-race`),
`make lint` (0 issues) and `govulncheck` (no vulnerabilities) pass.

- One `fynstall build --cli-only` in `examples/greet` made an amd64 and an
  arm64 installer from one config.
- The amd64 installer, run with `--yes` in the user's real home, installed
  `greet` and linked it into `~/.local/bin`. `greet` printed
  `greet from linux/amd64`, ahead of a `/usr/bin/greet` from another
  package. After the uninstall, `type greet` named `/usr/bin/greet` again,
  and the listings of `~/.local/share` and `~/.local/bin` matched those
  taken before.
- The arm64 installer's `--dry-run` under `qemu-aarch64` listed
  `share/arm64.txt`, which the amd64 plan does not have.
- Found on the way: `[bin/greet{exe}]` is not valid YAML, because `{` in a
  flow list starts a mapping. The example uses a block list and
  `docs/config.md` says how to quote it.
