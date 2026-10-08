/*
Command fynstall builds self-contained installers for Go and Fyne programs
from a fynstall.yaml. See specs/001-installer-prototype.md.

Only `version` exists so far; init, validate and build arrive in phase 1.
*/
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ushineko/fynstall/internal/version"
)

const usage = `Usage: fynstall <command>

Commands:
  version   print the fynstall version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code, so a test can
// call it. 2 is a usage error, as with the flag package.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		_, _ = fmt.Fprintln(stdout, "fynstall", version.Version)
		return 0
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "fynstall: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
