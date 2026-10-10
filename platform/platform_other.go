//go:build !linux

package platform

import (
	"fmt"
	"runtime"
)

func vars(string, func(string) string) (map[string]string, error) {
	return nil, fmt.Errorf("installing on %s is not supported yet", runtime.GOOS)
}

// SystemRoot is "": system installs off Linux come with spec 001 phase 7.
func SystemRoot(func(string) string) string { return "" }
