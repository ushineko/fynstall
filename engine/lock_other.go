//go:build !linux

package engine

// Lock does nothing yet off Linux; Windows gets a named mutex in spec 001
// phase 7.
func Lock(string, string, func(string) string) (func(), error) { return func() {}, nil }
