/*
Command fynstall builds self-contained installers for Go and Fyne programs
from a fynstall.yaml. See specs/001-installer-prototype.md.
*/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/ushineko/fynstall/builder"
	"github.com/ushineko/fynstall/config"
	"github.com/ushineko/fynstall/internal/version"
)

const usage = `Usage: fynstall <command> [flags]

Commands:
  init       write a commented fynstall.yaml in this directory
  validate   check a fynstall.yaml and list every problem
  build      build installers from a fynstall.yaml
  version    print the fynstall version

Run "fynstall <command> -h" for a command's flags.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is main without the process: it returns the exit code, so a test can
// call it. 2 is a usage error, as with the flag package.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	case "init":
		return initCmd(args[1:], stdout, stderr)
	case "validate":
		return validateCmd(args[1:], stdout, stderr)
	case "build":
		return buildCmd(ctx, args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "fynstall: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func flags(name string, stderr io.Writer) *flag.FlagSet {
	fl := flag.NewFlagSet("fynstall "+name, flag.ContinueOnError)
	fl.SetOutput(stderr)
	return fl
}

func initCmd(args []string, stdout, stderr io.Writer) int {
	fl := flags("init", stderr)
	path := fl.String("c", "fynstall.yaml", "file to write")
	if fl.Parse(args) != nil {
		return 2
	}
	// A config is committed and read by others, like any source file.
	f, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G302
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fynstall init: %v\n", err)
		return 1
	}
	_, err = f.WriteString(initTemplate)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fynstall init: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Wrote %s. Edit it, then run fynstall validate.\n", *path)
	return 0
}

func validateCmd(args []string, stdout, stderr io.Writer) int {
	fl := flags("validate", stderr)
	path := fl.String("c", "fynstall.yaml", "config to check")
	if fl.Parse(args) != nil {
		return 2
	}
	if _, err := config.Load(*path); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s is valid.\n", *path)
	return 0
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func buildCmd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var o builder.Options
	var targets listFlag
	var verbose bool
	fl := flags("build", stderr)
	fl.StringVar(&o.Config, "c", "fynstall.yaml", "config to build")
	fl.StringVar(&o.OutDir, "o", "", "output directory (default: dist beside the config)")
	fl.Var(&targets, "target", "os/arch to build for; repeat for more (default: the config's targets, else this machine)")
	fl.BoolVar(&o.CLIOnly, "cli-only", false, "build only the CLI variant: no wizard, no Fyne, no cgo, no graphics libraries")
	fl.BoolVar(&o.WithCLIOnly, "with-cli-only", false, "build the CLI variant as well as the full one")
	fl.StringVar(&o.RuntimePath, "runtime-path", "", "a fynstall checkout to build the installers against, instead of this fynstall's release")
	fl.BoolVar(&verbose, "v", false, "show the go command's output")
	if fl.Parse(args) != nil {
		return 2
	}
	o.Targets = targets
	if verbose {
		o.Log = stderr
	}
	arts, err := builder.Build(ctx, o)
	if err != nil {
		var errs config.Errors
		if errors.As(err, &errs) {
			_, _ = fmt.Fprintln(stderr, errs)
		} else {
			_, _ = fmt.Fprintf(stderr, "fynstall build: %v\n", err)
		}
		return 1
	}
	for _, a := range arts {
		_, _ = fmt.Fprintf(stdout, "%s\n  %s\n  %s\n", a.Target, a.Installer, a.Uninstaller)
	}
	return 0
}

const initTemplate = `# fynstall.yaml: how to build an installer for this program.
# Paths under payload are relative to this file.

app:
  id: io.example.myapp        # reverse-DNS; names the desktop entry on Linux
  name: My App
  version: 0.1.0
  publisher: Example

install:
  scopes: [user]              # user installs need no elevation
  dir:
    user: "{programs}/{id}"   # ~/.local/share/io.example.myapp on Linux,
                              # %LOCALAPPDATA%\Programs\io.example.myapp on Windows

# Every file the installer writes, relative to the install directory.
# A directory src is copied recursively; mode is detected unless given.
payload:
  - src: bin/myapp
    dst: bin/myapp
  # - src: share/
  #   dst: share/
  #   exclude: ["*.tmp"]

integration:
  # Paths the uninstaller never touches. It prints them, so the user knows
  # where their data is.
  keep_on_uninstall:
    - "{config}/myapp"

# Placeholders: {id} {name} {version} {home} {data} {config} {bin} {programs}
# targets: [linux/amd64]      # default: the machine that builds
`
