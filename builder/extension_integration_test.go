//go:build linux || windows

package builder_test

/*
A program's own installer code, end to end (spec 003): examples/keyed is
built with the real builder, which links its extension into the installer
and the uninstaller, and both are run as real processes against a temporary
home. Its hooks leave a line each in a file the test names, so the test sees
which ran, in what order, and with what.
*/

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/builder"
)

var (
	keyedOnce sync.Once
	keyedArt  builder.Artifact
	errKeyed  error
)

// keyed builds examples/keyed's CLI installer once. The config stays in
// the repository: its extension is a package of this module, so it has to
// be built from inside it.
func keyed(t *testing.T) builder.Artifact {
	t.Helper()
	keyedOnce.Do(func() {
		repo, err := filepath.Abs("..")
		if err != nil {
			errKeyed = err
			return
		}
		exe := ""
		if runtime.GOOS == "windows" {
			exe = ".exe"
		}
		cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath",
			"-o", filepath.Join(repo, "examples", "keyed", "bin", "keyed"+exe), "./examples/keyed")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			errKeyed = fmt.Errorf("build keyed: %w\n%s", err, out)
			return
		}
		out, err := os.MkdirTemp("", "fynstall-keyed-*")
		if err != nil {
			errKeyed = err
			return
		}
		arts, err := builder.Build(context.Background(), builder.Options{
			Config: filepath.Join(repo, "examples", "keyed", "fynstall.yaml"), OutDir: out,
			CLIOnly: true, RuntimePath: repo, RuntimeVersion: "1.0.0-test", Env: []string{"GOPROXY=off"},
		})
		if err != nil {
			errKeyed = err
			return
		}
		keyedArt = arts[0]
	})
	require.NoError(t, errKeyed)
	return keyedArt
}

// marks are the lines Keyed's hooks left in dir, each cut after its name
// and what follows it up to the first space past field n.
func marks(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "hooks.log"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

/*
The four hooks run around the changes, in order, with the parameters the
install was given: the two install hooks from the installer, the two
uninstall hooks from the uninstaller that was installed, which reads the
account back from its own record. And the home ends as it began.
*/
func TestAProgramsHooksRunAroundTheInstallAndTheUninstall(t *testing.T) {
	art := keyed(t)
	h := keyedHome(t)
	before := h.snap(t)
	dir := t.TempDir()

	code, out := h.run(t, art.Installer, "--cli", "--yes", "--account=ada", "--marker-dir="+dir)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "before the install, runs Keyed's own set-up code", "the plan says the program's own code runs (R7)")
	require.Contains(t, out, "Keyed's before-install hook ran for ada", "a hook's log line is shown")
	require.Equal(t, []string{
		"before-install account=ada root=io.ushineko.keyed scope=user upgrade=false",
		"after-install account=ada root=io.ushineko.keyed scope=user upgrade=false",
	}, marks(t, dir))

	code, out = h.run(t, keyedUninstaller(h), "--cli")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Before removing anything, it runs Keyed's own clean-up code.")
	require.Equal(t, []string{
		"before-uninstall account=ada root=io.ushineko.keyed scope=user upgrade=false",
		"after-uninstall account=ada root=io.ushineko.keyed scope=user upgrade=false",
	}, marks(t, dir)[2:], "the installed uninstaller runs the extension it was built with")
	require.Equal(t, before, h.snap(t))
}

// A BeforeInstall hook that fails stops the install before any change.
func TestAHookThatRefusesStopsTheInstallWithNothingChanged(t *testing.T) {
	art := keyed(t)
	h := keyedHome(t)
	before := h.snap(t)

	code, out := h.run(t, art.Installer, "--cli", "--yes", "--account=refused")
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "nothing was changed")
	require.Contains(t, out, "the account refused is not allowed in")
	require.Equal(t, before, h.snap(t))
}

// The CLI rule: an install that may not ask stops at a page whose required
// parameter has no value, and names the flag that answers it.
func TestASilentInstallNamesTheFlagAPageNeeds(t *testing.T) {
	art := keyed(t)
	h := keyedHome(t)
	before := h.snap(t)

	code, out := h.run(t, art.Installer, "--cli", "--yes")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "--account=…")
	require.Contains(t, out, "sign-in")
	require.Equal(t, before, h.snap(t))
}

// --dry-run lists the hooks and runs none of them.
func TestADryRunListsTheHooksAndRunsNone(t *testing.T) {
	art := keyed(t)
	h := keyedHome(t)
	before := h.snap(t)
	dir := t.TempDir()

	code, out := h.run(t, art.Installer, "--cli", "--dry-run", "--account=ada", "--marker-dir="+dir)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "before the install, runs Keyed's own set-up code")
	require.Contains(t, out, "after the install, runs Keyed's own set-up code")
	require.Empty(t, marks(t, dir))
	require.Equal(t, before, h.snap(t))
}

// An extension that needs another fynstall than the builder is stops the
// build, and both versions are named (R2). A module that is fynstall's own,
// or one that replaces fynstall with a directory, is not held to it.
func TestAnExtensionForAnotherFynstallStopsTheBuild(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	write("go.mod", "module example.test/app\n\ngo 1.26.0\n\nrequire github.com/ushineko/fynstall v9.9.9\n")
	write("installer/installer.go", "package installer\n")
	write("bin/app", "x")
	write("fynstall.yaml", "app:\n  id: io.example.app\n  name: App\n  version: 0.1.0\npayload:\n  - src: bin/app\n    dst: bin/app\nextension: ./installer\n")

	_, err := builder.Build(context.Background(), builder.Options{
		Config: filepath.Join(dir, "fynstall.yaml"), CLIOnly: true, RuntimeVersion: "0.2.0", Env: []string{"GOPROXY=off"},
	})
	require.ErrorContains(t, err, "requires fynstall 9.9.9")
	require.ErrorContains(t, err, "this builder is fynstall 0.2.0")

	write("go.mod", "module example.test/app\n\ngo 1.26.0\n")
	_, err = builder.Build(context.Background(), builder.Options{
		Config: filepath.Join(dir, "fynstall.yaml"), CLIOnly: true, RuntimeVersion: "0.2.0", Env: []string{"GOPROXY=off"},
	})
	require.ErrorContains(t, err, "does not require github.com/ushineko/fynstall")
}

// An extension whose package needs the wizard to build cannot go into a
// --cli-only installer: the build stops and says how to split it (the
// nogui rule).
func TestAnExtensionThatNeedsTheWizardToBuildStopsTheBuild(t *testing.T) {
	repo, err := filepath.Abs("..")
	require.NoError(t, err)
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	require.NoError(t, err)
	write("go.mod", fmt.Sprintf("module example.test/app\n\ngo 1.26.0\n\nrequire github.com/ushineko/fynstall v0.0.0\n\nreplace github.com/ushineko/fynstall => %q\n",
		filepath.ToSlash(repo)))
	write("go.sum", string(sum))
	// No build tag: the window side is in the file every build compiles.
	write("installer/installer.go", "package installer\n\nimport (\n\t\"github.com/ushineko/fynstall/extension\"\n\t_ \"github.com/ushineko/fynstall/extension/gui\"\n)\n\nvar Extension = extension.Extension{}\n")
	write("bin/app", "x")
	write("fynstall.yaml", "app:\n  id: io.example.app\n  name: App\n  version: 0.1.0\npayload:\n  - src: bin/app\n    dst: bin/app\nextension: ./installer\n")

	_, err = builder.Build(context.Background(), builder.Options{
		Config: filepath.Join(dir, "fynstall.yaml"), CLIOnly: true, RuntimePath: repo, RuntimeVersion: "1.0.0-test",
		Env: []string{"GOPROXY=off", "GOFLAGS=-mod=mod"},
	})
	require.ErrorContains(t, err, "does not build without the wizard")
	require.ErrorContains(t, err, "//go:build !nogui")
}
