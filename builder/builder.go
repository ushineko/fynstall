/*
Package builder turns a fynstall.yaml into installers. For each target it
generates two small main packages (an uninstaller, then an installer that
embeds the payload, the manifest and that uninstaller) and runs go build on
them (spec 001, R2–R5, R9b).
*/
package builder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/ushineko/fynstall/config"
	"github.com/ushineko/fynstall/internal/version"
	"github.com/ushineko/fynstall/manifest"
)

// RuntimeModule is the module every generated program imports.
const RuntimeModule = "github.com/ushineko/fynstall"

// Options control a build.
type Options struct {
	// Config is the path of fynstall.yaml.
	Config string
	// OutDir receives the installers; default "dist" beside the config.
	OutDir string
	// Targets override the config's targets; both empty means this host.
	Targets []string
	// CLIOnly builds only the CLI variant: no Fyne, no cgo (R12). Without
	// it the build makes the full variant, with the wizard.
	CLIOnly bool
	// WithCLIOnly builds the CLI variant as well as the full one.
	WithCLIOnly bool
	// RuntimePath, when set, is a local fynstall checkout the generated
	// programs use through a replace directive, instead of a released
	// version (R5).
	RuntimePath string
	// RuntimeVersion is stamped into the generated programs; default the
	// builder's own version.
	RuntimeVersion string
	// Go is the go command; default "go".
	Go string
	// Env is added to the go command's environment.
	Env []string
	// Log receives the go command's output; nil discards it.
	Log io.Writer
}

// Artifact is what one target and variant produced.
type Artifact struct {
	Target string
	// GUI is true for the full variant, with the wizard.
	GUI         bool
	Installer   string
	Uninstaller string
}

// Build builds an installer and an uninstaller for each target.
func Build(ctx context.Context, o Options) ([]Artifact, error) {
	c, err := config.Load(o.Config)
	if err != nil {
		return nil, err
	}
	if o.RuntimeVersion == "" {
		o.RuntimeVersion = version.Version
	}
	if o.Go == "" {
		o.Go = "go"
	}
	if o.OutDir == "" {
		o.OutDir = filepath.Join(c.Dir, "dist")
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	targets := o.Targets
	if len(targets) == 0 {
		targets = c.Targets
	}
	if len(targets) == 0 {
		targets = []string{runtime.GOOS + "/" + runtime.GOARCH}
	}
	for _, t := range targets {
		if !slices.Contains(config.KnownTargets, t) {
			return nil, fmt.Errorf("unknown target %q", t)
		}
		if strings.HasPrefix(t, "windows/") && !o.CLIOnly {
			return nil, fmt.Errorf("target %s: the wizard on Windows arrives later in spec 001 phase 7; build it with --cli-only", t)
		}
	}
	var variants []bool
	if !o.CLIOnly {
		variants = append(variants, true)
	}
	if o.CLIOnly || o.WithCLIOnly {
		variants = append(variants, false)
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	if variants[0] {
		for _, t := range targets {
			if t != host {
				return nil, fmt.Errorf("target %s: the wizard needs cgo, so a full installer builds only for this machine (%s); build other targets with --cli-only", t, host)
			}
		}
	}

	mod, err := moduleFiles(o)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(o.OutDir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", o.OutDir, err)
	}

	var out []Artifact
	for _, t := range targets {
		// One manifest per target: the payload differs between targets
		// (spec 002 D1a).
		m, files, generated, err := stage(c, o.RuntimeVersion, t)
		if err != nil {
			return nil, err
		}
		goos, goarch, _ := strings.Cut(t, "/")
		base := fmt.Sprintf("%s-%s-%s-%s", m.App.Basename(), m.App.Version, goos, goarch)
		for _, gui := range variants {
			m.GUI = gui
			manifestJSON, err := m.Marshal()
			if err != nil {
				return nil, err
			}
			name := base
			if !gui {
				name += "-cli"
			}
			exe := manifest.BuildVars(t)["exe"]
			a := Artifact{
				Target: t, GUI: gui,
				Installer:   filepath.Join(o.OutDir, name+"-installer"+exe),
				Uninstaller: filepath.Join(o.OutDir, name+"-uninstaller"+exe),
			}
			if err := buildTarget(ctx, o, mod, goos, goarch, m.App, manifestJSON, files, generated, a); err != nil {
				return nil, fmt.Errorf("target %s: %w", t, err)
			}
			out = append(out, a)
		}
	}
	return out, nil
}

func buildTarget(ctx context.Context, o Options, mod map[string][]byte, goos, goarch string, app manifest.App, manifestJSON []byte, files []staged, generated map[string][]byte, a Artifact) error {
	work, err := os.MkdirTemp("", "fynstall-build-*")
	if err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	// On Windows the installer and the uninstaller carry the app's icon as
	// a resource, so Explorer shows it for the files themselves.
	res := map[string][]byte{}
	if ico, ok := generated[manifest.WindowsIcon]; ok && goos == "windows" {
		list, err := iconResources(ico)
		if err != nil {
			return err
		}
		obj, err := resourceObject(goarch, list)
		if err != nil {
			return err
		}
		res[sysoName(goarch)] = obj
	}

	un := filepath.Join(work, "uninstaller")
	unMain := fmt.Sprintf(uninstallerMain, app.ID, app.Name, app.Version)
	if err := writeTree(un, mod, res, map[string][]byte{"main.go": []byte(unMain)}); err != nil {
		return err
	}
	if err := goBuild(ctx, o, un, goos, goarch, a.GUI, a.Uninstaller); err != nil {
		return fmt.Errorf("build uninstaller: %w", err)
	}
	unBytes, err := os.ReadFile(a.Uninstaller)
	if err != nil {
		return fmt.Errorf("read uninstaller: %w", err)
	}

	in := filepath.Join(work, "installer")
	gen := map[string][]byte{
		"main.go":         []byte(installerMain),
		"manifest.json":   manifestJSON,
		"uninstaller.bin": unBytes,
	}
	if err := writeTree(in, mod, res, gen); err != nil {
		return err
	}
	for _, f := range files {
		dst := filepath.Join(in, "payload", filepath.FromSlash(f.Path))
		if err := copyPayload(f.src, dst); err != nil {
			return err
		}
	}
	for p, b := range generated {
		dst := filepath.Join(in, "payload", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return fmt.Errorf("stage %s: %w", p, err)
		}
		if err := os.WriteFile(dst, b, 0o600); err != nil { // #nosec G703 -- p is a manifest.Icon path, fixed by the builder
			return fmt.Errorf("stage %s: %w", p, err)
		}
	}
	if err := goBuild(ctx, o, in, goos, goarch, a.GUI, a.Installer); err != nil {
		return fmt.Errorf("build installer: %w", err)
	}
	return nil
}

// moduleFiles are the go.mod (and go.sum) every generated program gets.
func moduleFiles(o Options) (map[string][]byte, error) {
	var gomod bytes.Buffer
	fmt.Fprintf(&gomod, "module fynstall.local/generated\n\ngo 1.26.0\n\n")
	files := map[string][]byte{}
	if o.RuntimePath != "" {
		abs, err := filepath.Abs(o.RuntimePath)
		if err != nil {
			return nil, fmt.Errorf("runtime path: %w", err)
		}
		// Slashes on every OS: go.mod reads a backslash path too, but not
		// one with a space in it unless it is quoted, and %q would double
		// the backslashes.
		fmt.Fprintf(&gomod, "require %s v0.0.0\n\nreplace %s => %q\n", RuntimeModule, RuntimeModule, filepath.ToSlash(abs))
		// The checkout's go.sum covers every module its go.mod names, so
		// the build needs no network.
		sum, err := os.ReadFile(filepath.Join(abs, "go.sum"))
		if err != nil {
			return nil, fmt.Errorf("runtime path: %w", err)
		}
		files["go.sum"] = sum
	} else {
		if strings.HasSuffix(o.RuntimeVersion, "-dev") || o.RuntimeVersion == "dev" {
			return nil, fmt.Errorf("this fynstall (%s) is not a release, so installers cannot name it as a version; pass --runtime-path with a fynstall checkout", o.RuntimeVersion)
		}
		fmt.Fprintf(&gomod, "require %s v%s\n", RuntimeModule, o.RuntimeVersion)
	}
	files["go.mod"] = gomod.Bytes()
	return files, nil
}

// goBuild builds the program in dir. The full variant is built with cgo
// and Fyne; the CLI variant with neither, under the nogui tag (R12).
func goBuild(ctx context.Context, o Options, dir, goos, goarch string, gui bool, out string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	if o.RuntimePath == "" {
		if err := goCmd(ctx, o, dir, goos, goarch, gui, "mod", "tidy"); err != nil {
			return err
		}
	}
	ldflags := fmt.Sprintf("-s -w -buildid= -X %s/internal/version.Version=%s", RuntimeModule, o.RuntimeVersion)
	args := []string{"build", "-trimpath", "-buildvcs=false"}
	if !gui {
		args = append(args, "-tags", "nogui")
	}
	return goCmd(ctx, o, dir, goos, goarch, gui, append(args, "-ldflags", ldflags, "-o", abs, ".")...)
}

func goCmd(ctx context.Context, o Options, dir, goos, goarch string, gui bool, args ...string) error {
	cmd := exec.CommandContext(ctx, o.Go, args...) // #nosec G204 -- the go command and fixed arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), o.Env...)
	cgo := "CGO_ENABLED=0"
	if gui {
		cgo = "CGO_ENABLED=1"
	}
	cmd.Env = append(cmd.Env, cgo, "GOOS="+goos, "GOARCH="+goarch, "GOWORK=off", "GOFLAGS=-mod=mod")
	var stderr bytes.Buffer
	cmd.Stdout = o.Log
	cmd.Stderr = io.MultiWriter(o.Log, &stderr)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w\n%s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func writeTree(dir string, sets ...map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	for _, set := range sets {
		for name, b := range set {
			if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
		}
	}
	return nil
}

func copyPayload(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("stage %s: %w", src, err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("stage %s: %w", src, err)
	}
	// dst is inside the work directory: its relative part is a manifest
	// path that config.CheckDst has already held inside the install root.
	if err := os.WriteFile(dst, b, 0o600); err != nil { // #nosec G703
		return fmt.Errorf("stage %s: %w", src, err)
	}
	return nil
}

// uninstallerMain is the uninstaller's main package. It carries the app's
// identity, so a copy that is not inside an install (the one beside the
// installer in dist/) can find the install and hand over to its own
// uninstaller. The values are Go-quoted with %q.
const uninstallerMain = `// Code generated by fynstall. DO NOT EDIT.

package main

import (
	"github.com/ushineko/fynstall/installer"
	"github.com/ushineko/fynstall/manifest"
)

func main() {
	installer.UninstallMain(manifest.App{ID: %q, Name: %q, Version: %q})
}
`

const installerMain = `// Code generated by fynstall. DO NOT EDIT.

package main

import (
	"embed"
	"io/fs"

	"github.com/ushineko/fynstall/installer"
)

//go:embed manifest.json
var manifestJSON []byte

//go:embed uninstaller.bin
var uninstaller []byte

//go:embed all:payload
var payload embed.FS

func main() {
	files, err := fs.Sub(payload, "payload")
	if err != nil {
		panic(err)
	}
	installer.Main(installer.Payload{Manifest: manifestJSON, Files: files, Uninstaller: uninstaller})
}
`
