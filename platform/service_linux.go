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

// UnitPath is where the unit file of the service name goes. Per-user
// scope uses the systemd user manager, so nothing needs root (spec 002
// phase 4a).
func UnitPath(v map[string]string, name string) string {
	return filepath.Join(v["config"], "systemd", "user", name+".service")
}

// HasServiceManager reports whether systemctl is on PATH.
func HasServiceManager() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// ServiceState reports whether the service name is enabled and whether it
// is running. A service that does not exist is neither.
func ServiceState(name string) (enabled, active bool) {
	out, _ := systemctl("is-enabled", name+".service")
	enabled = strings.TrimSpace(out) == "enabled"
	out, _ = systemctl("is-active", name+".service")
	active = strings.TrimSpace(out) == "active"
	return enabled, active
}

// ReloadServices makes the service manager read the unit files again.
func ReloadServices() error { return run("daemon-reload") }

// EnableService enables the service name, so it starts at login.
func EnableService(name string) error { return run("enable", name+".service") }

// RestartService starts the service name, or restarts it if it runs.
func RestartService(name string) error { return run("restart", name+".service") }

// StartService starts the service name.
func StartService(name string) error { return run("start", name+".service") }

// DisableService stops the service name and disables it.
func DisableService(name string) error { return run("disable", "--now", name+".service") }

func run(args ...string) error {
	if out, err := systemctl(args...); err != nil {
		return fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

func systemctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...) // #nosec G204 -- fixed subcommands and a unit name validation allows
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("run systemctl: %w", err)
	}
	return out.String(), nil
}
