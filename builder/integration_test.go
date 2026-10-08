package builder_test

/*
These tests build the examples/hello installer with the real builder and go
build, then run it and its uninstaller as real processes against a
temporary HOME. Nothing is mocked (spec 001, Test Strategy).

The builds happen once, in TestMain, and are shared: v1, a second build of
v1 for the reproducibility check, and v2, which differs only in its stamped
runtime version, for the drift check (R9e). Builds run with GOPROXY=off, so
the tests need the module cache and not the network.
*/

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/builder"
	"github.com/ushineko/fynstall/internal/snapshot"
)

const (
	v1 = "1.0.0-test"
	v2 = "2.0.0-test"
)

var (
	src       string // the staged copy of examples/hello: config, README, bin/hello
	v1Art     builder.Artifact
	v1Again   builder.Artifact
	v2Art     builder.Artifact
	setupFail error
)

func TestMain(m *testing.M) {
	work, err := os.MkdirTemp("", "fynstall-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	setupFail = setup(work)
	code := m.Run()
	_ = os.RemoveAll(work)
	os.Exit(code)
}

func setup(work string) error {
	repo, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	src = filepath.Join(work, "hello")
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o750); err != nil {
		return err
	}
	for _, f := range []string{"fynstall.yaml", "README.md"} {
		b, err := os.ReadFile(filepath.Join(repo, "examples", "hello", f))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(src, f), b, 0o600); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-o", filepath.Join(src, "bin", "hello"), "./examples/hello")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build hello: %w\n%s", err, out)
	}
	build := func(rv, out string) (builder.Artifact, error) {
		arts, err := builder.Build(context.Background(), builder.Options{
			Config: filepath.Join(src, "fynstall.yaml"), OutDir: filepath.Join(work, out),
			CLIOnly: true, RuntimePath: repo, RuntimeVersion: rv, Env: []string{"GOPROXY=off"},
		})
		if err != nil {
			return builder.Artifact{}, err
		}
		return arts[0], nil
	}
	if v1Art, err = build(v1, "v1"); err != nil {
		return err
	}
	if v1Again, err = build(v1, "v1-again"); err != nil {
		return err
	}
	v2Art, err = build(v2, "v2")
	return err
}

// home is a temporary HOME and the install directory the default config
// gives inside it.
type home struct {
	dir  string
	root string
}

func newHome(t *testing.T) home {
	t.Helper()
	require.NoError(t, setupFail)
	d := t.TempDir()
	return home{dir: d, root: filepath.Join(d, ".local", "share", "io.ushineko.hello")}
}

// run starts a program with only HOME and PATH set and no terminal, and
// returns its exit code and combined output.
func (h home) run(t *testing.T, prog string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), prog, args...)
	cmd.Env = []string{"HOME=" + h.dir, "PATH=" + os.Getenv("PATH")}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String()
	}
	require.NoError(t, err)
	return 0, out.String()
}

func (h home) snap(t *testing.T) map[string]string {
	t.Helper()
	s, err := snapshot.Take(h.dir)
	require.NoError(t, err)
	return s
}

func sum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallWritesThePayloadAndTheUninstallerRemovesIt(t *testing.T) {
	h := newHome(t)
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--cli", "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, sum(t, filepath.Join(src, "bin", "hello")), sum(t, filepath.Join(h.root, "bin", "hello")))
	require.Equal(t, sum(t, filepath.Join(src, "README.md")), sum(t, filepath.Join(h.root, "share", "doc", "README.md")))
	fi, err := os.Stat(filepath.Join(h.root, "bin", "hello"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "an ELF file is installed executable")
	require.Equal(t, sum(t, v1Art.Uninstaller), sum(t, filepath.Join(h.root, "uninstall")),
		"the installed uninstaller is the separate artifact in dist (R9b, R9c)")

	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, filepath.Join(h.dir, ".config", "io.ushineko.hello"), "it says where the kept data is")
	require.Equal(t, before, h.snap(t))
}

func TestUninstallRestoresAFileTheInstallReplaced(t *testing.T) {
	h := newHome(t)
	readme := filepath.Join(h.root, "share", "doc", "README.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(readme), 0o750))
	require.NoError(t, os.WriteFile(readme, []byte("my own notes\n"), 0o600))
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "replaces "+readme)
	require.Equal(t, sum(t, filepath.Join(src, "README.md")), sum(t, readme))

	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

func TestANewerInstallerRemovesThroughTheInstalledUninstaller(t *testing.T) {
	h := newHome(t)
	before := h.snap(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)

	code, out = h.run(t, v2Art.Installer, "--yes")
	require.Equal(t, 1, code, "a second install is refused, not layered over the first")
	require.Contains(t, out, "--uninstall")

	code, out = h.run(t, v2Art.Installer, "--uninstall", "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "fynstall uninstaller "+v1, "the v1 uninstaller ran, not v2's engine (R9e)")
	require.Equal(t, before, h.snap(t))
}

func TestBuildsAreReproducible(t *testing.T) {
	require.NoError(t, setupFail)
	require.Equal(t, sum(t, v1Art.Installer), sum(t, v1Again.Installer))
	require.Equal(t, sum(t, v1Art.Uninstaller), sum(t, v1Again.Uninstaller))
}

func TestCLIOnlyProgramsLinkNoLibraries(t *testing.T) {
	require.NoError(t, setupFail)
	for _, p := range []string{v1Art.Installer, v1Art.Uninstaller} {
		f, err := elf.Open(p)
		require.NoError(t, err)
		libs, err := f.ImportedLibraries()
		require.NoError(t, err)
		require.Empty(t, libs, p)
		for _, prog := range f.Progs {
			require.NotEqual(t, elf.PT_INTERP, prog.Type, "%s has a dynamic loader", p)
		}
		require.NoError(t, f.Close())
	}
}

func TestTheInstallerRefusesWhatItCannotDo(t *testing.T) {
	h := newHome(t)
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--gui")
	require.Equal(t, 2, code)
	require.Contains(t, out, "built without the wizard")

	code, out = h.run(t, v1Art.Installer)
	require.Equal(t, 2, code, "no terminal and no --yes")
	require.Contains(t, out, "--yes")

	code, out = h.run(t, v1Art.Installer, "--uninstall", "--yes")
	require.Equal(t, 1, code)
	require.Contains(t, out, "not installed")

	code, out = h.run(t, v1Art.Installer, "--dry-run")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "create   "+filepath.Join(h.root, "bin", "hello"))
	require.Equal(t, before, h.snap(t), "none of these changed anything")
}
