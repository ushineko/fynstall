//go:build !linux && !windows

package platform

import (
	"fmt"
	"runtime"
)

// HasDesktopIntegration is false: this OS has no backend.
const HasDesktopIntegration = false

func vars(string, func(string) string) (map[string]string, error) {
	return nil, fmt.Errorf("installing on %s is not supported yet", runtime.GOOS)
}

// SystemRoot is "": this OS has no system installs.
func SystemRoot(func(string) string) string { return "" }
