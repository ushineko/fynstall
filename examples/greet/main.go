/*
Command greet is the program fynstall's multi-target tests and desk checks
install. It is pure Go with no cgo, so it builds for every target from one
machine, and it prints the target it was built for, so an installed copy
shows which payload it came from (spec 002 D1a).

It reads the configuration file its installer wrote from parameters
(spec 002 D3a), if there is one, and greets with what it says. It never
prints the token: that is a secret.
*/
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = ""
	}
	greet(os.Stdout, filepath.Join(dir, "io.ushineko.greet", "config.json"))
}

// config is what the installer writes. Token is read only to say whether
// there is one.
type config struct {
	Greeting string `json:"greeting"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

func greet(w io.Writer, path string) {
	c := config{Greeting: "greet"}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	line := fmt.Sprintf("%s from %s/%s", c.Greeting, runtime.GOOS, runtime.GOARCH)
	if c.Name != "" {
		line += ", " + c.Name
	}
	if c.Token != "" {
		line += " (with a token)"
	}
	_, _ = fmt.Fprintln(w, line)
}
