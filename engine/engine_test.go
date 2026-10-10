package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/ushineko/fynstall/internal/snapshot"
	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// fixture is a manifest and payload for io.example.hello, and a temp HOME
// to install into. Every path the engine touches is under home.
type fixture struct {
	home    string
	m       *manifest.Manifest
	payload fstest.MapFS
	env     func(string) string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{home: t.TempDir(), payload: fstest.MapFS{}}
	f.env = func(k string) string {
		if k == "HOME" || k == "USERPROFILE" {
			return f.home
		}
		return ""
	}
	f.m = &manifest.Manifest{
		Schema: manifest.Schema, RuntimeVersion: "test",
		App:    manifest.App{ID: "io.example.hello", Name: "Hello", Version: "0.1.0"},
		Scopes: []string{"user"}, Dirs: map[string]string{"user": "{data}/{id}"},
		KeepOnUninstall: []string{"{config}/hello"},
	}
	f.add("bin/hello", "#!/bin/sh\necho hello\n", 0o755)
	f.add("share/doc/README", "hello\n", 0o644)
	return f
}

func (f *fixture) add(path, content string, mode uint32) {
	sum := sha256.Sum256([]byte(content))
	f.payload[path] = &fstest.MapFile{Data: []byte(content)}
	f.m.Files = append(f.m.Files, manifest.File{Path: path, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), Mode: mode})
}

// data and config are paths under {data} and {config}, wherever this OS
// puts them in the home.
func (f *fixture) data(elem ...string) string   { return f.under("data", elem) }
func (f *fixture) config(elem ...string) string { return f.under("config", elem) }

func (f *fixture) under(key string, elem []string) string {
	v, err := platform.Vars("user", f.env)
	if err != nil {
		panic(err)
	}
	return filepath.Join(append([]string{v[key]}, elem...)...)
}

func (f *fixture) root() string { return f.data("io.example.hello") }

// posixModes skips a check of permission bits where the OS keeps none.
func posixModes() bool { return runtime.GOOS != "windows" }

// needsLinks skips a test that makes symlinks where this process may not:
// on Windows that takes a privilege most users do not have.
func needsLinks(t *testing.T) {
	t.Helper()
	if err := os.Symlink("target", filepath.Join(t.TempDir(), "link")); err != nil {
		t.Skipf("this process cannot make symlinks: %v", err)
	}
}

// needsIntegration skips a test of launcher entries, icons and links in
// {bin} on an OS whose backend does not make them.
func needsIntegration(t *testing.T) {
	t.Helper()
	if !platform.HasDesktopIntegration {
		t.Skip("this OS's backend makes no launcher entries or links yet")
	}
}

func (f *fixture) snap(t *testing.T) map[string]string {
	t.Helper()
	s, err := snapshot.Take(f.home)
	require.NoError(t, err)
	return s
}

func (f *fixture) install(t *testing.T) (*Receipt, error) {
	t.Helper()
	p, err := NewPlan(f.m, Options{Env: f.env, Uninstaller: []byte("uninstaller")})
	require.NoError(t, err)
	return Apply(context.Background(), p, f.payload, []byte("uninstaller"), nil)
}

func TestInstallThenUninstallLeavesTheHomeAsItWas(t *testing.T) {
	f := newFixture(t)
	before := f.snap(t)

	r, err := f.install(t)
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(f.root(), "bin", "hello"))
	require.NoError(t, err)
	require.Equal(t, "#!/bin/sh\necho hello\n", string(b))
	fi, err := os.Stat(filepath.Join(f.root(), "bin", "hello"))
	require.NoError(t, err)
	if posixModes() {
		require.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "the manifest's mode, since embed.FS keeps none")
	}
	_, err = os.Stat(f.data("fynstall", "installs", "io.example.hello.json"))
	require.NoError(t, err, "the index entry")

	rr, err := ReadReceipt(ReceiptPath(f.root()))
	require.NoError(t, err)
	require.Equal(t, r.Journal, rr.Journal)
	_, err = Uninstall(rr, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t))
}

func TestUninstallPutsBackAFileTheInstallReplaced(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.root(), "share", "doc"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(f.root(), "share", "doc", "README"), []byte("mine\n"), 0o600))
	before := f.snap(t)

	_, err := f.install(t)
	require.NoError(t, err)
	b, _ := os.ReadFile(filepath.Join(f.root(), "share", "doc", "README"))
	require.Equal(t, "hello\n", string(b))

	r, err := ReadReceipt(ReceiptPath(f.root()))
	require.NoError(t, err)
	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t), "the original file, with its mode, and the directories that held it")
}

func TestAFailedInstallUndoesItselfAndRestoresWhatItReplaced(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.root(), "bin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(f.root(), "bin", "hello"), []byte("old\n"), 0o700))
	// The second file's content no longer matches its hash, as if the
	// payload had been corrupted after the build.
	f.payload["share/doc/README"].Data = []byte("tampered\n")
	before := f.snap(t)

	_, err := f.install(t)
	require.ErrorContains(t, err, "sha256")
	require.Equal(t, before, f.snap(t))
}

func TestACancelledInstallUndoesItself(t *testing.T) {
	f := newFixture(t)
	before := f.snap(t)
	p, err := NewPlan(f.m, Options{Env: f.env})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Apply(ctx, p, f.payload, nil, nil)
	require.True(t, errors.Is(err, context.Canceled), "%v", err)
	require.Equal(t, before, f.snap(t))
}

func TestASecondInstallIsRefusedWhileTheFirstIsIndexed(t *testing.T) {
	f := newFixture(t)
	_, err := f.install(t)
	require.NoError(t, err)
	_, err = NewPlan(f.m, Options{Env: f.env})
	require.True(t, errors.Is(err, ErrInstalled), "%v", err)
	ix, _, err := ReadIndex(f.m, "user", f.env)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(f.root(), UninstallName), ix.Uninstaller)
}

func TestASymlinkInTheInstallDirectoryCannotCarryAWriteOutside(t *testing.T) {
	needsLinks(t)
	f := newFixture(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(f.root(), 0o750))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.root(), "bin")))
	_, err := NewPlan(f.m, Options{Env: f.env})
	require.ErrorContains(t, err, "outside the directory it belongs in")
}

func TestUninstallNeverTouchesAKeptPath(t *testing.T) {
	f := newFixture(t)
	f.m.KeepOnUninstall = []string{"{data}/{id}/share"}
	_, err := f.install(t)
	require.NoError(t, err)
	r, err := ReadReceipt(ReceiptPath(f.root()))
	require.NoError(t, err)
	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(f.root(), "share", "doc", "README"))
	require.NoError(t, err, "inside a kept path")
	_, err = os.Stat(filepath.Join(f.root(), "bin"))
	require.True(t, errors.Is(err, os.ErrNotExist), "outside it")
}

// withIntegration adds an icon, a desktop entry and a link to the fixture.
func (f *fixture) withIntegration() {
	icon := "not really a PNG"
	sum := sha256.Sum256([]byte(icon))
	ic := manifest.Icon{Size: 48, Bytes: int64(len(icon)), SHA256: hex.EncodeToString(sum[:])}
	f.payload[ic.Path()] = &fstest.MapFile{Data: []byte(icon)}
	f.m.Icons = []manifest.Icon{ic}
	f.m.Desktop = []manifest.Desktop{{ID: "io.example.hello", Name: "Hello", Exec: "bin/hello", Icon: true}}
	f.m.Links = []manifest.Link{{Name: "hello", Target: "bin/hello"}}
}

func (f *fixture) bin() string { return filepath.Join(f.home, ".local", "bin", "hello") }

func TestIntegrationGoesUnderDataAndBinAndComesOutAgain(t *testing.T) {
	needsIntegration(t)
	f := newFixture(t)
	f.withIntegration()
	before := f.snap(t)

	r, err := f.install(t)
	require.NoError(t, err)
	require.True(t, r.RefreshMenu)
	share := filepath.Join(f.home, ".local", "share")
	_, err = os.Stat(filepath.Join(share, "icons", "hicolor", "48x48", "apps", "io.example.hello.png"))
	require.NoError(t, err)
	entry, err := os.ReadFile(filepath.Join(share, "applications", "io.example.hello.desktop"))
	require.NoError(t, err)
	require.Contains(t, string(entry), "Exec="+filepath.Join(f.root(), "bin", "hello")+"\n")
	require.Contains(t, string(entry), "Icon=io.example.hello\n")
	target, err := os.Readlink(f.bin())
	require.NoError(t, err)
	require.Equal(t, filepath.Join(f.root(), "bin", "hello"), target)

	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t))
}

func TestALinkPutsBackTheFileOrLinkItReplaced(t *testing.T) {
	needsIntegration(t)
	needsIntegration(t)
	for name, prepare := range map[string]func(t *testing.T, path string){
		"a file": func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho mine\n"), 0o700))
		},
		"a link": func(t *testing.T, path string) {
			require.NoError(t, os.Symlink("/somewhere/else/hello", path))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.withIntegration()
			require.NoError(t, os.MkdirAll(filepath.Dir(f.bin()), 0o750))
			prepare(t, f.bin())
			before := f.snap(t)

			r, err := f.install(t)
			require.NoError(t, err)
			target, err := os.Readlink(f.bin())
			require.NoError(t, err)
			require.Equal(t, filepath.Join(f.root(), "bin", "hello"), target)

			_, err = Uninstall(r, ReasonUninstall, nil)
			require.NoError(t, err)
			require.Equal(t, before, f.snap(t), "the same kind of thing, with the same content or target")
		})
	}
}

func TestAnIconDirectoryLinkedOutsideDataIsRefused(t *testing.T) {
	needsIntegration(t)
	f := newFixture(t)
	f.withIntegration()
	share := filepath.Join(f.home, ".local", "share")
	require.NoError(t, os.MkdirAll(share, 0o750))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(share, "icons")))
	_, err := NewPlan(f.m, Options{Env: f.env})
	require.ErrorContains(t, err, "outside the directory it belongs in")
}

func TestAConfigFileIsRenderedFromParametersAndHoldsItsSecretPrivately(t *testing.T) {
	f := newFixture(t)
	f.m.KeepOnUninstall = nil
	f.m.Parameters = []manifest.Parameter{{Name: "server"}, {Name: "token", Secret: true}}
	f.m.ConfigFiles = []manifest.ConfigFile{{Path: "{config}/hello/config.yml", Format: "yaml",
		Values: map[string]string{"server": "{param:server}", "token": "{param:token}", "home": "{home}"}}}
	before := f.snap(t)
	p, err := NewPlan(f.m, Options{Env: f.env, Params: map[string]string{"server": "a: b # not a comment", "token": "s3cr3t"}})
	require.NoError(t, err)
	r, err := Apply(context.Background(), p, f.payload, nil, nil)
	require.NoError(t, err)

	path := f.config("hello", "config.yml")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var got map[string]string
	require.NoError(t, yaml.Unmarshal(b, &got))
	require.Equal(t, map[string]string{"home": f.home, "server": "a: b # not a comment", "token": "s3cr3t"}, got)
	require.True(t, strings.HasSuffix(string(b), "\nserver: 'a: b # not a comment'\ntoken: s3cr3t\n"), "sorted keys, quoted where YAML needs it: %s", b)
	fi, err := os.Stat(path)
	require.NoError(t, err)
	if posixModes() {
		require.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "it holds a secret")
	}

	require.Equal(t, map[string]string{"server": "a: b # not a comment"}, r.Parameters)
	require.Equal(t, []string{"token"}, r.Secrets)
	rb, err := os.ReadFile(ReceiptPath(f.root()))
	require.NoError(t, err)
	require.NotContains(t, string(rb), "s3cr3t")

	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	require.Equal(t, before, f.snap(t))
}

func TestRenderJSON(t *testing.T) {
	b, err := render("json", map[string]string{"b": `quote " and \ slash`, "a": "1"})
	require.NoError(t, err)
	require.Equal(t, "{\n  \"a\": \"1\",\n  \"b\": \"quote \\\" and \\\\ slash\"\n}\n", string(b))
}

// Configuration in a kept path survives the uninstall: that is how it
// survives an upgrade, which runs the old uninstaller first (spec 002 D3a).
func TestAConfigFileInAKeptPathSurvivesTheUninstall(t *testing.T) {
	f := newFixture(t) // keeps {config}/hello
	f.m.ConfigFiles = []manifest.ConfigFile{{Path: "{config}/hello/config.json", Format: "json", Values: map[string]string{"a": "b"}}}
	p, err := NewPlan(f.m, Options{Env: f.env})
	require.NoError(t, err)
	r, err := Apply(context.Background(), p, f.payload, nil, nil)
	require.NoError(t, err)
	_, err = Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	b, err := os.ReadFile(f.config("hello", "config.json"))
	require.NoError(t, err)
	require.Equal(t, "{\n  \"a\": \"b\"\n}\n", string(b))
	_, err = os.Stat(f.root())
	require.True(t, errors.Is(err, os.ErrNotExist), "everything else is gone")
}

func TestProgressCountsEveryFileAndByteAndMovesInsideALargeFile(t *testing.T) {
	f := newFixture(t)
	f.add("share/big.bin", strings.Repeat("x", 3*progressStep+5), 0o644)
	p, err := NewPlan(f.m, Options{Env: f.env, Uninstaller: []byte("uninstaller")})
	require.NoError(t, err)
	var events []Event
	_, err = Apply(context.Background(), p, f.payload, []byte("uninstaller"), func(ev Event) {
		if ev.Kind == Progress {
			events = append(events, ev)
		}
	})
	require.NoError(t, err)

	var total int64
	for _, pf := range p.Files {
		total += pf.Size
	}
	last := events[len(events)-1].Counts
	require.Equal(t, Counts{Files: len(p.Files), FilesTotal: len(p.Files), Bytes: total, BytesTotal: total}, last)
	inside := 0
	for _, ev := range events {
		if strings.HasSuffix(ev.Text, "big.bin") && ev.Counts.Files < 3 {
			inside++
		}
	}
	require.GreaterOrEqual(t, inside, 3, "the bar moves while a large file is copied")
}

func (f *fixture) uninstall(t *testing.T) (*Receipt, []Leftover) {
	t.Helper()
	r, err := ReadReceipt(ReceiptPath(f.root()))
	require.NoError(t, err)
	left, err := Uninstall(r, ReasonUninstall, nil)
	require.NoError(t, err)
	return r, left
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestAPayloadLinkIsInstalledAsALinkAndRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows payload has no links: the builder copies what they point at (spec 002 D4a)")
	}
	f := newFixture(t)
	f.m.Symlinks = []manifest.Symlink{
		{Path: "share/doc/README.txt", Target: "README"},
		{Path: "share/docs", Target: "doc"},
		{Path: "bin/readme", Target: "../share/docs/README.txt"},
	}
	before := f.snap(t)
	_, err := f.install(t)
	require.NoError(t, err)
	target, err := os.Readlink(filepath.Join(f.root(), "bin", "readme"))
	require.NoError(t, err)
	require.Equal(t, "../share/docs/README.txt", target, "as written, not resolved")
	b, err := os.ReadFile(filepath.Join(f.root(), "bin", "readme"))
	require.NoError(t, err)
	require.Equal(t, "hello\n", string(b), "a chain through a directory link reaches the file")

	_, left := f.uninstall(t)
	require.Empty(t, left)
	require.Equal(t, before, f.snap(t))
}

func TestAPayloadLinkPutsBackWhatWasAtItsPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows payload has no links: the builder copies what they point at (spec 002 D4a)")
	}
	f := newFixture(t)
	f.m.Symlinks = []manifest.Symlink{{Path: "share/a", Target: "doc"}, {Path: "share/b", Target: "doc/README"}}
	write(t, filepath.Join(f.root(), "share", "a"), "a file was here")
	require.NoError(t, os.Symlink("elsewhere", filepath.Join(f.root(), "share", "b")))
	before := f.snap(t)
	_, err := f.install(t)
	require.NoError(t, err)
	target, err := os.Readlink(filepath.Join(f.root(), "share", "a"))
	require.NoError(t, err)
	require.Equal(t, "doc", target)
	f.uninstall(t)
	require.Equal(t, before, f.snap(t), "the file and the old link are back")
}

func TestAPayloadLinkThatLeavesTheInstallDirectoryIsRefused(t *testing.T) {
	absolute := "/etc/passwd"
	if runtime.GOOS == "windows" {
		absolute = `C:\Windows\win.ini`
	}
	for _, target := range []string{"../../../../etc/passwd", absolute} {
		f := newFixture(t)
		f.m.Symlinks = []manifest.Symlink{{Path: "share/x", Target: target}}
		_, err := NewPlan(f.m, Options{Env: f.env})
		require.ErrorContains(t, err, filepath.Join(f.root(), "share", "x")+" links to "+target, target)
	}
}

// The program writes a bytecode cache, and links one entry of it outside
// the install. The patterns remove the cache and the link, never what the
// link points at.
func TestUninstallRemoveDeletesWhatItsPatternsMatchAndNothingElse(t *testing.T) {
	needsLinks(t)
	f := newFixture(t)
	f.m.UninstallRemove = []string{"share/**/__pycache__", "bin/*.log"}
	before := f.snap(t)
	outside := filepath.Join(t.TempDir(), "precious")
	write(t, outside, "not the install's")
	_, err := f.install(t)
	require.NoError(t, err)
	write(t, filepath.Join(f.root(), "share", "__pycache__", "a.pyc"), "a")
	write(t, filepath.Join(f.root(), "share", "doc", "__pycache__", "sub", "b.pyc"), "b")
	write(t, filepath.Join(f.root(), "bin", "run.log"), "log")
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(f.root(), "share", "doc", "__pycache__", "out")))

	_, left := f.uninstall(t)
	require.Empty(t, left)
	require.Equal(t, before, f.snap(t))
	b, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "not the install's", string(b), "a link is removed, not followed")
}

func TestLeftoversAreListedNotDeletedUntilAskedFor(t *testing.T) {
	needsLinks(t)
	f := newFixture(t)
	before := f.snap(t)
	_, err := f.install(t)
	require.NoError(t, err)
	write(t, filepath.Join(f.root(), "bin", "cache.db"), "c")
	write(t, filepath.Join(f.root(), "share", "new", "x"), "x")
	require.NoError(t, os.Symlink("/", filepath.Join(f.root(), "share", "new", "root")))

	r, left := f.uninstall(t)
	share := filepath.Join(f.root(), "share")
	require.Equal(t, []Leftover{
		{Path: filepath.Join(f.root(), "bin", "cache.db")},
		{Path: filepath.Join(share, "new"), Dir: true},
		{Path: filepath.Join(share, "new", "root")},
		{Path: filepath.Join(share, "new", "x")},
	}, left)
	require.Equal(t, 3, LeftoverFiles(left))
	_, err = os.Stat(filepath.Join(share, "new", "x"))
	require.NoError(t, err, "listed, not deleted")
	_, err = os.Stat(filepath.Join(share, "doc"))
	require.True(t, errors.Is(err, os.ErrNotExist), "an emptied directory is still removed")

	require.NoError(t, RemoveLeftovers(r, left, nil))
	require.Equal(t, before, f.snap(t))
}

// A directory the install found already there holds the person's own
// files: nothing in it is a leftover, and RemoveLeftovers refuses it.
func TestADirectoryThatWasThereBeforeHoldsNoLeftovers(t *testing.T) {
	f := newFixture(t)
	f.m.UninstallRemove = []string{"**"}
	mine := filepath.Join(f.root(), "mine.txt")
	write(t, mine, "mine")
	_, err := f.install(t)
	require.NoError(t, err)
	write(t, filepath.Join(f.root(), "bin", "cache.db"), "c")

	r, left := f.uninstall(t)
	require.Empty(t, left, "bin/cache.db matched ** and was removed; mine.txt is not in a directory the install created")
	_, err = os.Stat(mine)
	require.NoError(t, err)
	err = RemoveLeftovers(r, []Leftover{{Path: mine}}, nil)
	require.ErrorContains(t, err, "is not a leftover of Hello")
	_, err = os.Stat(mine)
	require.NoError(t, err)
}

func TestAKeptPathIsNeitherALeftoverNorRemovedByAPattern(t *testing.T) {
	f := newFixture(t)
	f.m.KeepOnUninstall = []string{"{data}/{id}/share/state"}
	f.m.UninstallRemove = []string{"share/**"}
	_, err := f.install(t)
	require.NoError(t, err)
	state := filepath.Join(f.root(), "share", "state", "db")
	write(t, state, "s")
	_, left := f.uninstall(t)
	require.Empty(t, left)
	_, err = os.Stat(state)
	require.NoError(t, err)
}

func TestTheLockRefusesASecondHolderUntilReleased(t *testing.T) {
	dir := t.TempDir()
	env := func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": dir}[k] }
	unlock, err := Lock("io.example.hello", "user", env)
	require.NoError(t, err)
	_, err = Lock("io.example.hello", "user", env)
	require.ErrorIs(t, err, ErrLocked)
	other, err := Lock("io.example.other", "user", env)
	require.NoError(t, err, "another app has its own lock")
	other()
	unlock()
	again, err := Lock("io.example.hello", "user", env)
	require.NoError(t, err)
	again()
}

func TestAMigrateWhoseTwoEndsExistIsRefused(t *testing.T) {
	f := newFixture(t)
	f.m.Actions = []manifest.Action{{Migrate: &manifest.Migrate{From: "{data}/old", To: "{config}/hello/data"}}}
	write(t, f.data("old", "a"), "a")
	write(t, f.config("hello", "data", "b"), "b")
	_, err := NewPlan(f.m, Options{Env: f.env})
	require.ErrorContains(t, err, "both")
}

// A run action that fails carries its last output lines, and the install
// it was part of is undone, its own undo included.
func TestAFailingRunCarriesItsOutputAndIsUndone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the program it runs is a shell script")
	}
	f := newFixture(t)
	marker := filepath.Join(f.home, "undone")
	f.add("bin/setup", "#!/bin/sh\nif [ \"$1\" = undo ]; then : > \""+marker+"\"; exit 0; fi\necho one; echo two >&2; exit 3\n", 0o755)
	f.m.Actions = []manifest.Action{{Run: &manifest.Run{Exec: "bin/setup", Undo: []string{"undo"}}}}
	before := f.snap(t)
	_, err := f.install(t)
	require.ErrorContains(t, err, "setup: exit status 3: one / two")
	_, statErr := os.Stat(marker)
	require.NoError(t, statErr, "the failed run's undo ran")
	require.NoError(t, os.Remove(marker))
	require.Equal(t, before, f.snap(t))
}
