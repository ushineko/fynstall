package engine

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// OwnRunDir makes RunDir. The temporary directory it is in is the user's
// own on Windows, so there is no shared directory to guard against.
func OwnRunDir(scope string, env func(string) string) (string, error) {
	dir := RunDir(scope, env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", dir, err)
	}
	return dir, nil
}

// Lock takes the lock of app id in scope and returns its release. It does
// not wait: a second installer is refused with ErrLocked, not queued. The
// lock is a named mutex, held by existing: Windows removes it when its last
// handle closes, so it goes with the process that held it. A per-user lock
// is in the session's namespace and a system one in the machine's, so every
// user's installer sees the same system lock (spec 002 L3).
func Lock(id, scope string, _ func(string) string) (func(), error) {
	space := `Local\`
	if scope == "system" {
		space = `Global\`
	}
	name, err := windows.UTF16PtrFromString(space + "fynstall-" + id + "-" + scope)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	h, err := windows.CreateMutex(nil, false, name)
	switch {
	case errors.Is(err, windows.ERROR_ALREADY_EXISTS):
		_ = windows.CloseHandle(h)
		return nil, ErrLocked
	case err != nil:
		return nil, fmt.Errorf("lock: %w", err)
	}
	return func() { _ = windows.CloseHandle(h) }, nil
}
