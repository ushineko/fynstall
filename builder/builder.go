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
	"errors"
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
	// CLIOnly builds without Fyne or cgo (R12). Phase 1 builds nothing else.
	CLIOnly bool
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

// Artifact is what one target produced.
type Artifact struct {
	Target      string
	Installer   string
	Uninstaller string
}

// Build builds an installer and an uninstaller for each target.
func Build(ctx context.Context, o Options) ([]Artifact, error) {
	if !o.CLIOnly {
		return nil, errors.New("only --cli-only installers can be built so far; the wizard arrives in spec 001 phase 4")
	}
	c, err := config.Load(o.Config)
	if err != nil {
		return nil, err
	}
	if slices.Contains(c.Install.Scopes, "system") {
		return nil, errors.New("install.scopes: system scope arrives in spec 001 phase 5")
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
		if strings.HasPrefix(t, "windows/") {
			return nil, fmt.Errorf("target %s: Windows arrives in spec 001 phase 7", t)
		}
	}
	mod, err := moduleFiles(o)
	if err != nil {
		return nil, err
	}

	m, files, generated, err := stage(c, o.RuntimeVersion)
	if err != nil {
		return nil, err
	}
	manifestJSON, err := m.Marshal()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(o.OutDir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", o.OutDir, err)
	}

	var out []Artifact
	for _, t := range targets {
		goos, goarch, _ := strings.Cut(t, "/")
		base := fmt.Sprintf("%s-%s-%s-%s", m.App.Basename(), m.App.Version, goos, goarch)
		a := Artifact{
			Target:      t,
			Installer:   filepath.Join(o.OutDir, base+"-installer"),
			Uninstaller: filepath.Join(o.OutDir, base+"-uninstaller"),
		}
		if err := buildTarget(ctx, o, mod, goos, goarch, manifestJSON, files, generated, a); err != nil {
			return nil, fmt.Errorf("target %s: %w", t, err)
		}
		out = append(out, a)
	}
	return out, nil
}

func buildTarget(ctx context.Context, o Options, mod map[string][]byte, goos, goarch string, manifestJSON []byte, files []staged, generated map[string][]byte, a Artifact) error {
	work, err := os.MkdirTemp("", "fynstall-build-*")
	if err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	un := filepath.Join(work, "uninstaller")
	if err := writeTree(un, mod, map[string][]byte{"main.go": []byte(uninstallerMain)}); err != nil {
		return err
	}
	if err := goBuild(ctx, o, un, goos, goarch, a.Uninstaller); err != nil {
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
	if err := writeTree(in, mod, gen); err != nil {
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
	if err := goBuild(ctx, o, in, goos, goarch, a.Installer); err != nil {
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
		fmt.Fprintf(&gomod, "require %s v0.0.0\n\nreplace %s => %s\n", RuntimeModule, RuntimeModule, abs)
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

func goBuild(ctx context.Context, o Options, dir, goos, goarch, out string) error {
	abs, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	if o.RuntimePath == "" {
		if err := goCmd(ctx, o, dir, goos, goarch, "mod", "tidy"); err != nil {
			return err
		}
	}
	ldflags := fmt.Sprintf("-s -w -buildid= -X %s/internal/version.Version=%s", RuntimeModule, o.RuntimeVersion)
	return goCmd(ctx, o, dir, goos, goarch, "build", "-trimpath", "-buildvcs=false", "-tags", "nogui", "-ldflags", ldflags, "-o", abs, ".")
}

func goCmd(ctx context.Context, o Options, dir, goos, goarch string, args ...string) error {
	cmd := exec.CommandContext(ctx, o.Go, args...) // #nosec G204 -- the go command and fixed arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch, "GOWORK=off", "GOFLAGS=-mod=mod")
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

const uninstallerMain = `// Code generated by fynstall. DO NOT EDIT.

package main

import "github.com/ushineko/fynstall/installer"

func main() { installer.UninstallMain() }
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
