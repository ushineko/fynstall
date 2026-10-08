//go:build !linux

package installer

import "os"

// isTerminal is a character-device check until each platform gets its own
// (Windows in spec 001 phase 7).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
