package installer

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// isTerminal reports whether f is a terminal a person can answer on. A
// character device is not enough: /dev/null is one too.
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS) // #nosec G115 -- a file descriptor fits in an int
	return err == nil
}

// readSecret reads one line from f without echoing it, so a secret typed
// at a prompt is not shown on the screen.
func readSecret(f *os.File) (string, error) {
	fd := int(f.Fd()) // #nosec G115 -- a file descriptor fits in an int
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	quiet := *old
	quiet.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &quiet); err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	defer func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, old) }()
	var b []byte
	one := make([]byte, 1)
	for {
		n, err := f.Read(one)
		if n == 0 || err != nil || one[0] == '\n' {
			break
		}
		b = append(b, one[0])
	}
	return strings.TrimRight(string(b), "\r"), nil
}
