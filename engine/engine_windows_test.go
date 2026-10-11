package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/internal/regtest"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

const (
	uninstallKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\io.example.hello`
	environment  = `HKCU\Environment`
)

// withShell adds a launcher entry, a link and a publisher to the fixture.
func (f *fixture) withShell() {
	f.m.App.Publisher = "Example"
	f.m.Desktop = []manifest.Desktop{{ID: "io.example.hello", Name: "Hello: the example", Comment: "Says hello", Exec: "bin/hello", Args: []string{"--name", "a b"}}}
	f.m.Links = []manifest.Link{{Name: "hello", Target: "bin/hello"}}
}

func (f *fixture) reg(t *testing.T, key, name string) (string, bool) {
	t.Helper()
	return regtest.Get(t, f.registry, key, name)
}

func TestAnInstallRegistersWithWindowsAndTheUninstallTakesItBack(t *testing.T) {
	f := newFixture(t)
	f.withShell()
	before := f.snap(t)

	p, err := NewPlan(f.m, Options{Env: f.env, Uninstaller: []byte("uninstaller")})
	require.NoError(t, err)
	require.Equal(t, []string{StepDirs, StepFiles, StepShell, StepRecord}, Steps(p))
	require.Equal(t, Steps(p), ManifestSteps(f.m, false), "the steps are known before the plan is")
	require.Empty(t, p.Links, "a link is a PATH entry here, not a file")
	r, err := Apply(context.Background(), p, f.payload, []byte("uninstaller"), nil)
	require.NoError(t, err)
	require.True(t, r.RefreshMenu, "PATH changed, so running programs are told")

	for name, want := range map[string]string{
		"DisplayName": "Hello", "DisplayVersion": "0.1.0", "Publisher": "Example",
		"InstallLocation":      f.root(),
		"UninstallString":      `"` + filepath.Join(f.root(), "uninstall.exe") + `"`,
		"QuietUninstallString": `"` + filepath.Join(f.root(), "uninstall.exe") + `" --quiet`,
		"EstimatedSize":        "1", "NoModify": "1", "NoRepair": "1",
	} {
		got, ok := f.reg(t, uninstallKey, name)
		require.True(t, ok, name)
		require.Equal(t, want, got, name)
	}
	_, ok := f.reg(t, uninstallKey, "DisplayIcon")
	require.False(t, ok, "this payload has no icon")
	path, _ := f.reg(t, environment, "Path")
	require.Equal(t, filepath.Join(f.root(), "bin"), path)

	shortcut := f.config("Microsoft", "Windows", "Start Menu", "Programs", "Hello_ the example.lnk")
	b, err := os.ReadFile(shortcut)
	require.NoError(t, err, "the name is the entry's, without the characters a file name cannot have")
	require.Equal(t, []byte{0x4c, 0, 0, 0, 0x01, 0x14, 0x02, 0}, b[:8], "a shell link's header")

	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t), "files and registry")
}

// PATH belongs to every installer, so the uninstall takes out its own entry
// and leaves what is there now, not what was there at install time.
func TestUninstallTakesItsOwnEntryOutOfPathAndLeavesTheRest(t *testing.T) {
	f := newFixture(t)
	f.withShell()
	bin := filepath.Join(f.root(), "bin")
	regtest.Set(t, f.registry, environment, "Path", `C:\one;%USERPROFILE%\two`, true)
	regtest.Set(t, f.registry, environment, "TEMP", `%USERPROFILE%\Temp`, true)

	r, err := f.install(t)
	require.NoError(t, err)
	path, _ := f.reg(t, environment, "Path")
	require.Equal(t, `C:\one;%USERPROFILE%\two;`+bin, path, "added at the end, the rest as written")

	regtest.Set(t, f.registry, environment, "Path", path+`;C:\three`, true) // another installer, later
	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	path, _ = f.reg(t, environment, "Path")
	require.Equal(t, `C:\one;%USERPROFILE%\two;C:\three`, path)
	temp, _ := f.reg(t, environment, "TEMP")
	require.Equal(t, `%USERPROFILE%\Temp`, temp, "other values are not touched")
}

func TestADirectoryAlreadyOnPathIsNotAddedOrRemoved(t *testing.T) {
	f := newFixture(t)
	f.withShell()
	mine := `C:\one;` + filepath.Join(f.root(), "BIN") + `\` // the person's own entry, written their way
	regtest.Set(t, f.registry, environment, "Path", mine, false)
	before := f.snap(t)

	r, err := f.install(t)
	require.NoError(t, err)
	path, _ := f.reg(t, environment, "Path")
	require.Equal(t, mine, path)
	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t), "the entry was the person's, and stays")
}

// An Uninstall entry that is there already, left by something else, is put
// back value for value.
func TestRegistryValuesThatWereThereArePutBack(t *testing.T) {
	f := newFixture(t)
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\io.example.hello`
	regtest.Set(t, f.registry, key, "DisplayName", "Something older", false)
	regtest.Set(t, f.registry, key, "Comments", "not one of ours", false)
	before := f.snap(t)

	r, err := f.install(t)
	require.NoError(t, err)
	name, _ := f.reg(t, key, "DisplayName")
	require.Equal(t, "Hello", name)
	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t))
}

// The registry step comes before the actions, so an action that fails
// leaves registry changes to undo.
func TestAFailedInstallUndoesItsRegistryChanges(t *testing.T) {
	f := newFixture(t)
	f.withShell()
	f.add("bin/setup.exe", "not a program", 0o755)
	f.m.Actions = []manifest.Action{{Run: &manifest.Run{Exec: "bin/setup.exe", NoUndo: true}}}
	regtest.Set(t, f.registry, environment, "Path", `C:\one`, false)
	before := f.snap(t)

	_, err := f.install(t)
	require.ErrorContains(t, err, "setup.exe")
	require.Equal(t, before, f.snap(t))
}

func TestPathListsAreComparedAsWindowsComparesPaths(t *testing.T) {
	list, added := platform.PathList(`C:\A;c:\tools\`, `C:\Tools`)
	require.False(t, added)
	require.Equal(t, `C:\A;c:\tools\`, list)
	list, added = platform.PathList("", `C:\x`)
	require.True(t, added)
	require.Equal(t, `C:\x`, list)
	list, _ = platform.PathList(`C:\A;`, `C:\x`)
	require.Equal(t, `C:\A;C:\x`, list, "no empty entry after a trailing separator")
	require.Equal(t, `C:\A;C:\B`, platform.PathListWithout(`C:\A;c:\X\;C:\B;C:\x`, `C:\x`))
}

const servicesKey = `HKLM\SYSTEM\CurrentControlSet\Services\`

// withService makes the fixture an install for everyone with one service,
// under a system root inside the test's home. The service manager is the
// stand-in that platform has for a registry root of the tests.
func (f *fixture) withService(t *testing.T, restart string) {
	t.Helper()
	user := f.env
	f.env = func(k string) string {
		if k == "FYNSTALL_TEST_SYSTEM_ROOT" {
			return filepath.Join(f.home, "machine")
		}
		return user(k)
	}
	require.NoError(t, os.Mkdir(filepath.Join(f.home, "machine"), 0o750))
	f.m.Scopes = []string{"user", "system"}
	f.m.Dirs["system"] = "{programs}/{id}"
	f.m.KeepOnUninstall = nil
	f.m.Actions = []manifest.Action{{Service: &manifest.Service{
		Name: "hello", Exec: "bin/hello", Args: []string{"serve", "{data}/a b"}, Start: true, Restart: restart,
	}}}
}

func (f *fixture) installSystem(t *testing.T) (*Receipt, error) {
	t.Helper()
	p, err := NewPlan(f.m, Options{Scope: "system", Env: f.env, Uninstaller: []byte("uninstaller")})
	require.NoError(t, err)
	return Apply(context.Background(), p, f.payload, []byte("uninstaller"), nil)
}

// A service action is a service of the Windows service manager: registered
// when the install is for everyone, started, and stopped and removed by the
// uninstaller before its files go (spec 002 D2a).
func TestAServiceIsRegisteredWithWindowsAndRemovedAgain(t *testing.T) {
	f := newFixture(t)
	f.withService(t, "on-failure")
	before := f.snap(t)

	_, err := NewPlan(f.m, Options{Scope: "user", Env: f.env, Uninstaller: []byte("uninstaller")})
	require.ErrorContains(t, err, "--scope system", "Windows has no services of one user")

	var events []string
	p, err := NewPlan(f.m, Options{Scope: "system", Env: f.env, Uninstaller: []byte("uninstaller")})
	require.NoError(t, err)
	r, err := Apply(context.Background(), p, f.payload, []byte("uninstaller"), nil)
	require.NoError(t, err)

	root := filepath.Join(f.home, "machine", "Program Files", "io.example.hello")
	for name, want := range map[string]string{
		// Each word quoted as Windows reads a command line; an argument is
		// passed as the config wrote it.
		"ImagePath":   `"` + filepath.Join(root, "bin", "hello") + `" serve "` + filepath.Join(f.home, "machine", "ProgramData") + `/a b"`,
		"DisplayName": "hello", "Description": "Hello", "ObjectName": "LocalSystem", "Start": "2",
		platform.StandInRestart: "on-failure", platform.StandInState: "running",
	} {
		got, ok := f.reg(t, servicesKey+"hello", name)
		require.True(t, ok, name)
		require.Equal(t, want, got, name)
	}
	at := slices.IndexFunc(r.Journal, func(e Entry) bool { return e.Op == OpService })
	require.GreaterOrEqual(t, at, 0)
	require.Equal(t, `HKCU\`+f.registry+`\`+servicesKey+"hello", r.Journal[at].Path, "the journal names the service by its key")

	// A newer version plans for after this one's uninstaller has run, so the
	// service it removes is not in the way (R17).
	_, err = NewPlan(f.m, Options{Scope: "system", Env: f.env, Uninstaller: []byte("uninstaller"), Replacing: r})
	require.NoError(t, err)

	_, err = Uninstall(r, ReasonUninstall, func(e Event) { events = append(events, e.Text) })
	require.NoError(t, err)
	service, program := slices.Index(events, "removed service hello"), slices.Index(events, "removed "+filepath.Join(root, "bin", "hello"))
	require.GreaterOrEqual(t, service, 0, events)
	require.Less(t, service, program, "the service goes before its program")
	require.Equal(t, before, f.snap(t), "files and registry")
}

// A service that is there already is someone else's. The service manager
// does not give back all that a service was set up with, so the install
// stops instead of replacing what it could not put back.
func TestAServiceThatIsThereAlreadyStopsTheInstall(t *testing.T) {
	f := newFixture(t)
	f.withService(t, "no")
	regtest.Set(t, f.registry, servicesKey+"hello", "ImagePath", `C:\elsewhere\hello.exe`, true)
	before := f.snap(t)

	_, err := NewPlan(f.m, Options{Scope: "system", Env: f.env, Uninstaller: []byte("uninstaller")})
	require.ErrorContains(t, err, "already has a service of that name")
	require.Equal(t, before, f.snap(t))
}

// An action after the service fails: the service is stopped and removed
// with the rest.
func TestAFailedInstallRemovesTheServiceItRegistered(t *testing.T) {
	f := newFixture(t)
	f.withService(t, "always")
	f.add("bin/setup.exe", "not a program", 0o755)
	f.m.Actions = append(f.m.Actions, manifest.Action{Run: &manifest.Run{Exec: "bin/setup.exe", NoUndo: true}})
	before := f.snap(t)

	_, err := f.installSystem(t)
	require.ErrorContains(t, err, "setup.exe")
	require.Equal(t, before, f.snap(t))
}
