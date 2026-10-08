package installer

import (
	"os"

	"golang.org/x/sys/unix"
)

// isTerminal reports whether f is a terminal a person can answer on. A
// character device is not enough: /dev/null is one too.
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS) // #nosec G115 -- a file descriptor fits in an int
	return err == nil
}
