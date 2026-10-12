package installer

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ushineko/fynstall/platform"
)

// Messages of chooseMode that name what this OS calls things.
const (
	msgNoWindowAsAdmin = "the window does not run as an administrator: run the installer as yourself, and it asks for an administrator when it needs one"
	msgNoDisplay       = "--gui needs a display, and this session has no desktop"
)

//nolint:gochecknoglobals // handles of system libraries, loaded on first use
var (
	procFreeConsole          = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")
	procAttachConsole        = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procGetProcessWinStation = user32.NewProc("GetProcessWindowStation")
	procGetUserObjectInfo    = user32.NewProc("GetUserObjectInformationW")
)

// hasDisplay reports whether a window can be shown: the process is on a
// window station that has a desktop a person sees. A service and a session
// over SSH are on one that has none.
func hasDisplay(func(string) string) bool {
	station, _, _ := procGetProcessWinStation.Call()
	if station == 0 {
		return false
	}
	// USEROBJECTFLAGS: two BOOLs and the flags.
	var flags struct {
		inherit, reserved int32
		flags             uint32
	}
	const uoiFlags, wsfVisible = 1, 1
	var needed uint32
	ok, _, _ := procGetUserObjectInfo.Call(station, uoiFlags, uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), uintptr(unsafe.Pointer(&needed))) // #nosec G103 -- a struct of this call's own, filled by the system
	return ok != 0 && flags.flags&wsfVisible != 0
}

// ownConsole reports whether this process is alone on its console: Windows
// made the console for it, which is what happens when a person starts the
// program from Explorer, the Start Menu or Settings (spec 001 R19). A
// program typed into a console shares it with the shell.
func ownConsole() bool { return platform.ConsoleProcesses() == 1 }

// releaseConsole closes the console Windows made for this process, so no
// console window stays open behind the wizard.
func releaseConsole() { _, _, _ = procFreeConsole.Call() }

/*
borrowConsole gives a program with no console the console of whoever started
it, to print on (spec 006). An installer built --windowed is a program with
a window, so Windows gives it no console and nowhere to print: typed into a
console with --dry-run or --help it would say nothing. It takes the
console's output only, and only where it has none of its own: output sent to
a file or a pipe goes there as before, and the keyboard stays the shell's,
which does not wait for a program with a window. Started from the desktop
there is no console to borrow, and nothing happens.
*/
func borrowConsole() {
	if platform.ConsoleProcesses() != 0 {
		return
	}
	const attachParentProcess = ^uintptr(0)
	if ok, _, _ := procAttachConsole.Call(attachParentProcess); ok == 0 {
		return
	}
	for _, std := range []**os.File{&os.Stdout, &os.Stderr} {
		if usable(*std) {
			continue
		}
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			*std = f
		}
	}
}

// usable reports whether f is somewhere output can go: a file, a pipe or a
// console. A program with a window that nothing redirected has none.
func usable(f *os.File) bool {
	t, err := windows.GetFileType(windows.Handle(f.Fd()))
	return err == nil && t != windows.FILE_TYPE_UNKNOWN
}

// uninstallWindowAsAdmin is true: Settings > Apps starts the uninstaller of
// an install for everyone as an administrator, with --gui, and there is no
// asking it not to. A window that refused to open there left the program
// installed and showed nothing. The installer's wizard keeps the rule (R13):
// it is started by a person, as that person.
const uninstallWindowAsAdmin = true

// privileged reports whether the process runs as an administrator with its
// rights in force (elevated), which the window never does (R13).
func privileged() bool { return windows.GetCurrentProcessToken().IsElevated() }
