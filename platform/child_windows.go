package platform

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList") //nolint:gochecknoglobals // a system call, loaded on first use

// ConsoleProcesses is how many processes share this process's console: 0
// when it has none, and 1 when Windows made the console for this process
// alone, as it does for a console program started from Explorer.
func ConsoleProcesses() int {
	var ids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids))) // #nosec G103 -- a buffer of this call's own
	return int(n)                                                                                  // #nosec G115 -- a count of processes
}

// Background keeps a console program that this process starts and waits for
// from opening a console window of its own. That happens when this process
// has no console, as an installer showing its wizard does. With a console,
// the child shares it and cmd is left alone.
func Background(cmd *exec.Cmd) {
	if ConsoleProcesses() != 0 {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
