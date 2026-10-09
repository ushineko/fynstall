/*
Command beacon is the program fynstall's tests and desk checks of
install-time actions install (spec 002 phase 4a). It is pure Go with no
cgo.

	beacon serve           run as a service: say it is alive now and then
	beacon setup <file>    a run action: write file
	beacon teardown <file> its undo: remove file, and its directory if empty
	beacon goodbye         an uninstall hook: say it ran

setup fails when a file named fail-setup is beside <file>, and goodbye
fails when a file named fail-goodbye is in the directory it runs in, so the
tests can make each one fail.
*/
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "beacon:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: beacon serve | setup <file> | teardown <file> | goodbye")
	}
	switch args[0] {
	case "serve":
		return serve(out, 30*time.Second)
	case "setup":
		if len(args) != 2 {
			return errors.New("usage: beacon setup <file>")
		}
		return setup(args[1], out)
	case "teardown":
		if len(args) != 2 {
			return errors.New("usage: beacon teardown <file>")
		}
		return teardown(args[1], out)
	case "goodbye":
		if _, err := os.Stat("fail-goodbye"); err == nil {
			return errors.New("goodbye failed: fail-goodbye is here")
		}
		_, _ = fmt.Fprintln(out, "beacon: the uninstall hook ran")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func serve(out io.Writer, every time.Duration) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, os.Interrupt)
	t := time.NewTicker(every)
	defer t.Stop()
	_, _ = fmt.Fprintln(out, "beacon: alive")
	for {
		select {
		case <-stop:
			_, _ = fmt.Fprintln(out, "beacon: stopping")
			return nil
		case <-t.C:
			_, _ = fmt.Fprintln(out, "beacon: alive")
		}
	}
}

// setup writes file, which the installer names in the run action's
// arguments: writing where it is told is the point.
func setup(file string, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), "fail-setup")); err == nil { // #nosec G703 -- the path the installer passes
		return errors.New("setup failed: fail-setup is beside " + file)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil { // #nosec G703 -- as above
		return fmt.Errorf("setup: %w", err)
	}
	if err := os.WriteFile(file, []byte("set up by beacon\n"), 0o600); err != nil { // #nosec G703 -- as above
		return fmt.Errorf("setup: %w", err)
	}
	_, _ = fmt.Fprintln(out, "beacon: set up", file)
	return nil
}

func teardown(file string, out io.Writer) error {
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) { // #nosec G703 -- the path the installer passes
		return fmt.Errorf("teardown: %w", err)
	}
	_ = os.Remove(filepath.Dir(file)) // #nosec G703 -- only if empty
	_, _ = fmt.Fprintln(out, "beacon: tore down", file)
	return nil
}
