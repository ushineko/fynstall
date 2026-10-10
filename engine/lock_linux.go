package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// OwnRunDir makes RunDir and checks it is a directory only this user can
// write: in a shared temporary directory it must not be one, or a link,
// that someone else put there.
func OwnRunDir(scope string, env func(string) string) (string, error) {
	dir := RunDir(scope, env)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("check %s: %w", dir, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Geteuid() || fi.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("%s is not a directory that only this user can write", dir)
	}
	return dir, nil
}

// Lock takes the lock of app id in scope and returns its release. It does
// not wait: a second installer is refused with ErrLocked, not queued. The
// lock is the kernel's, so it goes with the process that held it.
func Lock(id, scope string, env func(string) string) (func(), error) {
	dir, err := OwnRunDir(scope, env)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, id+"."+scope+".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { // #nosec G115 -- a file descriptor fits an int
		_ = f.Close()
		return nil, ErrLocked
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // #nosec G115 -- as above
		_ = f.Close()
	}, nil
}
