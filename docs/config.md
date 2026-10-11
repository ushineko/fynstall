# fynstall.yaml

`fynstall.yaml` describes one program and how to install it. `fynstall
build` reads it, and `fynstall validate` checks it without building. Paths
under `payload` are relative to the directory that holds the file.

Every problem is reported with the file, the line and the field, and all
problems are reported at once. An unknown key is an error, so a misspelt
optional key does not become a setting that is silently ignored.

`fynstall init` writes a commented example to start from.

**One config serves every platform.** Every key means the same thing on
Linux and on Windows. The config says what the program needs, such as a
launcher entry or a link on `PATH`, and the installer does it the way each
platform does. Paths are placeholders such as `{data}`, never platform paths.
Platform details that the installer can work out from the config, such as
the Windows Uninstall registry entry, have no keys of their own.

## Contents

- [app](#app)
- [install](#install)
- [payload](#payload)
- [integration](#integration)
- [parameters](#parameters)
- [actions](#actions)
- [uninstall](#uninstall)
- [ui](#ui)
- [targets](#targets)
- [Placeholders](#placeholders)
- [What an install writes](#what-an-install-writes)

## app

| Key | Required | Meaning |
|---|---|---|
| `id` | yes | A reverse-DNS ID with at least two segments, such as `io.example.hello`. On Linux it names the desktop entry, and Wayland compositors match it to the window's `app_id`. |
| `name` | yes | The name shown to the user. |
| `version` | yes | A version such as `1.2.3` or `1.2.3-rc.1`. |
| `publisher` | no | Who makes the program. |
| `icon` | no | A square PNG of at least 512 px, relative to `fynstall.yaml`. |
| `licence` | no | A text or Markdown file. The wizard shows it, and the install goes on only once it is accepted. |

The build resizes the icon to 16, 32, 48, 64, 128, 256 and 512 px. Each
size is installed into the hicolor icon theme under the app ID. KDE uses
the size that matches the size it asks for, and it only scales between the
sizes the theme declares, so one large icon is not enough.

The output files take their name from the last segment of the ID:
`io.example.hello` version 0.1.0 gives `hello-0.1.0-linux-amd64-installer`.

## install

| Key | Default | Meaning |
|---|---|---|
| `scopes` | `[user]` | The scopes a user may choose. The first one is the default. |
| `dir.user` | `{data}/{id}` | Where a per-user install goes. |
| `dir.system` | `/opt/{id}` | Where a system-wide install goes. |

A per-user install needs no elevation. A system install is for everyone
on the computer. The installer and the uninstaller stay the person's own
programs and ask for an administrator once, through `pkexec` in the window
and `sudo` on the command line; only the changes run as root. When both
scopes are offered, the wizard asks "Install for: Just me / Everyone on this
computer", and the command line takes `--scope system`. A config that
offers system scope cannot use `{home}`. See [platforms.md](platforms.md).

The user can choose another directory when the installer asks, or with
`--dir`.

## payload

A list of the files to install. Each entry has these keys:

| Key | Required | Meaning |
|---|---|---|
| `src` | yes | A file or a directory, relative to `fynstall.yaml`. |
| `dst` | yes | Where it goes, relative to the install directory. |
| `mode` | no | An octal mode such as `0755`, for every file in the entry. |
| `exclude` | no | Patterns that leave out files in a directory entry. |
| `targets` | no | The targets this entry is for, as `os/arch` patterns such as `linux/arm64` or `windows/*`. The default is every target. |

**One payload list for every target.** `src` and `dst` can use three
placeholders that the build resolves for each target: `{os}`, `{arch}`, and
`{exe}`, which is `.exe` for Windows and empty for everything else.

```yaml
payload:
  - src: build/{os}-{arch}/greet{exe}
    dst: bin/greet{exe}
  - src: notes/arm64.txt
    dst: share/arm64.txt
    targets: [linux/arm64]
```

`fynstall validate` checks that each `src` exists for every target it
applies to, and names the target in the error. An entry whose `targets`
match none of the config's targets is an error. The same placeholders work
in `integration.path_links` and in a desktop entry's `exec`.

**Quote a placeholder in a flow list.** Inside `[...]`, YAML reads `{` as
the start of a mapping, so `[bin/greet{exe}]` does not parse. Write
`["bin/greet{exe}"]`, or use a block list (`- bin/greet{exe}`).

**Files.** A `dst` that ends in `/` is a directory, and the file keeps its
own name in it.

**Directories.** A directory is copied with everything in it. An `exclude`
pattern matches a file's name or its path relative to `src`, with the
syntax of Go's `path.Match`. A pattern that matches a directory leaves out
the whole directory.

**Modes.** Go embedding does not keep file modes, so the build records one
for each file. Without `mode`, a file that starts as an ELF or PE
executable, or with `#!`, gets `0755`, and every other file gets `0644`.

**What is refused.** A `dst` must be a relative path with forward slashes
that stays inside the install directory. It cannot be inside `.fynstall`,
which fynstall uses for its own records. It cannot be named `go.mod`, or
contain a character that Go embedding refuses: a double quote, a single
quote, a backtick, `*`, `<`, `>`, `?`, `|`, `\` or `:`.
Two entries cannot install to the same `dst`.

**Symlinks.** A symlink inside a directory entry is installed as a link,
with its target as it was written. A bundled runtime has many, such as
`libffi.so -> libffi.so.8.4.0`. The build refuses a link when:

- its target is an absolute path;
- it resolves outside the directory entry it is in, including through
  another link on the way;
- it points at nothing, or at something `exclude` leaves out;
- it points at a directory that holds another directory link, which a
  copy could never finish;
- another entry installs a file under it.

An entry whose `src` is itself a link is refused: name the file it points
at. Making a link on Windows needs a privilege that most users do not have,
so a Windows installer gets a copy of what each link points at. A link to a
directory becomes a copy of that directory. Go embedding stores identical
files once, so the copies do not make the installer larger.

## integration

| Key | Meaning |
|---|---|
| `path_links` | Payload destinations to link into `{bin}`, each under its own base name. |
| `desktop` | Launcher entries. See below. |
| `keep_on_uninstall` | Paths that the uninstaller never touches, such as the program's own settings. The uninstaller prints them, so the user knows where their data is. |

**Links.** Each link is a symlink in `{bin}` (`~/.local/bin` for a per-user
install) that points at the installed file. Two links cannot have the same
name. If `{bin}` is not on the user's `PATH`, the installer says so.

**Desktop entries.** Each entry is written to
`{data}/applications/<id>.desktop`.

| Key | Required | Meaning |
|---|---|---|
| `id` | no | The file name without `.desktop`. The default is `app.id`. |
| `name` | yes | The name in the launcher. |
| `comment` | no | One line that describes the program. |
| `exec` | yes | The payload destination to run. The installer writes its absolute path. |
| `args` | no | Arguments after the program. |
| `categories` | no | Launcher categories, such as `Utility`. |
| `terminal` | no | `true` runs the program in a terminal. |
| `generic_name` | no | What kind of program it is, such as `Wallpaper manager`. |
| `keywords` | no | Words a launcher's search matches. |
| `startup_notify` | no | `true` when the program tells the desktop its window is up, so the launcher shows it is starting. Left out when unset. |
| `startup_wm_class` | no | The window class X11 and XWayland match to the entry. The default is the entry's `id`, which is what Fyne sets for the app ID; a `terminal` entry has none. |

The program's main window needs an entry whose ID is the app ID. On
Wayland, the compositor finds a window's icon through the desktop entry
whose name matches the window's `app_id`. Without that entry the taskbar
shows a generic icon. The `Icon` key of every entry is the app ID when
`app.icon` is set.

The installer quotes `exec` and `args` as the Desktop Entry Specification
requires, so an install directory with spaces in its path works.

After an install and after an uninstall, the installer runs `kbuildsycoca6`
if it is on `PATH`, so KDE shows the change without a new login. Other
desktops watch the directories themselves. `update-desktop-database` is not
run: it rebuilds only the MIME cache, and these entries declare no MIME
types.

## parameters

Values the installer is given or asks for, such as a server address or an
access token. Actions use them as `{param:<name>}`.

| Key | Meaning |
|---|---|
| `name` | Lowercase letters, digits and dashes. It is also the flag `--<name>`, so it cannot be one of the installer's own flags (`dir`, `yes`, `scope` and the others). |
| `label` | What a prompt and the wizard call it. The default is the name. |
| `description` | One line of help. |
| `default` | The value when nothing else gives one. |
| `secret` | The value is never printed, logged or recorded. A secret has no default, because the default would be published with the config. |
| `required` | The install stops, and names the flag, when no source gives a value. A required parameter has no default. |

**Where a value comes from**, first match wins:

1. a flag: `--token=…`;
2. `fynstall-params.yml` in the installer's directory, a flat map of names
   to values; a name that is not a parameter is an error;
3. a person, when the installer runs in a terminal without `--yes`; a
   secret is read without echo;
4. the default.

The install record keeps the values of non-secret parameters, for a later
upgrade, and only the names of secret ones.

## actions

Changes an install makes beyond copying files. Each action is recorded with
its undo, as a file is, so the uninstaller reverses it. The installer lists
every action before it starts: in `--dry-run`, before its question, and on
the wizard's summary page. Actions run in the order the config gives them,
after the files are in place. The uninstaller undoes them in reverse,
before it removes any file. A failed install undoes the actions it took.

```yaml
actions:
  - config_file:
      path: "{config}/greet/config.json"
      values:
        greeting: "{param:greeting}"
        token: "{param:token}"
  - service:
      name: beacon
      exec: bin/beacon
      args: [serve]
  - run:
      exec: bin/beacon
      args: [setup, "{config}/beacon/setup-done"]
      undo: [teardown, "{config}/beacon/setup-done"]
  - run:
      on: uninstall
      exec: bin/beacon
      args: [goodbye]
  - migrate:
      from: "{data}/beacon-0"
      to: "{data}/{id}/data"
```

**config_file** writes a configuration file from parameters, so the
installer does not run the program to produce one.

- `path` starts with `{config}/`, `{data}/` or `{home}/`. Configuration
  lives outside the install directory.
- `format` is `yaml` or `json`. Without it, the path's extension decides.
- `values` is one flat map. Keys are written sorted, and each value is
  quoted as the format requires.
- A file that holds a secret parameter is written readable by its owner
  only (`0600`).
- The uninstaller removes the file unless its path is under
  `keep_on_uninstall`. Keep it when the configuration must outlive an
  uninstall, or an upgrade, which runs the old uninstaller first.

**service** runs a payload program as a service.

| Key | Meaning |
|---|---|
| `name` | The service's name: letters, digits, `_`, `.` and `-`. |
| `description` | One line. The default is `app.name`. |
| `exec` | The payload destination to run. It can use `{os}`, `{arch}` and `{exe}`. |
| `args` | Its arguments. They can use the path placeholders. |
| `start` | Start it after the install. The default is `true`. |
| `restart` | `no`, `on-failure` (the default) or `always`. |

On Linux, a per-user install writes a systemd user unit,
`{config}/systemd/user/<name>.service`, and enables it, so it starts at
login. The uninstaller stops, disables and removes it. If a unit of that
name was there before, the install saves it, and the uninstaller puts it
back, enabled and running again if it was. The install needs `systemctl`
on `PATH`. A system install's service comes with system scope.

**run** runs a payload program, never a shell, in the install directory.
It is the last resort for a change no other action makes: the installer
cannot see what a program changed, so it shows the command before it runs
it.

| Key | Meaning |
|---|---|
| `exec` | The payload destination to run. It can use `{os}`, `{arch}` and `{exe}`. |
| `args` | Its arguments. They can use the path placeholders and `{param:<name>}`. |
| `undo` | The arguments that undo it, or the word `none`. Required. |
| `on` | `install` (the default) or `uninstall`. |
| `continue_on_error` | For `on: uninstall` only: let the uninstall go on when the program fails. |

- With `on: install`, the program runs during the install. A failure fails
  the install, which is undone, this program's own `undo` included. The
  uninstaller runs `undo`. Its output is shown with `--verbose`, and its
  last lines are part of the error when it fails.
- With `on: uninstall`, the program is an uninstall hook. It runs before
  the uninstaller removes anything, while the program's files are still
  there: for something the program made at runtime that the install cannot
  know about. A failure stops the uninstall with nothing removed, unless
  `continue_on_error: true`. The uninstaller says it will run it before it
  does.
- A secret parameter cannot be an argument. Arguments show in the process
  list, and an `undo` is kept in the install's record. Pass a secret
  through a `config_file`.
- `before_install` and `after_install` come with Go extensions (spec 003).

**migrate** moves data that an older version kept elsewhere.

- `from` and `to` start with `{config}/`, `{data}/` or `{home}/`.
- `to` must be at or under a `keep_on_uninstall` path, so the uninstaller
  leaves the moved data.
- When nothing is at `from`, the action does nothing. When both exist, the
  install is refused before it starts.
- A failed install moves the data back. An uninstall does not.

**One at a time.** While an installer or uninstaller of an app runs, a
second one for the same app and scope is refused. The lock is in
`$XDG_RUNTIME_DIR/fynstall`, so it leaves nothing in the home.

## uninstall

What the uninstaller does beyond undoing the install.

```yaml
uninstall:
  remove: ["python/**/__pycache__"]
```

| Key | Meaning |
|---|---|
| `remove` | Patterns, relative to the install directory, for files the program makes, such as a bytecode cache. The uninstaller removes them without asking. |

A pattern uses the syntax of Go's `path.Match` for each part between
slashes, and `**` matches any number of directories. A pattern cannot be
absolute or contain `..`. A pattern that matches a directory removes what is
in it, one file at a time, and then the directory. The uninstaller does not
follow links while it removes; it removes the link. It looks only in the
directories the install created, so a pattern never reaches files that were
in the install directory before the install. It never touches a path under
`keep_on_uninstall`.

**Leftovers.** Without a pattern, a file that the program made is not
deleted, because the install did not create it. The uninstaller lists what
is left in the directories the install created. The uninstall window then
asks whether to remove them too, with "Keep them" and "Remove them too"; on
the command line, `--remove-leftovers` deletes exactly those files. Nothing outside the install directory is ever
a leftover.

For a Python runtime, the bytecode cache can also be compiled when the
payload is built, with hash-based `.pyc` files
(`python -m compileall --invalidation-mode checked-hash`). Go embedding
does not keep file times, so a time-based `.pyc` would be written again. The
cache is then part of the install and of its uninstall.

## ui

What the wizard shows.

| Key | Meaning |
|---|---|
| `welcome` | A Markdown file for the first page. Without it, the page says what is installed, by whom, and that it needs no administrator rights. |
| `launch` | A payload destination that the finish page offers to start. It can use `{os}`, `{arch}` and `{exe}`. |

## targets

A list of `os/arch` pairs. Without it, and without `--target`, the build is
for the machine that runs it. The known targets are `linux/amd64`,
`linux/arm64` and `windows/amd64`. A Windows target builds with `--cli-only`
only; the wizard on Windows comes later in spec 001 phase 7.

The build makes one installer per target, each with the payload for its
target, and records the target in the installer's manifest.

## Placeholders

Path templates in `install.dir` and `keep_on_uninstall` can use these
names. Each one is resolved on the machine that runs the installer.

| Placeholder | Linux, per-user | Linux, system | Windows, per-user |
|---|---|---|---|
| `{id}`, `{name}`, `{version}` | From `app`. | From `app`. | From `app`. |
| `{home}` | `$HOME` | none: a validation error | `%USERPROFILE%` |
| `{data}` | `$XDG_DATA_HOME`, else `~/.local/share` | `/usr/local/share` | `%LOCALAPPDATA%`, else `%USERPROFILE%\AppData\Local` |
| `{config}` | `$XDG_CONFIG_HOME`, else `~/.config` | `/etc` | `%APPDATA%`, else `%USERPROFILE%\AppData\Roaming` |
| `{bin}` | `~/.local/bin` | `/usr/local/bin` | none: the install stops |

An unknown placeholder is a validation error. A relative `XDG_*` value is
ignored, as the XDG base directory specification requires; so is a relative
`LOCALAPPDATA` or `APPDATA`. Windows has no system scope yet. See
[platforms.md](platforms.md).

## What an install writes

Inside the install directory:

- the payload;
- `uninstall` (`uninstall.exe` on Windows), the uninstaller built with the
  installer;
- `.fynstall/receipt.json`, the record of every change the install made;
- `.fynstall/backup/`, a copy of every file the install replaced.

Outside it:

- `{data}/fynstall/installs/<id>.json` records where the install is. A later
  installer reads it to find the install and its uninstaller.
- `{data}/applications/<id>.desktop` for each desktop entry.
- `{data}/icons/hicolor/<size>x<size>/apps/<app id>.png` for each icon size.
- `{bin}/<name>` for each link.
- `{config}/systemd/user/<name>.service` for each service, on Linux;
  `/etc/systemd/system/<name>.service` for a system install.

On Windows the installer writes the index entry and nothing else outside
the install directory, apart from the configuration files: launcher
entries, icons and links are not applied there yet.

A file or link that is already at one of these paths is saved first. A
link is saved as its target, so the uninstaller puts back a link and not a
copy of the file it pointed at.

The uninstaller undoes the receipt's changes in reverse order. It removes
what the install created and puts back what it replaced. Then it removes
what `uninstall.remove` matches, the receipt, and the directories the
install created, if they are empty. It lists what the program left in the
install directory; see [uninstall](#uninstall).

## Upgrade, repair and downgrade

Running an installer while its app is installed replaces the installed
version: an upgrade when this one is newer, a repair when it is the same,
and a downgrade when it is older. A downgrade asks first; with `--yes`, it
needs `--downgrade`.

1. The installer reads the installed version's record. The parameters it
   was given are used again, so nobody is asked twice. A secret comes back
   from the configuration file the installed version wrote, through the
   same `config_file`.
2. It shows the plan for after the installed version is gone, and asks.
3. The installed version's own uninstaller removes it, with `--upgrade`
   (an older uninstaller gets `--quiet`). Its uninstall hooks and the
   undos of its `run` actions see `FYNSTALL_UNINSTALL_REASON=upgrade`.
   Kept paths stay, and what the program left stays where it is.
4. The installer plans again, checks the plan is the one it showed, and
   installs in the same place and scope.

A system install is replaced in one privileged helper, after one prompt.
