package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock takes the lock of app id in scope and returns its release. It does
// not wait: a second installer is refused with ErrLocked, not queued. The
// lock is the kernel's, so it goes with the process that held it.
func Lock(id, scope string, env func(string) string) (func(), error) {
	dir, err := lockDir(env)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	// In a shared temporary directory, the lock directory must be ours and
	// not a link someone else put there.
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("lock: %s is not a directory that only this user can write", dir)
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
