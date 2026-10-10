package builder_test

/*
The Windows half of the integration tests (spec 001 phase 7). They build
the examples/greet installer with the real builder and go build, then run it
and its uninstaller as real processes against a temporary profile. Nothing
is mocked. examples/greet is pure Go, so these need no C compiler; the
wizard, and examples/hello with it, come later in phase 7.

The builds happen once, in TestMain, with GOPROXY=off, so the tests need
the module cache and not the network.
*/

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/builder"
	"github.com/ushineko/fynstall/internal/regtest"
	"github.com/ushineko/fynstall/internal/snapshot"
)

const (
	v1 = "1.0.0-test"
	v2 = "2.0.0-test"
)

var (
	src       string             // the staged copy of examples/greet, built for each target
	all       []builder.Artifact // every target of the config, from one build
	v1Art     builder.Artifact   // windows/amd64
	v1Again   builder.Artifact
	v2Art     builder.Artifact // differs only in its stamped runtime version
	greet020  builder.Artifact // app version 0.2.0: an upgrade
	twoScopes builder.Artifact // a config that offers user and system scope
	shell     builder.Artifact // a launcher entry, an icon, a link and a publisher
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

const windows = "windows/amd64"

func setup(work string) error {
	repo, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	src = filepath.Join(work, "greet")
	for _, f := range []string{"fynstall.yaml", "notes/arm64.txt"} {
		if err := copyFile(filepath.Join(repo, "examples", "greet", filepath.FromSlash(f)), filepath.Join(src, filepath.FromSlash(f))); err != nil {
			return err
		}
	}
	for _, target := range []string{"linux/amd64", "linux/arm64", windows} {
		goos, arch, _ := strings.Cut(target, "/")
		name := "greet"
		if goos == "windows" {
			name += ".exe"
		}
		cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath",
			"-o", filepath.Join(src, "build", goos+"-"+arch, name), "./examples/greet")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+arch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build greet for %s: %w\n%s", target, err, out)
		}
	}
	build := func(config, rv, out string, targets ...string) ([]builder.Artifact, error) {
		return builder.Build(context.Background(), builder.Options{
			Config: config, OutDir: filepath.Join(work, out), Targets: targets,
			CLIOnly: true, RuntimePath: repo, RuntimeVersion: rv, Env: []string{"GOPROXY=off"},
		})
	}
	one := func(config, rv, out string) (builder.Artifact, error) {
		arts, err := build(config, rv, out, windows)
		if err != nil {
			return builder.Artifact{}, err
		}
		return arts[0], nil
	}
	config := filepath.Join(src, "fynstall.yaml")
	if all, err = build(config, v1, "v1"); err != nil {
		return err
	}
	if len(all) != 3 || all[2].Target != windows {
		return fmt.Errorf("greet: want its three targets with %s last, got %+v", windows, all)
	}
	v1Art = all[2]
	if v1Again, err = one(config, v1, "v1-again"); err != nil {
		return err
	}
	if v2Art, err = one(config, v2, "v2"); err != nil {
		return err
	}

	// The same program at app version 0.2.0, for the upgrade.
	b, err := os.ReadFile(config)
	if err != nil {
		return err
	}
	s := strings.Replace(string(b), "version: 0.1.0", "version: 0.2.0", 1)
	if s == string(b) {
		return errors.New("the greet config changed shape; update the 0.2.0 build")
	}
	newer := filepath.Join(src, "fynstall-0.2.0.yaml")
	if err := os.WriteFile(newer, []byte(s), 0o600); err != nil {
		return err
	}
	if greet020, err = one(newer, v1, "greet-0.2.0"); err != nil {
		return err
	}

	// A config that offers both scopes, as examples/hello does.
	dir := filepath.Join(work, "scopes")
	cfg := "app:\n  id: io.example.scopes\n  name: Scopes\n  version: 1.0.0\n" +
		"install:\n  scopes: [user, system]\npayload:\n  - src: notes.txt\n    dst: notes.txt\n"
	for name, content := range map[string]string{"fynstall.yaml": cfg, "notes.txt": "notes\n"} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return err
		}
	}
	if twoScopes, err = one(filepath.Join(dir, "fynstall.yaml"), v1, "scopes-dist"); err != nil {
		return err
	}

	// A program with everything the Windows shell is told about.
	dir = filepath.Join(work, "shell")
	cfg = "app:\n  id: io.example.shell\n  name: Shell Example\n  version: 1.2.3\n  publisher: Example Makers\n  icon: icon.png\n" +
		"payload:\n  - src: greet.exe\n    dst: bin/greet.exe\n" +
		"integration:\n  path_links: [bin/greet.exe]\n  desktop:\n    - name: Shell Example\n      comment: Greets\n      exec: bin/greet.exe\n      args: [--flag, two words]\n"
	if err := copyFile(filepath.Join(src, "build", "windows-amd64", "greet.exe"), filepath.Join(dir, "greet.exe")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(repo, "examples", "hello", "hello.png"), filepath.Join(dir, "icon.png")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "fynstall.yaml"), []byte(cfg), 0o600); err != nil {
		return err
	}
	shell, err = one(filepath.Join(dir, "fynstall.yaml"), v1, "shell-dist")
	return err
}

func copyFile(from, to string) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		return err
	}
	return os.WriteFile(to, b, 0o600)
}

// home is a temporary profile, the install directory the default config
// gives inside it, and a temporary directory outside it. LOCALAPPDATA and
// APPDATA are left unset, so both come from the profile.
type home struct {
	dir  string
	root string
	tmp  string
	path string
	// reg is the test's own registry root, below HKCU: the installer writes
	// its Uninstall entry and PATH there, never in the real ones.
	reg string
}

func newHome(t *testing.T, id string) home {
	t.Helper()
	require.NoError(t, setupFail)
	d := t.TempDir()
	return home{dir: d, root: filepath.Join(d, "AppData", "Local", "Programs", id), tmp: t.TempDir(), path: t.TempDir(), reg: regtest.Root(t)}
}

func greetHome(t *testing.T) home { return newHome(t, "io.ushineko.greet") }

func (h home) config() string {
	return filepath.Join(h.dir, "AppData", "Roaming", "io.ushineko.greet", "config.json")
}

func (h home) uninstaller() string { return filepath.Join(h.root, "uninstall.exe") }

// run starts a program with only the profile, an empty PATH, the temporary
// directory and SystemRoot set, and no console, and returns its exit code
// and combined output.
func (h home) run(t *testing.T, prog string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), prog, args...)
	require.NotEmpty(t, h.reg, "without its own registry root an installer would write the real registry")
	cmd.Env = []string{"USERPROFILE=" + h.dir, "PATH=" + h.path, "TEMP=" + h.tmp, "TMP=" + h.tmp,
		"SystemRoot=" + os.Getenv("SystemRoot"), regtest.Env + "=" + h.reg}
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
	// The registry is part of "as it was" (R9d).
	for k, v := range regtest.Snapshot(t, h.reg) {
		s["registry:"+k] = v
	}
	return s
}

// value reads a registry value the installer wrote, by its real key.
func (h home) value(t *testing.T, key, name string) string {
	t.Helper()
	v, ok := regtest.Get(t, h.reg, key, name)
	require.True(t, ok, "%s in %s", name, key)
	return v
}

// movedAside lists the uninstallers that moved themselves to the temporary
// directory (R9f).
func (h home) movedAside(t *testing.T) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(h.tmp, "fynstall-removed-*.exe"))
	require.NoError(t, err)
	return got
}

func sum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallWritesThePayloadAndTheUninstallerRemovesIt(t *testing.T) {
	h := greetHome(t)
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--cli", "--yes", "--name=Ada", "--token=example-not-a-credential")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "To remove it, run "+h.uninstaller())
	bin := filepath.Join(h.root, "bin")
	require.Contains(t, out, "PATH     "+bin+" is added to yours", "the plan says so before it is done")
	require.Contains(t, out, bin+" is on your PATH.")
	require.Equal(t, bin, h.value(t, `HKCU\Environment`, "Path"), "greet's link is a PATH entry on Windows (spec 002 L10)")
	require.Equal(t, sum(t, filepath.Join(src, "build", "windows-amd64", "greet.exe")), sum(t, filepath.Join(h.root, "bin", "greet.exe")))
	require.Equal(t, sum(t, v1Art.Uninstaller), sum(t, h.uninstaller()),
		"the installed uninstaller is the separate artifact in dist (R9b, R9c)")
	_, err := os.Stat(filepath.Join(h.dir, "AppData", "Local", "fynstall", "installs", "io.ushineko.greet.json"))
	require.NoError(t, err, "the index entry is under {data}")

	cmd := exec.CommandContext(t.Context(), filepath.Join(h.root, "bin", "greet.exe"))
	cmd.Env = []string{"AppData=" + filepath.Join(h.dir, "AppData", "Roaming"), "SystemRoot=" + os.Getenv("SystemRoot")}
	got, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "hello from windows/amd64, Ada (with a token)\n", string(got), "the program reads what the installer wrote under {config}")

	// The uninstaller is the running program, and one of the files it
	// removes. The install directory is gone when it exits (R9f).
	code, out = h.run(t, h.uninstaller(), "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Removed Greet")
	require.Equal(t, before, h.snap(t))
	aside := h.movedAside(t)
	require.Len(t, aside, 1, "it moved its own file out of the install directory")
	require.Equal(t, sum(t, v1Art.Uninstaller), sum(t, aside[0]))
}

func TestUninstallRestoresAFileTheInstallReplaced(t *testing.T) {
	h := greetHome(t)
	mine := filepath.Join(h.root, "bin", "greet.exe")
	require.NoError(t, os.MkdirAll(filepath.Dir(mine), 0o750))
	require.NoError(t, os.WriteFile(mine, []byte("my own program\n"), 0o600))
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "replaces "+mine)
	require.Equal(t, sum(t, filepath.Join(src, "build", "windows-amd64", "greet.exe")), sum(t, mine))

	code, out = h.run(t, h.uninstaller(), "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

func TestANewerInstallerRemovesThroughTheInstalledUninstaller(t *testing.T) {
	h := greetHome(t)
	before := h.snap(t)
	code, out := h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)

	// The same app version again is a repair (R17), and it removes the
	// install through the installed uninstaller, not its own engine. That
	// uninstaller holds the install lock while the installer waits.
	code, out = h.run(t, v2Art.Installer, "--yes", "--verbose")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Repairs Greet 0.1.0")
	require.Contains(t, out, "fynstall uninstaller "+v1, "the v1 uninstaller ran (R9e)")
	require.Equal(t, sum(t, v2Art.Uninstaller), sum(t, h.uninstaller()), "and the repair installed its own")

	code, out = h.run(t, v2Art.Installer, "--uninstall", "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "fynstall uninstaller "+v2, "--uninstall runs the installed uninstaller, which the repair installed (R9e)")
	require.Equal(t, before, h.snap(t))
	require.Len(t, h.movedAside(t), 2, "each uninstaller that ran moved itself aside")
}

func TestAnUpgradeKeepsTheParametersItWasGiven(t *testing.T) {
	h := greetHome(t)
	before := h.snap(t)
	code, out := h.run(t, v1Art.Installer, "--yes", "--name=Ada")
	require.Equal(t, 0, code, out)

	code, out = h.run(t, greet020.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Upgrades Greet 0.1.0 to 0.2.0")
	b, err := os.ReadFile(h.config())
	require.NoError(t, err)
	require.JSONEq(t, `{"greeting":"hello","name":"Ada","token":""}`, string(b), "the name came from the 0.1.0 receipt")

	code, out = h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 2, code, "an older version over a newer one, without --downgrade")
	require.Contains(t, out, "--downgrade")

	code, out = h.run(t, h.uninstaller())
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Removed Greet 0.2.0")
	require.Equal(t, before, h.snap(t))
}

func TestBuildsAreReproducible(t *testing.T) {
	require.NoError(t, setupFail)
	require.Equal(t, sum(t, v1Art.Installer), sum(t, v1Again.Installer))
	require.Equal(t, sum(t, v1Art.Uninstaller), sum(t, v1Again.Uninstaller))
}

func TestOneConfigBuildsEveryTargetFromThisMachine(t *testing.T) {
	require.NoError(t, setupFail)
	for _, a := range all {
		for _, p := range []string{a.Installer, a.Uninstaller} {
			if a.Target == windows {
				require.True(t, strings.HasSuffix(p, ".exe"), p)
				f, err := pe.Open(p)
				require.NoError(t, err, p)
				// debug/pe lists no libraries, only "symbol:library" pairs.
				syms, err := f.ImportedSymbols()
				require.NoError(t, err)
				require.NotEmpty(t, syms)
				for _, s := range syms {
					_, lib, _ := strings.Cut(s, ":")
					require.Equal(t, "kernel32.dll", strings.ToLower(lib), "%s: a CLI-only program links no graphics libraries (R12), but imports %s", p, s)
				}
				require.NoError(t, f.Close())
				continue
			}
			require.False(t, strings.HasSuffix(p, ".exe"), p)
			f, err := elf.Open(p)
			require.NoError(t, err, "%s: a Linux target builds a Linux program from Windows", p)
			require.NoError(t, f.Close())
		}
	}
}

func TestTheInstallerRefusesWhatItCannotDo(t *testing.T) {
	h := greetHome(t)
	before := h.snap(t)

	code, out := h.run(t, v1Art.Installer, "--gui")
	require.Equal(t, 2, code)
	require.Contains(t, out, "built without the wizard")

	code, out = h.run(t, v1Art.Installer)
	require.Equal(t, 2, code, "no console and no --yes")
	require.Contains(t, out, "--yes")

	code, out = h.run(t, v1Art.Installer, "--uninstall", "--yes")
	require.Equal(t, 1, code)
	require.Contains(t, out, "not installed")

	code, out = h.run(t, v1Art.Installer, "--dry-run")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "create   "+filepath.Join(h.root, "bin", "greet.exe"))
	require.Contains(t, out, "create   "+h.root+`\`+"\n", "a directory ends in this OS's separator")
	require.Equal(t, before, h.snap(t), "none of these changed anything")
}

// The uninstaller beside the installer in dist/ is not inside an install:
// it hands over to the installed copy, or says there is nothing to remove.
func TestTheUninstallerInDistHandsOverToTheInstalledOne(t *testing.T) {
	h := greetHome(t)
	before := h.snap(t)

	code, out := h.run(t, v1Art.Uninstaller, "--yes")
	require.Equal(t, 1, code)
	require.Contains(t, out, "Greet is not installed for this user")

	code, out = h.run(t, v1Art.Installer, "--yes")
	require.Equal(t, 0, code, out)
	code, out = h.run(t, v1Art.Uninstaller, "--yes")
	require.Equal(t, 0, code, out)
	require.Equal(t, 2, strings.Count(out, "fynstall uninstaller "+v1), "the dist copy, then the installed one it ran")
	require.Contains(t, out, "Removed Greet")
	require.Equal(t, before, h.snap(t))
	_, err := os.Stat(v1Art.Uninstaller)
	require.NoError(t, err, "the copy in dist removes the install, not itself")
}

// A config that offers both scopes installs per-user on Windows, and says
// that the other scope is not there yet rather than failing on it.
func TestSystemScopeIsRefusedAndPerUserStillInstalls(t *testing.T) {
	h := newHome(t, "io.example.scopes")
	before := h.snap(t)

	code, out := h.run(t, twoScopes.Installer, "--yes", "--scope", "system")
	require.Equal(t, 1, code, out)
	require.Contains(t, out, "system scope")
	require.Contains(t, out, "not available on this platform yet")
	require.Equal(t, before, h.snap(t))

	code, out = h.run(t, twoScopes.Installer, "--yes")
	require.Equal(t, 0, code, out)
	b, err := os.ReadFile(filepath.Join(h.root, "notes.txt"))
	require.NoError(t, err)
	require.Equal(t, "notes\n", string(b))

	code, out = h.run(t, h.uninstaller())
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

// The files a program made are listed, and removed when asked for.
func TestLeftoversAreListedOrRemoved(t *testing.T) {
	for _, remove := range []bool{false, true} {
		h := greetHome(t)
		before := h.snap(t)
		code, out := h.run(t, v1Art.Installer, "--yes")
		require.Equal(t, 0, code, out)
		made := filepath.Join(h.root, "bin", "cache", "state.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(made), 0o750))
		require.NoError(t, os.WriteFile(made, []byte("made by the program"), 0o600))

		if !remove {
			code, out = h.run(t, h.uninstaller())
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "Left 1 file the program made")
			require.Contains(t, out, made)
			_, err := os.Stat(made)
			require.NoError(t, err, "listed, not deleted")
			_, err = os.Stat(h.uninstaller())
			require.True(t, errors.Is(err, os.ErrNotExist), "the uninstaller itself is gone")
			continue
		}
		code, out = h.run(t, h.uninstaller(), "--remove-leftovers")
		require.Equal(t, 0, code, out)
		require.NotContains(t, out, "Left ")
		require.Equal(t, before, h.snap(t))
	}
}

const shellUninstallKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\io.example.shell`

// What Linux does with a .desktop file, an icon theme and a link, Windows
// does with a Start Menu shortcut, an .ico and PATH; and every install has
// the entry Settings > Apps lists (R18, spec 002 L8 and L10).
func TestAnInstallIsInTheStartMenuInSettingsAndOnPath(t *testing.T) {
	h := newHome(t, "io.example.shell")
	before := h.snap(t)

	code, out := h.run(t, shell.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "==> Registering with Windows")

	icon := filepath.Join(h.root, ".fynstall", "app.ico")
	b, err := os.ReadFile(icon)
	require.NoError(t, err)
	require.Equal(t, []byte{0, 0, 1, 0, 5, 0}, b[:6], "an icon file with five sizes")
	// Explorer shows the same icon for the installer and the uninstaller:
	// each carries the images as resources.
	for _, p := range []string{shell.Installer, shell.Uninstaller, h.uninstaller()} {
		builder.RequireIcon(t, b, builder.ReadResources(t, p))
	}
	uninstall := `"` + h.uninstaller() + `"`
	for name, want := range map[string]string{
		"DisplayName": "Shell Example", "DisplayVersion": "1.2.3", "Publisher": "Example Makers",
		"InstallLocation": h.root, "DisplayIcon": icon,
		"UninstallString": uninstall, "QuietUninstallString": uninstall + " --quiet",
		"NoModify": "1", "NoRepair": "1",
	} {
		require.Equal(t, want, h.value(t, shellUninstallKey, name), name)
	}
	require.Equal(t, filepath.Join(h.root, "bin"), h.value(t, `HKCU\Environment`, "Path"))

	// The shortcut, read back by the shell that Explorer uses.
	lnk := filepath.Join(h.dir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs", "Shell Example.lnk")
	_, err = os.Stat(lnk)
	require.NoError(t, err)
	if ps, err := exec.LookPath("powershell.exe"); err == nil {
		script := `$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:LNK); ` +
			`"$($s.TargetPath)|$($s.Arguments)|$($s.WorkingDirectory)|$($s.Description)|$($s.IconLocation)"`
		cmd := exec.CommandContext(t.Context(), ps, "-NoProfile", "-NonInteractive", "-Command", script)
		cmd.Env = append(os.Environ(), "LNK="+lnk)
		got, err := cmd.Output()
		require.NoError(t, err)
		require.Equal(t, strings.Join([]string{
			filepath.Join(h.root, "bin", "greet.exe"), `--flag "two words"`, h.root, "Greets", icon + ",0",
		}, "|"), strings.TrimSpace(string(got)))
	} else {
		t.Log("powershell.exe is not on PATH; the shortcut was not read back")
	}

	// Settings > Apps runs QuietUninstallString, or UninstallString.
	code, out = h.run(t, h.uninstaller(), "--quiet")
	require.Equal(t, 0, code, out)
	require.Empty(t, out, "--quiet prints only problems")
	require.Equal(t, before, h.snap(t), "the files, the shortcut and the registry")
}

// A shortcut and a PATH that were there before are as they were after.
func TestUninstallPutsBackTheShortcutAndLeavesTheRestOfPath(t *testing.T) {
	h := newHome(t, "io.example.shell")
	lnk := filepath.Join(h.dir, "AppData", "Roaming", "Microsoft", "Windows", "Start Menu", "Programs", "Shell Example.lnk")
	require.NoError(t, os.MkdirAll(filepath.Dir(lnk), 0o750))
	require.NoError(t, os.WriteFile(lnk, []byte("the person's own shortcut"), 0o600))
	regtest.Set(t, h.reg, `HKCU\Environment`, "Path", `C:\one;%USERPROFILE%\two`, true)
	before := h.snap(t)

	code, out := h.run(t, shell.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "shortcut "+lnk)
	require.Contains(t, out, "replaces the one there")
	require.Equal(t, `C:\one;%USERPROFILE%\two;`+filepath.Join(h.root, "bin"), h.value(t, `HKCU\Environment`, "Path"))

	code, out = h.run(t, h.uninstaller())
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}

// An upgrade goes through the old uninstaller, which takes its registry
// entries with it, and the new version writes its own.
func TestARepairLeavesOneSetOfRegistryEntries(t *testing.T) {
	h := newHome(t, "io.example.shell")
	before := h.snap(t)
	code, out := h.run(t, shell.Installer, "--yes")
	require.Equal(t, 0, code, out)
	// A shortcut holds the times of the file it points at, which the repair
	// wrote again, so its bytes differ.
	const lnk = "AppData/Roaming/Microsoft/Windows/Start Menu/Programs/Shell Example.lnk"
	installed := h.snap(t)
	require.Contains(t, installed, lnk)
	delete(installed, lnk)

	code, out = h.run(t, shell.Installer, "--yes")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "Repairs Shell Example 1.2.3")
	repaired := h.snap(t)
	require.Contains(t, repaired, lnk)
	delete(repaired, lnk)
	require.Equal(t, installed, repaired, "the same files, and PATH with the directory once")

	code, out = h.run(t, h.uninstaller())
	require.Equal(t, 0, code, out)
	require.Equal(t, before, h.snap(t))
}
