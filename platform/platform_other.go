//go:build !linux && !windows

package platform

import (
	"fmt"
	"runtime"
)

// Integration is none: this OS has no backend.
const Integration = NoIntegration

// Services is none: this OS has no backend.
const Services = NoServices

func vars(string, func(string) string) (map[string]string, error) {
	return nil, fmt.Errorf("installing on %s is not supported yet", runtime.GOOS)
}

// SystemRoot is "": this OS has no system installs.
func SystemRoot(func(string) string) string { return "" }
