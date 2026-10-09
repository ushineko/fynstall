package platform

import (
	"fmt"
	"strings"
)

// Unit is what a service action runs: an absolute program and its
// arguments, and what to do when it exits.
type Unit struct {
	AppID       string
	Description string
	Exec        string
	Args        []string
	// Restart is no, on-failure or always.
	Restart string
}

// RenderUnit is u as a systemd unit file. Every argument is quoted, and
// the characters systemd expands (% and $) are escaped, so a path with
// spaces or a value with a dollar sign reaches the program as written.
func RenderUnit(u Unit) []byte {
	words := []string{quoteUnitWord(u.Exec)}
	for _, a := range u.Args {
		words = append(words, quoteUnitWord(a))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Written by the installer of %s. Its uninstaller removes it.\n", u.AppID)
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\n\n", u.Description)
	fmt.Fprintf(&b, "[Service]\nExecStart=%s\nRestart=%s\n\n", strings.Join(words, " "), u.Restart)
	b.WriteString("[Install]\nWantedBy=default.target\n")
	return []byte(b.String())
}

func quoteUnitWord(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$", "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}
