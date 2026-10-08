//go:build !linux

package platform

import (
	"fmt"
	"runtime"
)

func vars(string, func(string) string) (map[string]string, error) {
	return nil, fmt.Errorf("installing on %s is not supported yet", runtime.GOOS)
}
