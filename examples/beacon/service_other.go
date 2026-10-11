//go:build !windows

package main

import (
	"io"
	"time"
)

// serveAs runs serve: a systemd unit is an ordinary program.
func serveAs(out io.Writer, every time.Duration) error { return serve(out, every, nil) }
