package installer

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// isTerminal reports whether f is a console a person can answer on. A
// character device is not enough: NUL is one too.
func isTerminal(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}

// readSecret reads one line from f without echoing it, so a secret typed
// at a prompt is not shown on the screen.
func readSecret(f *os.File) (string, error) {
	h := windows.Handle(f.Fd())
	var old uint32
	if err := windows.GetConsoleMode(h, &old); err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	if err := windows.SetConsoleMode(h, old&^windows.ENABLE_ECHO_INPUT); err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	defer func() { _ = windows.SetConsoleMode(h, old) }()
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
