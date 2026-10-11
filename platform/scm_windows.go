package platform

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Services is the Windows service manager's (spec 002 D2a).
const Services = SCM

// How long a service gets to say it runs, or that it stopped, and how long
// its process then gets to end.
const (
	serviceWait = 30 * time.Second
	processWait = 10 * time.Second
)

// realService is the name of the service whose key is key, when key is in
// the machine's own registry. A key that the tests moved under their root
// (see RegistryRoot) is not: it is a stand-in, and nothing here touches the
// service manager for it (scmStandIn).
func realService(key string) (string, bool) {
	name, ok := strings.CutPrefix(key, servicesKey+`\`)
	return name, ok && name != "" && !strings.Contains(name, `\`)
}

// SCMExists reports whether Windows has the service whose key is key. It
// asks with the rights of any user, because the plan is made before an
// administrator is asked for.
func SCMExists(key string) (bool, error) {
	name, own := realService(key)
	if !own {
		return standInExists(key)
	}
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, fmt.Errorf("ask the service manager: %w", err)
	}
	defer func() { _ = windows.CloseServiceHandle(m) }()
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, fmt.Errorf("service %s: %w", name, err)
	}
	s, err := windows.OpenService(m, p, windows.SERVICE_QUERY_STATUS)
	switch {
	case err == nil:
		_ = windows.CloseServiceHandle(s)
		return true, nil
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		return false, nil
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return true, nil // a service this user may not even ask about
	}
	return false, fmt.Errorf("ask the service manager about %s: %w", name, err)
}

// SCMCreate registers the service d under key. It runs as LocalSystem and
// starts with the computer. When the config asks for a restart, Windows
// starts it again after a failure: a crash, or a stop with an exit code
// other than 0. A service that is half made is removed again, so the caller
// records it only once it is whole.
func SCMCreate(key string, d ServiceDef) (err error) {
	name, own := realService(key)
	if !own {
		return standInCreate(key, d)
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service %s: connect to the service manager: %w", name, err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.CreateService(name, d.Exec, mgr.Config{
		StartType: mgr.StartAutomatic, ErrorControl: mgr.ErrorNormal,
		DisplayName: d.DisplayName, Description: d.Description,
	}, d.Args...)
	if errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("service %s: Windows is still removing a service of that name; close the Services window and any program that shows services, then run the installer again: %w", name, err)
	}
	if err != nil {
		return fmt.Errorf("create service %s: %w", name, err)
	}
	defer func() {
		if err != nil {
			_ = s.Delete()
		}
		_ = s.Close()
	}()
	if d.Restart == "no" {
		return nil
	}
	// Three restarts five seconds apart, counted over a day, then Windows
	// leaves the service stopped.
	restart := mgr.RecoveryAction{Type: mgr.ServiceRestart, Delay: 5 * time.Second}
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{restart, restart, restart}, 24*60*60); err != nil {
		return fmt.Errorf("service %s: set what Windows does when it fails: %w", name, err)
	}
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("service %s: set what Windows does when it fails: %w", name, err)
	}
	return nil
}

// SCMStart starts the service whose key is key, and waits until it says it
// runs. A service that stops again at once is an error.
func SCMStart(key string) error {
	name, own := realService(key)
	if !own {
		return standInSet(key, standInRunning)
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service %s: connect to the service manager: %w", name, err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("start service %s: %w", name, err)
	}
	st, err := waitService(s, svc.Running, svc.Stopped)
	switch {
	case err != nil:
		return fmt.Errorf("start service %s: %w", name, err)
	case st.State == svc.Stopped:
		return fmt.Errorf("start service %s: it stopped as soon as it started (exit code %d)", name, st.Win32ExitCode)
	}
	return nil
}

// SCMRemove stops the service whose key is key, waits for its process to
// end so that its files can be removed, and removes the service. A service
// that is already gone is not an error.
func SCMRemove(key string) error {
	name, own := realService(key)
	if !own {
		return standInRemove(key)
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service %s: connect to the service manager: %w", name, err)
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	defer func() { _ = s.Close() }()

	stopErr := stopService(s)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop service %s: %w", name, stopErr)
	}
	// Removed even when it did not stop: Windows then removes it once it
	// does, and it does not start with the computer again.
	if err := s.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return errors.Join(stopErr, fmt.Errorf("remove service %s: %w", name, err))
	}
	return stopErr
}

// stopService stops s and waits for the process that ran it to end.
func stopService(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return fmt.Errorf("ask its state: %w", err)
	}
	if st.State == svc.Stopped {
		return nil
	}
	var proc windows.Handle
	if st.ProcessId != 0 {
		// Without the handle the wait is skipped; the stop is not.
		proc, _ = windows.OpenProcess(windows.SYNCHRONIZE, false, st.ProcessId)
	}
	if proc != 0 {
		defer func() { _ = windows.CloseHandle(proc) }()
	}
	if st.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("%w", err)
		}
	}
	if _, err := waitService(s, svc.Stopped); err != nil {
		return err
	}
	if proc != 0 {
		// The service says it stopped before its process is gone, and
		// Windows does not delete the file of a running program.
		if ev, _ := windows.WaitForSingleObject(proc, uint32(processWait.Milliseconds())); ev != windows.WAIT_OBJECT_0 { // #nosec G115 -- ten seconds
			return errors.New("its process is still running")
		}
	}
	return nil
}

// waitService waits until s is in one of states, and returns its status.
func waitService(s *mgr.Service, states ...svc.State) (svc.Status, error) {
	for deadline := time.Now().Add(serviceWait); ; {
		st, err := s.Query()
		if err != nil {
			return st, fmt.Errorf("ask its state: %w", err)
		}
		for _, want := range states {
			if st.State == want {
				return st, nil
			}
		}
		if time.Now().After(deadline) {
			return st, fmt.Errorf("it did not answer the service manager within %s", serviceWait)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

/*
The stand-in for the service manager, for the tests. A system install that
they run has its registry keys under a root of the test's own, and the key
of a service is one of them. For such a key nothing is registered and no
program is started: the "service" is the values Windows itself keeps for
one, written under the test's root, with what the config asked for and
whether it "runs". The tests of the real service manager are apart
(scm_windows_test.go) and need an administrator.
*/

// The stand-in's own values: the restart policy as the config wrote it,
// and whether the service was started.
const (
	StandInRestart = "FynstallTestRestart"
	StandInState   = "FynstallTestState"
	standInRunning = "running"
	standInStopped = "stopped"
)

// standInKey refuses a key that is not a service's key under a test's root.
func standInKey(key string) error {
	if !strings.HasPrefix(key, `HKCU\`) || !strings.Contains(key, `\`+servicesKey+`\`) {
		return fmt.Errorf("%s is not the key of a service", key)
	}
	return nil
}

func standInExists(key string) (bool, error) {
	if err := standInKey(key); err != nil {
		return false, err
	}
	_, had, err := RegGet(key, "ImagePath")
	return had, err
}

func standInCreate(key string, d ServiceDef) error {
	exists, err := standInExists(key)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("create service %s: %w", d.Name, windows.ERROR_SERVICE_EXISTS)
	}
	if _, err := RegCreateKey(key); err != nil {
		return err
	}
	for _, v := range []RegValue{
		{"ImagePath", RegExpandString, windows.ComposeCommandLine(append([]string{d.Exec}, d.Args...))},
		{"DisplayName", RegString, d.DisplayName},
		{"Description", RegString, d.Description},
		{"ObjectName", RegString, "LocalSystem"},
		{"Type", RegNumber, "16"}, // a program of its own
		{"Start", RegNumber, "2"}, // with the computer
		{"ErrorControl", RegNumber, "1"},
		{StandInRestart, RegString, d.Restart},
		{StandInState, RegString, standInStopped},
	} {
		if err := RegSet(key, v); err != nil {
			return err
		}
	}
	return nil
}

func standInSet(key, state string) error {
	exists, err := standInExists(key)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("start the service at %s: %w", key, windows.ERROR_SERVICE_DOES_NOT_EXIST)
	}
	return RegSet(key, RegValue{Name: StandInState, Kind: RegString, Data: state})
}

func standInRemove(key string) error {
	if err := standInKey(key); err != nil {
		return err
	}
	for _, name := range []string{"ImagePath", "DisplayName", "Description", "ObjectName", "Type", "Start", "ErrorControl", StandInRestart, StandInState} {
		if err := RegDelete(key, name); err != nil {
			return err
		}
	}
	// The key, and the keys above it that the stand-in had to make: in the
	// real registry they are Windows' own and always there.
	for range strings.Count(servicesKey, `\`) + 2 {
		gone, err := RegDeleteKeyIfEmpty(key)
		if err != nil || !gone {
			return err
		}
		key = key[:strings.LastIndex(key, `\`)]
	}
	return nil
}
