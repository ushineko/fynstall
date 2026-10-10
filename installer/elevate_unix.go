//go:build !windows

package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// elevator is the program that runs the helper as root: pkexec for the
// window, which asks in a dialog, and sudo for the command line, which
// asks in the terminal. FYNSTALL_ELEVATE names another, which the tests
// use to run the helper without root.
func elevator(gui bool, getenv func(string) string) (string, error) {
	prog := "sudo"
	if gui {
		prog = "pkexec"
	}
	if p := getenv("FYNSTALL_ELEVATE"); p != "" {
		prog = p
	}
	path, err := exec.LookPath(prog)
	if err != nil {
		return "", fmt.Errorf("this install is for everyone on this computer and needs an administrator, but %s is not installed", prog)
	}
	return path, nil
}

// launchHelper starts self under the elevation program. The helper reads
// its stdin and reports on its stdout.
func launchHelper(ctx context.Context, gui bool, getenv func(string) string, stderr io.Writer, self string, args []string) (io.WriteCloser, io.Reader, func() error, error) {
	prog, err := elevator(gui, getenv)
	if err != nil {
		return nil, nil, nil, err
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), prog, append([]string{self}, args...)...) // #nosec G204 -- this program, under the elevation program
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("start the helper: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("start the helper: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("start %s: %w", prog, err)
	}
	wait := func() error {
		err := cmd.Wait()
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 126 || exit.ExitCode() == 127) {
			// pkexec's codes for a dismissed or failed authentication.
			return errDenied
		}
		return err //nolint:wrapcheck // finish says what it means
	}
	return stdin, stdout, wait, nil
}

// helperForced is false: as root there is no helper, and the tests of one
// do not run as root.
func helperForced(func(string) string) bool { return false }

// helperChannel leaves the helper on its stdin and stdout.
func helperChannel(args []string, e Env) ([]string, Env) { return args, e }
