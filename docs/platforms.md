# What an install does on each platform

`fynstall.yaml` means the same on every platform (spec 002). This page
says what the platform backends do with it. Windows arrives in parts with
spec 001 phase 7; the Windows section says what is there. macOS is later.

## Linux

### Paths

| | Per-user | System |
|---|---|---|
| Install directory | `{programs}/{id}` | `{programs}/{id}` |
| `{programs}` | the same as `{data}` | `/opt` |
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

## Windows

Windows has per-user installs and installs for everyone on the computer,
from the wizard and from the command line, with a Start Menu shortcut, an
entry in Settings > Apps and an entry on `PATH`. Services follow in spec 001
phase 7.

### Paths

| | Per-user | System |
|---|---|---|
| Install directory | `{programs}\{id}` | `{programs}\{id}` |
| `{programs}` | `{data}\Programs` | `%ProgramFiles%` |
| `{home}` | `%USERPROFILE%` | none |
| `{data}` | `%LOCALAPPDATA%`, else `%USERPROFILE%\AppData\Local` | `%ProgramData%` |
| `{config}` | `%APPDATA%`, else `%USERPROFILE%\AppData\Roaming` | `%ProgramData%` |
| `{bin}` | none | none |
| Install index | `{data}\fynstall\installs\<id>.json` | `%ProgramData%\fynstall\installs\<id>.json` |
| Uninstaller | `<install directory>\uninstall.exe` | the same |
| Launcher entries | `{config}\Microsoft\Windows\Start Menu\Programs\<name>.lnk` | the Start Menu of all users, under `%ProgramData%` |
| Icon | `<install directory>\.fynstall\app.ico` | the same |
| Links | the directory of each link's program, on the user's `PATH` | the same, on the computer's `PATH` |
| Record for Settings > Apps | `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\<id>` | the same key under `HKLM` |
| `PATH` | `HKCU\Environment` | `HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment` |
| Install lock | the named mutex `Local\fynstall-<id>-user` | `Global\fynstall-<id>-system` |

A system install asks Windows where the machine's folders are. It does not
read `ProgramFiles` or `ProgramData` from the environment, so a variable
cannot send an administrator's writes elsewhere. Windows has one data
folder for the machine, so `{data}` and `{config}` are the same there.

`{data}` is the local folder, which stays on the machine. `{config}` is the
roaming folder, which follows the profile, as settings do. A relative value
of `LOCALAPPDATA` or `APPDATA` is ignored.

Windows has no directory of links, so there is no `{bin}`. A template that
names `{bin}` fails with "no value for {bin}".

A full installer, with the wizard, builds on Windows for Windows and needs a
C compiler (`gcc` on `PATH`, as MSYS2 provides). `fynstall build --cli-only
--target windows/amd64` builds from Linux and from Windows, with no C
compiler.

### Elevation

A per-user install never asks for elevation.

A system install asks for an administrator once, through the UAC prompt. As
on Linux, the installer and the uninstaller stay the person's own
processes, and only the changes run as an administrator, in a helper: the
same program started again with the verb `runas`.

- The UAC prompt gives the two processes no pipes, so they talk over two
  named pipes that the installer makes first: one that the helper reads and
  one that it reports on. The pipes have a random name, admit only the
  person, the administrators and the system, and take no client from another
  computer. The helper opens them so that the installer cannot act with the
  helper's rights.
- The plan file, the digest check, the events, Cancel and the question
  about leftovers work as on Linux. Closing the helper's input stops it.
- When the person closes the UAC prompt, nothing changes and the installer
  says that an administrator did not allow it.
- A program that already runs as an administrator (an elevated console)
  makes the changes itself, with no helper and no prompt.
- The record of a system install is in `%ProgramData%`, where any user can
  make files. The helper makes the record the administrators' own, and an
  installer takes a record only when an administrator or the system owns
  it. Otherwise it stops and names the file.
- A helper that has more rights than the process that started it ignores
  the variables the tests use to move an install
  (`FYNSTALL_TEST_SYSTEM_ROOT`, `FYNSTALL_TEST_REGISTRY_ROOT`,
  `FYNSTALL_ELEVATE`).

A system install in a directory that users can write (one chosen with
`--dir` outside `%ProgramFiles%`) leaves its uninstaller where a user can
replace it, and Settings > Apps runs that file. Keep system installs under
`%ProgramFiles%`.

### The wizard or the command line

The installer is a console program, so that its command line prints and
asks in `cmd` and PowerShell like any other. It chooses its front end by who
started it:

- **From Explorer, the Start Menu or Settings**, Windows makes a console for
  the program alone. The installer sees that it is alone on its console,
  closes the console and opens the wizard. The console window shows for a
  moment first.
- **From a console**, the shell shares its console with the installer, and
  the installer runs on the command line. `--gui` opens the wizard from
  there.
- **Over SSH or as a service** there is no desktop, and the installer runs
  on the command line and asks nothing.
- **As an administrator** (an elevated console, or "Run as administrator")
  the installer runs on the command line, and `--gui` is refused: the window
  does not run with an administrator's rights.

The uninstaller chooses the same way. The Uninstall button in Settings >
Apps opens its window, which asks once.

A problem found before the wizard can open is shown in a small window, as a
program started from the desktop has no console to print it on.

### The Start Menu, Settings and PATH

The config keys are the same as on Linux, and the Windows backend does what
Windows expects for each.

- **`integration.desktop`** makes a Start Menu shortcut for each entry,
  named for the entry. The installer writes it through the Windows shell.
  `categories`, `keywords` and the other launcher keys have no meaning in a
  shortcut and are not used.
- **`app.icon`** becomes one `.ico` file with the sizes 16, 32, 48, 64 and
  256, made when the installer is built. The shortcut and Settings > Apps
  show it. The installer and the uninstaller carry the same images as a
  resource, so Explorer shows the icon for the `.exe` files too. The build
  writes the resource itself, on Linux as on Windows, with no other tool.
- **`integration.path_links`** puts the directory of each named program on
  the user's `PATH`. Windows finds a program by its file name, so there is
  no link. A console that is opened after the install has the new `PATH`.
- **Every install** has an entry in Settings > Apps. Its fields come from
  `app` and the install: name, version, publisher, location, icon, size,
  and the commands that run the installed `uninstall.exe`. No config key
  sets them.

The plan lists the shortcut, the registry key and the `PATH` entry before
the install starts, and `--dry-run` prints them.

### What the uninstaller puts back

The receipt records each registry key that the install made and the earlier
content of each value that it wrote. The uninstaller writes those values
back and removes the keys that it made, when they are empty. A shortcut that
was already there comes back as it was.

`PATH` is different, because other installers change it too. The installer
adds its directory at the end and records only that directory. The
uninstaller removes that one entry and leaves the rest of the value as it
finds it. A directory that was on `PATH` before the install is not added
again, and the uninstaller does not remove it.

### The uninstaller removes itself

Windows does not delete the file of a running program, and `uninstall.exe`
is one of the files that the uninstaller removes. Windows does allow the
file to move. The uninstaller moves its own file to the temporary directory
as `fynstall-removed-<number>.exe`. The install directory is then empty, and
the uninstaller removes it before it exits.

The uninstaller marks the moved file for deletion at the next start of the
computer. Windows accepts that request only from an administrator. For
other users the moved file stays in the temporary directory.

The move is a rename, so the temporary directory and the install directory
must be on the same drive. On different drives the uninstaller reports that
it did not remove `uninstall.exe`, and leaves it and the install directory.

### One installer at a time

The lock is a named mutex. Windows removes it when the process that holds it
ends, so a stopped installer leaves no lock behind. A second installer or
uninstaller of the same program stops with the same message as on Linux.
