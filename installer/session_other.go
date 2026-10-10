//go:build !windows

package installer

import "os"

// Messages of chooseMode that name what this OS calls things.
const (
	msgNoWindowAsAdmin = "the window does not run as root: run the installer as yourself, and it asks for an administrator when it needs one"
	msgNoDisplay       = "--gui needs a display, and neither DISPLAY nor WAYLAND_DISPLAY is set"
)

// hasDisplay reports whether a window can be shown: a Wayland or an X11
// display is named.
func hasDisplay(getenv func(string) string) bool {
	return getenv != nil && (getenv("WAYLAND_DISPLAY") != "" || getenv("DISPLAY") != "")
}

// ownConsole is false: a terminal here always belongs to the shell that
// started the program (see the Windows file).
func ownConsole() bool { return false }

// releaseConsole does nothing here.
func releaseConsole() {}

// privileged reports whether the process runs as root, which the window
// never does (spec 001 R13).
func privileged() bool { return os.Geteuid() == 0 }
