//go:build !linux && !windows

package engine

import (
	"fmt"
	"os"
)

// Lock does nothing on an OS without a backend.
func Lock(string, string, func(string) string) (func(), error) { return func() {}, nil }

// OwnRunDir is RunDir, made.
func OwnRunDir(scope string, env func(string) string) (string, error) {
	dir := RunDir(scope, env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", dir, err)
	}
	return dir, nil
}
