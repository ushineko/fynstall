//go:build !linux && !windows

package installer

import (
	"bufio"
	"os"
	"strings"
)

// isTerminal is a character-device check on an OS without its own.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// readSecret reads one line; this platform has no way yet to stop the echo.
func readSecret(f *os.File) (string, error) {
	s, err := bufio.NewReader(f).ReadString('\n')
	return strings.TrimRight(s, "\r\n"), err
}
