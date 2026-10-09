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
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	greetAMD64 builder.Artifact
	greetARM64 builder.Artifact
	src        string // the staged copy of examples/hello: config, README, bin/hello
	v1Art      builder.Artifact
	v1Again    builder.Artifact
	v2Art      builder.Artifact
	setupFail  error
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
	for _, f := range []string{"fynstall.yaml", "README.md", "hello.png"} {
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
	if v2Art, err = build(v2, "v2"); err != nil {
		return err
	}
	return setupGreet(repo, work)
}

// setupGreet builds examples/greet for linux/amd64 and linux/arm64, pure Go
// with no cgo, and one installer per target from the same config.
func setupGreet(repo, work string) error {
	dir := filepath.Join(work, "greet")
	for _, f := range []string{"fynstall.yaml", "notes/arm64.txt"} {
		b, err := os.ReadFile(filepath.Join(repo, "examples", "greet", filepath.FromSlash(f)))
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			return err
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath",
			"-o", filepath.Join(dir, "build", "linux-"+arch, "greet"), "./examples/greet")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build greet for %s: %w\n%s", arch, err, out)
		}
	}
	arts, err := builder.Build(context.Background(), builder.Options{
		Config: filepath.Join(dir, "fynstall.yaml"), OutDir: filepath.Join(work, "greet-dist"),
		CLIOnly: true, RuntimePath: repo, RuntimeVersion: v1, Env: []string{"GOPROXY=off"},
	})
	if err != nil {
		return err
	}
	if len(arts) != 2 {
		return fmt.Errorf("greet: %d artifacts, want 2", len(arts))
	}
	greetAMD64, greetARM64 = arts[0], arts[1]
	return nil
}

// home is a temporary HOME, the install directory the default config gives
// inside it, and the PATH programs run with. PATH is an empty directory, so
// the desktop's real tools (kbuildsycoca6) never run against the test's
// home; TestTheMenuIsRefreshedAfterInstallAndUninstall puts a fake one there.
type home struct {
	dir  string
	root string
	path string
}

func newHome(t *testing.T) home {
	t.Helper()
	require.NoError(t, setupFail)
	d := t.TempDir()
	return home{dir: d, root: filepath.Join(d, ".local", "share", "io.ushineko.hello"), path: t.TempDir()}
}

func (h home) data(rel string) string {
	return filepath.Join(h.dir, ".local", "share", filepath.FromSlash(rel))
}
func (h home) link() string { return filepath.Join(h.dir, ".local", "bin", "hello") }

// run starts a program with only HOME and PATH set and no terminal, and
// returns its exit code and combined output.
func (h home) run(t *testing.T, prog string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), prog, args...)
	cmd.Env = []string{"HOME=" + h.dir, "PATH=" + h.path}
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

	// Desktop integration (R4, R15, R16).
	entry := h.data("applications/io.ushineko.hello.desktop")
	b, err := os.ReadFile(entry)
	require.NoError(t, err)
	require.Contains(t, string(b), "Exec="+filepath.Join(h.root, "bin", "hello")+"\n")
	require.Contains(t, string(b), "Icon=io.ushineko.hello\n")
	if tool, err := exec.LookPath("desktop-file-validate"); err == nil {
		out, err := exec.CommandContext(t.Context(), tool, entry).CombinedOutput()
		require.NoError(t, err, string(out))
		require.Empty(t, strings.TrimSpace(string(out)))
	} else {
		t.Log("desktop-file-validate is not installed; the entry was not validated")
	}
	for _, size := range []int{16, 32, 48, 64, 128, 256, 512} {
		p := h.data(fmt.Sprintf("icons/hicolor/%dx%d/apps/io.ushineko.hello.png", size, size))
		f, err := os.Open(p)
		require.NoError(t, err)
		cfg, err := png.DecodeConfig(f)
		require.NoError(t, f.Close())
		require.NoError(t, err, p)
		require.Equal(t, []int{size, size}, []int{cfg.Width, cfg.Height}, p)
	}
	target, err := filepath.EvalSymlinks(h.link())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(h.root, "bin", "hello"), target)
	require.Contains(t, out, "is not on your PATH", "the test PATH does not hold ~/.local/bin")

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

func TestUninstallRestoresTheFileALinkReplaced(t *testing.T) {
	h := newHome(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(h.link()), 0o750))
	require.NoError(t, os.WriteFile(h.link(), []byte("#!/bin/sh\necho my own hello\n"), 0o700))
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "replaces "+h.link())
	target, err := os.Readlink(h.link())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(h.root, "bin", "hello"), target)

	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

func TestTheMenuIsRefreshedAfterInstallAndUninstall(t *testing.T) {
	h := newHome(t)
	log := filepath.Join(t.TempDir(), "calls")
	fake := "#!/bin/sh\necho called >> '" + log + "'\n"
	require.NoError(t, os.WriteFile(filepath.Join(h.path, "kbuildsycoca6"), []byte(fake), 0o700)) // #nosec G306 -- a test script that must run
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	b, err := os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "called\n", string(b), "once, after the install")

	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--yes")
	require.Equal(t, 0, code, out)
	b, err = os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "called\ncalled\n", string(b), "and once after the uninstall")
	require.Equal(t, before, h.snap(t))
}

func greetHome(t *testing.T) home {
	t.Helper()
	h := newHome(t)
	h.root = filepath.Join(h.dir, ".local", "share", "io.ushineko.greet")
	return h
}

func TestOneConfigInstallsEachTargetsOwnPayload(t *testing.T) {
	require.NoError(t, setupFail)
	require.Equal(t, "linux/amd64", greetAMD64.Target)
	require.Equal(t, "linux/arm64", greetARM64.Target)
	if runtime.GOARCH != "amd64" {
		t.Skip("the amd64 half runs natively on amd64 only")
	}
	h := greetHome(t)
	before := h.snap(t)

	code, out := h.run(t, greetAMD64.Installer, "--yes")
	require.Equal(t, 0, code, out)
	got, err := exec.CommandContext(t.Context(), filepath.Join(h.root, "bin", "greet")).Output()
	require.NoError(t, err)
	require.Equal(t, "greet from linux/amd64\n", string(got))
	_, err = os.Stat(filepath.Join(h.root, "share", "arm64.txt"))
	require.True(t, errors.Is(err, os.ErrNotExist), "the arm64-only file is not in the amd64 payload")

	code, out = h.run(t, filepath.Join(h.root, "uninstall"), "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

func TestTheArm64InstallerInstallsUnderEmulation(t *testing.T) {
	require.NoError(t, setupFail)
	qemu, err := exec.LookPath("qemu-aarch64")
	if err != nil {
		t.Skip("qemu-aarch64 is not installed; the arm64 installer was built but not run")
	}
	h := greetHome(t)
	before := h.snap(t)

	code, out := h.run(t, qemu, greetARM64.Installer, "--yes")
	require.Equal(t, 0, code, out)
	b, err := os.ReadFile(filepath.Join(h.root, "share", "arm64.txt"))
	require.NoError(t, err)
	require.Equal(t, "This file is installed on linux/arm64 only.\n", string(b))
	f, err := elf.Open(filepath.Join(h.root, "bin", "greet"))
	require.NoError(t, err)
	require.Equal(t, elf.EM_AARCH64, f.Machine)
	require.NoError(t, f.Close())

	code, out = h.run(t, qemu, filepath.Join(h.root, "uninstall"), "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}
