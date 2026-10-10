//go:build !linux

package engine

import (
	"fmt"
	"os"
)

// Lock does nothing yet off Linux; Windows gets a named mutex in spec 001
// phase 7.
func Lock(string, string, func(string) string) (func(), error) { return func() {}, nil }

// OwnRunDir is RunDir, made; Windows checks its owner in phase 7.
func OwnRunDir(scope string, env func(string) string) (string, error) {
	dir := RunDir(scope, env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", dir, err)
	}
	return dir, nil
}
