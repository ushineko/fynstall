package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrLocked is returned by Lock while another installer or uninstaller of
// the same app and scope runs (spec 002 L3).
var ErrLocked = errors.New("another installer or uninstaller of this program is running")

// RunDir is where the lock files go, and the list of leftovers a system
// uninstall hands to its second elevation: /run/fynstall for root in
// system scope, so every user's installer sees the same lock; else the
// user's runtime directory when it is set, else a directory of the user's
// own in the temporary directory. None is in the home, so nothing is left
// behind there.
func RunDir(scope string, env func(string) string) string {
	if scope == "system" && os.Geteuid() == 0 {
		return "/run/fynstall"
	}
	if d := env("XDG_RUNTIME_DIR"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "fynstall")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("fynstall-%d", os.Getuid()))
}
