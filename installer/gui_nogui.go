//go:build nogui

package installer

import (
	"github.com/ushineko/fynstall/engine"
	"github.com/ushineko/fynstall/manifest"
)

// guiAvailable is false in a --cli-only build, which links no Fyne (R12).
const guiAvailable = false

// installGUI is never reached: chooseMode never picks the GUI here.
func installGUI(*manifest.Manifest, Payload, installFlags, Env) int { return exitUsage }

// notice is never reached: a CLI-only build never chooses the GUI.
func notice(string, string) {}

// uninstallGUI is never reached, for the same reason.
func uninstallGUI(*engine.Receipt, bool) int { return exitUsage }
