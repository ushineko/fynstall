/*
Command greet is the program fynstall's multi-target tests and desk checks
install. It is pure Go with no cgo, so it builds for every target from one
machine, and it prints the target it was built for, so an installed copy
shows which payload it came from (spec 002 D1a).
*/
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

func main() { greet(os.Stdout) }

func greet(w io.Writer) {
	_, _ = fmt.Fprintf(w, "greet from %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
