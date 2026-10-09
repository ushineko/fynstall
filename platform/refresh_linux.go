package platform

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RefreshMenu asks the desktop to read launcher entries and icons again,
// so an install or uninstall shows without a logout. On KDE that is
// kbuildsycoca6; other desktops watch the directories themselves. A tool
// that is not installed is skipped: it is a missing desktop, not an error.
//
// update-desktop-database is not run. It rebuilds only the MIME cache, and
// the entries fynstall writes declare no MIME types, so running it would
// change a file the install had no reason to touch.
func RefreshMenu() error {
	tool, err := exec.LookPath("kbuildsycoca6")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, tool).CombinedOutput(); err != nil { // #nosec G204 -- a fixed tool found on PATH
		return fmt.Errorf("%s: %w: %s", tool, err, out)
	}
	return nil
}
