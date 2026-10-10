package platform

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RefreshMenu tells running programs that the environment changed, so a
// console opened from Explorer after an install or uninstall has the new
// PATH without a logout. The Start Menu watches its own directories and
// needs nothing. The tests, which write under a registry root of their own,
// have nothing to announce.
func RefreshMenu() error {
	if os.Getenv("FYNSTALL_TEST_REGISTRY_ROOT") != "" {
		return nil
	}
	const (
		hwndBroadcast    = 0xffff
		wmSettingChange  = 0x001A
		smtoAbortIfHung  = 0x0002
		timeoutMillisecs = 5000
	)
	what, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return fmt.Errorf("announce the new PATH: %w", err)
	}
	// A window that does not answer in time is its own problem: the
	// message was sent, and the registry holds the value either way.
	_, _, _ = procSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(what)), // #nosec G103 -- a string for a Windows call
		smtoAbortIfHung, timeoutMillisecs, 0)
	return nil
}

var procSendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW") //nolint:gochecknoglobals // a system procedure
