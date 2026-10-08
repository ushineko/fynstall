# fynstall.yaml

`fynstall.yaml` describes one program and how to install it. `fynstall
build` reads it, and `fynstall validate` checks it without building. Paths
under `payload` are relative to the directory that holds the file.

Every problem is reported with the file, the line and the field, and all
problems are reported at once. An unknown key is an error, so a misspelt
optional key does not become a setting that is silently ignored.

`fynstall init` writes a commented example to start from.

## Contents

- [app](#app)
- [install](#install)
- [payload](#payload)
- [integration](#integration)
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
| `keep_on_uninstall` | Paths that the uninstaller never touches, such as the program's own settings. The uninstaller prints them, so the user knows where their data is. |

Launcher entries, icons and links on `PATH` come with spec 001 phase 2.

## targets

A list of `os/arch` pairs. Without it, and without `--target`, the build is
for the machine that runs it. The known targets are `linux/amd64`,
`linux/arm64` and `windows/amd64`. Windows builds come with spec 001 phase 7.

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

Outside it, `{data}/fynstall/installs/<id>.json` records where the install
is. A later installer reads it to find the install and its uninstaller.

The uninstaller undoes the receipt's changes in reverse order. It removes
what the install created and puts back what it replaced. Then it removes the
receipt and the directories the install created, if they are empty.
