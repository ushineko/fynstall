//go:build !windows

package platform

import "os/exec"

// Background does nothing here: only Windows opens a console window for a
// program started without one.
func Background(*exec.Cmd) {}
