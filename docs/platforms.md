# What an install does on each platform

`fynstall.yaml` means the same on every platform (spec 002). This page
says what the platform backends do with it. Windows comes with spec 001
phase 7, and macOS later.

## Linux

### Paths

| | Per-user | System |
|---|---|---|
| Install directory | `{data}/{id}` | `/opt/{id}` |
| `{data}` | `$XDG_DATA_HOME`, else `~/.local/share` | `/usr/local/share` |
| `{config}` | `$XDG_CONFIG_HOME`, else `~/.config` | `/etc` |
| `{bin}` | `~/.local/bin` | `/usr/local/bin` |
| `{home}` | `$HOME` | none |
| Install index | `{data}/fynstall/installs/<id>.json` | `/var/lib/fynstall/installs/<id>.json` |
| Launcher entries | `{data}/applications` | `/usr/local/share/applications` |
| Icons | `{data}/icons/hicolor/<size>/apps` | `/usr/local/share/icons/hicolor/<size>/apps` |
| Services | systemd user units in `{config}/systemd/user`, started at login | system units in `/etc/systemd/system`, started at boot |
| Install lock | `$XDG_RUNTIME_DIR/fynstall` | `/run/fynstall` |

A system install stays out of `/usr/share` and `/usr/bin`, which belong to
the package manager. Desktops read `/usr/local/share` by default, so its
launcher entries and icons show without anything else.

After a change to launcher entries or icons, the installer and the
uninstaller run `kbuildsycoca6` when it is on `PATH`, so KDE shows the
change at once. Other desktops watch the directories.

### Elevation

A per-user install never asks for elevation.

A system install asks for an administrator once. The installer and the
uninstaller are the person's own processes from start to end: they plan,
ask and draw, and the window never runs as root. Only the changes do, in a
helper: the same program started again under `pkexec` (from the window,
which shows the desktop's password dialog) or `sudo` (on the command line).

- The installer writes the person's choices (scope, directory, parameter
  values) and the digest of the plan it showed into a file only that person
  can read, in a directory only they can write. It removes the file when
  the helper ends.
- The helper makes the plan again from its own payload, as root, and
  applies it only when its digest is the same. Otherwise nothing changes and
  it says the install is not the one that was shown.
- The helper reports each step back, and the person's window or terminal
  shows it. Cancel in the window closes the helper's input, and the helper
  stops and undoes what it did.
- The uninstaller of a system install starts its own helper the same way.
  When the program left files, the helper waits while the window asks about
  them, so "Remove them too" needs no second password.
- Services, `run` actions and uninstall hooks of a system install run as
  root.
