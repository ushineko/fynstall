package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
WriteShortcut writes a shortcut through the shell's own IShellLink object
(spec 001 R18), so the file is in whatever form this version of Windows
writes. The object is driven through its method table directly: that needs
no C compiler, which the CLI-only build does not have (R12).

The shortcut is saved under a temporary name and renamed over path, so path
is never half written.
*/
func WriteShortcut(path string, s Shortcut) (err error) {
	// COM belongs to a thread, and a goroutine may change thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch cerr := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); {
	case cerr == nil, errors.Is(cerr, syscall.Errno(1)): // S_FALSE: already initialised on this thread
		defer windows.CoUninitialize()
	case errors.Is(cerr, syscall.Errno(0x80010106)): // RPC_E_CHANGED_MODE: initialised another way, and usable
	default:
		return fmt.Errorf("shortcut %s: start COM: %w", path, cerr)
	}

	var link *comObject
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer, // #nosec G103 -- arguments of a Windows call
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link))) // #nosec G103 -- as above
	if err := hresult(hr); err != nil {
		return fmt.Errorf("shortcut %s: create the shell link: %w", path, err)
	}
	defer link.call(comRelease)

	args := make([]string, len(s.Args))
	for i, a := range s.Args {
		args[i] = windows.EscapeArg(a)
	}
	for _, set := range []struct {
		method int
		value  string
	}{
		{shellLinkSetPath, s.Target},
		{shellLinkSetArguments, strings.Join(args, " ")},
		{shellLinkSetWorkingDirectory, s.Dir},
		{shellLinkSetDescription, s.Description},
	} {
		if set.value == "" {
			continue
		}
		if err := link.callText(set.method, set.value); err != nil {
			return fmt.Errorf("shortcut %s: set %q: %w", path, set.value, err)
		}
	}
	if s.Icon != "" {
		if err := link.callText(shellLinkSetIconLocation, s.Icon, 0); err != nil {
			return fmt.Errorf("shortcut %s: set the icon: %w", path, err)
		}
	}

	var file *comObject
	if hr := link.call(comQueryInterface, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&file))); failed(hr) { // #nosec G103 -- arguments of a Windows call
		return fmt.Errorf("shortcut %s: %w", path, hresult(hr))
	}
	defer file.call(comRelease)

	tmp := filepath.Join(filepath.Dir(path), fmt.Sprintf(".fynstall-%d.lnk", os.Getpid()))
	if err := file.callText(persistFileSave, tmp, 1); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("shortcut %s: save: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("shortcut %s: %w", path, err)
	}
	return nil
}

var procCoCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance") //nolint:gochecknoglobals // a system procedure

const clsctxInprocServer = 1

// The object and interface IDs, from the Windows SDK's shobjidl_core.h and
// objidl.h.
var ( //nolint:gochecknoglobals // fixed IDs, passed by address
	clsidShellLink = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// Positions in the method tables: IUnknown's three, then each interface's
// own in the order the SDK declares them.
const (
	comQueryInterface = 0
	comRelease        = 2

	shellLinkSetDescription      = 7
	shellLinkSetWorkingDirectory = 9
	shellLinkSetArguments        = 11
	shellLinkSetIconLocation     = 17
	shellLinkSetPath             = 20

	persistFileSave = 6
)

// comObject is a COM interface pointer: its first word points at the
// method table.
type comObject struct {
	methods *[32]uintptr
}

// call calls the method at index with the object and args, and returns its
// HRESULT.
func (o *comObject) call(index int, args ...uintptr) uintptr {
	hr, _, _ := syscall.SyscallN(o.methods[index], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...) // #nosec G103 -- the object is its own first argument
	return hr
}

// callText calls the method at index with text as its first argument, then
// more, and makes a failing HRESULT an error.
func (o *comObject) callText(index int, text string, more ...uintptr) error {
	p, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return fmt.Errorf("%q: %w", text, err)
	}
	hr := o.call(index, append([]uintptr{uintptr(unsafe.Pointer(p))}, more...)...) // #nosec G103 -- a string for a Windows call
	runtime.KeepAlive(p)
	return hresult(hr)
}

// hresult is nil for a COM result that says success, and an error that
// shows the code for one that says failure.
func hresult(hr uintptr) error {
	if failed(hr) {
		return fmt.Errorf("HRESULT 0x%08x", uint32(hr)) // #nosec G115 -- an HRESULT is 32 bits
	}
	return nil
}

func failed(hr uintptr) bool { return int32(hr) < 0 } // #nosec G115 -- an HRESULT is 32 bits
