package platform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Services is systemd's: a service is a unit file (spec 002 D2a).
const Services = Systemd

// UnitPath is where the unit file of the service name goes. Per-user
// scope uses the systemd user manager, so nothing needs root (spec 002
// phase 4a); system scope uses the system manager.
func UnitPath(v map[string]string, name string) string {
	if System(v) {
		return filepath.Join(v["config"], "systemd", "system", name+".service")
	}
	return filepath.Join(v["config"], "systemd", "user", name+".service")
}

// HasServiceManager reports whether systemctl is on PATH.
func HasServiceManager() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// ServiceState reports whether the service name is enabled and whether it
// is running. A service that does not exist is neither.
func ServiceState(system bool, name string) (enabled, active bool) {
	out, _ := systemctl(system, "is-enabled", name+".service")
	enabled = strings.TrimSpace(out) == "enabled"
	out, _ = systemctl(system, "is-active", name+".service")
	active = strings.TrimSpace(out) == "active"
	return enabled, active
}

// ReloadServices makes the service manager read the unit files again.
func ReloadServices(system bool) error { return run(system, "daemon-reload") }

// EnableService enables the service name, so it starts at login.
func EnableService(system bool, name string) error { return run(system, "enable", name+".service") }

// RestartService starts the service name, or restarts it if it runs.
func RestartService(system bool, name string) error { return run(system, "restart", name+".service") }

// StartService starts the service name.
func StartService(system bool, name string) error { return run(system, "start", name+".service") }

// DisableService stops the service name and disables it.
func DisableService(system bool, name string) error {
	return run(system, "disable", "--now", name+".service")
}

func run(system bool, args ...string) error {
	if out, err := systemctl(system, args...); err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(managerArgs(system, args), " "), err, strings.TrimSpace(out))
	}
	return nil
}

// managerArgs adds --user for the user's manager.
func managerArgs(system bool, args []string) []string {
	if system {
		return args
	}
	return append([]string{"--user"}, args...)
}

func systemctl(system bool, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "systemctl", managerArgs(system, args)...) // #nosec G204 -- fixed subcommands and a unit name validation allows
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("run systemctl: %w", err)
	}
	return out.String(), nil
}
