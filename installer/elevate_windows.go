package installer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ushineko/fynstall/platform"
)

/*
On Windows the helper is started through a UAC prompt (ShellExecuteEx with
the verb "runas"), which gives the two processes no pipes. They talk over
two named pipes that the person's process makes before it starts the
helper: one the helper reads, which stands for its stdin, and one it
writes its events to. Two, because the parent stops the helper by closing
its input while it goes on reading the events.

The pipes have a random name, admit only the person, the administrators and
the system, and take no client from another computer. The helper opens them
so that the process on the other end cannot act with the helper's rights.
What it reads there is what it reads from stdin on Linux: the end of input,
which stops it, and one answer about leftovers.
*/

// pipePrefix starts the name of every pipe pair; the helper opens nothing
// else.
const pipePrefix = `\\.\pipe\fynstall-`

// helperPipeFlag hands the helper the name of its pipes.
const helperPipeFlag = "--helper-pipe="

// elevateDirect, as the value of FYNSTALL_ELEVATE, starts the helper as a
// plain child with no UAC prompt. The tests use it to run a system install
// under their own roots without an administrator.
const elevateDirect = "direct"

// elevatePrompt, as the value of FYNSTALL_ELEVATE, asks for a helper
// through the UAC prompt even in a process that needs none. It is for a
// check by hand of that path from an elevated console, where Windows does
// not show the prompt.
const elevatePrompt = "prompt"

//nolint:gochecknoglobals // system calls, loaded on first use
var (
	procShellExecuteEx       = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	procPipeServerProcessID  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNamedPipeServerProcessId")
	errHelperLeftBeforeStart = errors.New("the privileged helper stopped before it started")
)

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size       uint32
	mask       uint32
	hwnd       uintptr
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instApp    uintptr
	idList     uintptr
	class      *uint16
	keyClass   uintptr
	hotKey     uint32
	icon       uintptr
	process    windows.Handle
}

// newPipe makes the server end of one pipe of the pair. out is true for
// the pipe this process writes.
func newPipe(name string, out bool, sa *windows.SecurityAttributes) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("pipe name: %w", err)
	}
	access := uint32(windows.PIPE_ACCESS_INBOUND)
	if out {
		access = windows.PIPE_ACCESS_OUTBOUND
	}
	h, err := windows.CreateNamedPipe(p, access|windows.FILE_FLAG_FIRST_PIPE_INSTANCE|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS, 1, 64<<10, 64<<10, 0, sa)
	if err != nil {
		return 0, fmt.Errorf("make the helper's pipe: %w", err)
	}
	return h, nil
}

// pipeSecurity admits the system, the administrators (the helper may run as
// another person who is one) and the person this process runs as.
func pipeSecurity() (*windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("find who runs this program: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, fmt.Errorf("make the helper's pipe: %w", err)
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

// connect waits for the helper to open the pipe h, and gives up when the
// helper's process ends first or ctx is done.
func connect(ctx context.Context, h, process windows.Handle) error {
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("wait for the helper: %w", err)
	}
	defer func() { _ = windows.CloseHandle(ev) }()
	ov := windows.Overlapped{HEvent: ev}
	switch err := windows.ConnectNamedPipe(h, &ov); {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return nil
	case !errors.Is(err, windows.ERROR_IO_PENDING):
		return fmt.Errorf("wait for the helper: %w", err)
	}
	for {
		// Wake up now and then to look at ctx.
		const poll = 200
		which, err := windows.WaitForMultipleObjects([]windows.Handle{ev, process}, false, poll)
		switch {
		case err != nil:
			return fmt.Errorf("wait for the helper: %w", err)
		case which == windows.WAIT_OBJECT_0:
			return nil
		case which == windows.WAIT_OBJECT_0+1:
			_ = windows.CancelIoEx(h, &ov)
			return errHelperLeftBeforeStart
		case ctx.Err() != nil:
			_ = windows.CancelIoEx(h, &ov)
			return fmt.Errorf("stopped: %w", ctx.Err())
		}
	}
}

// launchHelper starts self as an administrator and returns the pipe the
// helper reads, the pipe it reports on, and a wait for its end.
func launchHelper(ctx context.Context, _ bool, getenv func(string) string, stderr io.Writer, self string, args []string) (io.WriteCloser, io.Reader, func() error, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, nil, nil, fmt.Errorf("name the helper's pipe: %w", err)
	}
	name := pipePrefix + hex.EncodeToString(random[:])
	sa, err := pipeSecurity()
	if err != nil {
		return nil, nil, nil, err
	}
	toHelper, err := newPipe(name+"-in", true, sa)
	if err != nil {
		return nil, nil, nil, err
	}
	fromHelper, err := newPipe(name+"-out", false, sa)
	if err != nil {
		_ = windows.CloseHandle(toHelper)
		return nil, nil, nil, err
	}
	closePipes := func() {
		_ = windows.CloseHandle(toHelper)
		_ = windows.CloseHandle(fromHelper)
	}
	args = append(append([]string{}, args...), helperPipeFlag+name)

	process, wait, err := startHelper(getenv, stderr, self, args)
	if err != nil {
		closePipes()
		return nil, nil, nil, err
	}
	for _, h := range []windows.Handle{toHelper, fromHelper} {
		if err := connect(ctx, h, process); err != nil {
			closePipes()
			if werr := wait(); errors.Is(err, errHelperLeftBeforeStart) && werr != nil {
				err = fmt.Errorf("%w (%w)", err, werr)
			}
			return nil, nil, nil, err
		}
	}
	return os.NewFile(uintptr(toHelper), name+"-in"), os.NewFile(uintptr(fromHelper), name+"-out"), wait, nil
}

// startHelper starts the helper's process and returns a handle to wait on
// while it connects, and a wait for its end.
func startHelper(getenv func(string) string, stderr io.Writer, self string, args []string) (windows.Handle, func() error, error) {
	if getenv("FYNSTALL_ELEVATE") == elevateDirect {
		cmd := exec.CommandContext(context.Background(), self, args...) // #nosec G204 -- this program
		cmd.Stderr = stderr
		platform.Background(cmd)
		if err := cmd.Start(); err != nil {
			return 0, nil, fmt.Errorf("start the helper: %w", err)
		}
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid)) // #nosec G115 -- a process ID
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return 0, nil, fmt.Errorf("start the helper: %w", err)
		}
		return h, func() error {
			defer func() { _ = windows.CloseHandle(h) }()
			return cmd.Wait() //nolint:wrapcheck // finish says what it means
		}, nil
	}

	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, err := windows.UTF16PtrFromString(self)
	if err != nil {
		return 0, nil, fmt.Errorf("start the helper: %w", err)
	}
	params, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return 0, nil, fmt.Errorf("start the helper: %w", err)
	}
	const (
		noCloseProcess = 0x00000040 // return the process handle
		noAsync        = 0x00000100 // finish before returning: this thread has no message loop
		hide           = 0          // the helper is a console program; its console stays hidden
	)
	info := shellExecuteInfo{mask: noCloseProcess | noAsync, verb: verb, file: file, parameters: params, show: hide}
	info.size = uint32(unsafe.Sizeof(info))
	ok, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))) // #nosec G103 -- a struct of this call's own
	switch {
	case ok == 0 && errors.Is(callErr, windows.ERROR_CANCELLED):
		// The person closed the prompt, or is not allowed to answer it.
		return 0, nil, errDenied
	case ok == 0:
		return 0, nil, fmt.Errorf("ask for an administrator: %w", callErr)
	case info.process == 0:
		return 0, nil, errors.New("ask for an administrator: Windows started no process")
	}
	process := info.process
	return process, func() error {
		defer func() { _ = windows.CloseHandle(process) }()
		if _, err := windows.WaitForSingleObject(process, windows.INFINITE); err != nil {
			return fmt.Errorf("wait for the helper: %w", err)
		}
		var code uint32
		if err := windows.GetExitCodeProcess(process, &code); err != nil {
			return fmt.Errorf("wait for the helper: %w", err)
		}
		if code != 0 {
			return fmt.Errorf("exit status %d", code)
		}
		return nil
	}, nil
}

// helperForced reports whether the tests ask for a helper in a process
// that needs none: developers run them from elevated consoles too.
func helperForced(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	v := getenv("FYNSTALL_ELEVATE")
	return v == elevateDirect || v == elevatePrompt
}

// testVariables move an install under the tests' own roots.
var testVariables = []string{"FYNSTALL_TEST_SYSTEM_ROOT", "FYNSTALL_TEST_REGISTRY_ROOT", "FYNSTALL_ELEVATE"} //nolint:gochecknoglobals // a fixed list

/*
helperChannel is the helper's side. When args name a pipe pair, it opens
them and returns e reading and writing them, with the flag taken out of
args. Otherwise it returns both as they are.

A helper that has more rights than the process on the other end of its
pipes was started through a UAC prompt, by a person or a program that could
not make these changes itself. Such a helper drops the tests' variables, so
that nobody sends an administrator's writes, or its search for an installed
uninstaller, to a place of their own choosing.
*/
func helperChannel(args []string, e Env) ([]string, Env) {
	at := -1
	for i, a := range args {
		if strings.HasPrefix(a, helperPipeFlag) {
			at = i
		}
	}
	if at < 0 {
		return args, e
	}
	name := strings.TrimPrefix(args[at], helperPipeFlag)
	rest := append(append([]string{}, args[:at]...), args[at+1:]...)
	fail := func(err error) ([]string, Env) {
		_, _ = fmt.Fprintf(e.Err, "helper: %v\n", err)
		os.Exit(exitFail)
		return nil, e
	}
	if !strings.HasPrefix(name, pipePrefix) || strings.ContainsAny(strings.TrimPrefix(name, pipePrefix), `\/`) {
		return fail(fmt.Errorf("%q is not a helper's pipe", name))
	}
	in, err := openPipe(name+"-in", windows.GENERIC_READ)
	if err != nil {
		return fail(err)
	}
	out, err := openPipe(name+"-out", windows.GENERIC_WRITE)
	if err != nil {
		return fail(err)
	}
	if privileged() && !serverElevated(in) {
		for _, v := range testVariables {
			_ = os.Unsetenv(v)
		}
	}
	e.In, e.Out = os.NewFile(uintptr(in), name+"-in"), os.NewFile(uintptr(out), name+"-out")
	e.Interactive, e.OutTerminal, e.OwnConsole = false, false, false
	return rest, e
}

// openPipe opens the helper's end of a pipe. The server gets no right to
// act as this process (SECURITY_ANONYMOUS).
func openPipe(name string, access uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, fmt.Errorf("pipe name: %w", err)
	}
	h, err := windows.CreateFile(p, access, 0, nil, windows.OPEN_EXISTING,
		windows.SECURITY_SQOS_PRESENT|windows.SECURITY_ANONYMOUS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", name, err)
	}
	return h, nil
}

// serverElevated reports whether the process that made the pipe h runs
// elevated. When that cannot be found out, it says no.
func serverElevated(h windows.Handle) bool {
	var pid uint32
	if ok, _, _ := procPipeServerProcessID.Call(uintptr(h), uintptr(unsafe.Pointer(&pid))); ok == 0 { // #nosec G103 -- a number of this call's own
		return false
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(process) }()
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer func() { _ = token.Close() }()
	return token.IsElevated()
}
