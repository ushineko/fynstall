package main

import (
	"fmt"
	"io"
	"time"

	"golang.org/x/sys/windows/svc"
)

// serveAs runs serve. Started by the Windows service manager, the program
// must answer it: say that it runs, and stop when asked. A program that
// does not is stopped by Windows after 30 seconds, so a service action
// needs a program written for it. Started any other way, it is an ordinary
// program.
func serveAs(out io.Writer, every time.Duration) error {
	if ok, err := svc.IsWindowsService(); err != nil || !ok {
		return serve(out, every, nil)
	}
	// The name is ignored for a service with a program of its own.
	if err := svc.Run("", service{every: every}); err != nil {
		return fmt.Errorf("run as a service: %w", err)
	}
	return nil
}

type service struct{ every time.Duration }

// Execute is the conversation with the service manager.
func (s service) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	stop := make(chan struct{})
	done := make(chan error, 1)
	// A service has no console to print on.
	go func() { done <- serve(io.Discard, s.every, stop) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				return false, 1
			}
			return false, 0
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				close(stop)
				<-done
				return false, 0
			}
		}
	}
}
