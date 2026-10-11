package installer

import (
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

// privileged reports whether the process runs as an administrator with its
// rights in force (elevated), which the window never does (R13).
func privileged() bool { return windows.GetCurrentProcessToken().IsElevated() }
