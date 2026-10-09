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

A per-user install needs no elevation. System scope is not available yet;
spec 001 phase 5 adds it.

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
Symlinks in the payload are refused, not followed. Two entries cannot
install to the same `dst`.

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
its undo, as a file is, so the uninstaller reverses it. `config_file` is
the only type so far; `service`, `run` and `migrate` come with spec 002
phase 4.

```yaml
actions:
  - config_file:
      path: "{config}/greet/config.json"
      values:
        greeting: "{param:greeting}"
        token: "{param:token}"
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

## ui

What the wizard shows.

| Key | Meaning |
|---|---|
| `welcome` | A Markdown file for the first page. Without it, the page says what is installed, by whom, and that it needs no administrator rights. |
| `launch` | A payload destination that the finish page offers to start. It can use `{os}`, `{arch}` and `{exe}`. |

## targets

A list of `os/arch` pairs. Without it, and without `--target`, the build is
for the machine that runs it. The known targets are `linux/amd64`,
`linux/arm64` and `windows/amd64`. Windows builds come with spec 001 phase 7.

The build makes one installer per target, each with the payload for its
target, and records the target in the installer's manifest.

## Placeholders

Path templates in `install.dir` and `keep_on_uninstall` can use these
names. Each one is resolved on the machine that runs the installer.

| Placeholder | Linux, per-user |
|---|---|
| `{id}`, `{name}`, `{version}` | From `app`. |
| `{home}` | `$HOME` |
| `{data}` | `$XDG_DATA_HOME`, else `~/.local/share` |
| `{config}` | `$XDG_CONFIG_HOME`, else `~/.config` |
| `{bin}` | `~/.local/bin` |

An unknown placeholder is a validation error. A relative `XDG_*` value is
ignored, as the XDG base directory specification requires.

## What an install writes

Inside the install directory:

- the payload;
- `uninstall`, the uninstaller built with the installer;
- `.fynstall/receipt.json`, the record of every change the install made;
- `.fynstall/backup/`, a copy of every file the install replaced.

Outside it:

- `{data}/fynstall/installs/<id>.json` records where the install is. A later
  installer reads it to find the install and its uninstaller.
- `{data}/applications/<id>.desktop` for each desktop entry.
- `{data}/icons/hicolor/<size>x<size>/apps/<app id>.png` for each icon size.
- `{bin}/<name>` for each link.

A file or link that is already at one of these paths is saved first. A
link is saved as its target, so the uninstaller puts back a link and not a
copy of the file it pointed at.

The uninstaller undoes the receipt's changes in reverse order. It removes
what the install created and puts back what it replaced. Then it removes the
receipt and the directories the install created, if they are empty.
