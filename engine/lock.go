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

// lockDir is where the lock files go: the user's runtime directory when it
// is set, else a directory of the user's own in the temporary directory.
// Neither is in the home, so a lock leaves nothing behind there.
func lockDir(env func(string) string) (string, error) {
	if d := env("XDG_RUNTIME_DIR"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "fynstall"), nil
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("fynstall-%d", os.Getuid())), nil
}
