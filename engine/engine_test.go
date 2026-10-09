package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/internal/snapshot"
	"github.com/ushineko/fynstall/manifest"
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
		if k == "HOME" {
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

func (f *fixture) root() string { return filepath.Join(f.home, ".local", "share", "io.example.hello") }

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
	require.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "the manifest's mode, since embed.FS keeps none")
	_, err = os.Stat(filepath.Join(f.home, ".local", "share", "fynstall", "installs", "io.example.hello.json"))
	require.NoError(t, err, "the index entry")

	rr, err := ReadReceipt(ReceiptPath(f.root()))
	require.NoError(t, err)
	require.Equal(t, r.Journal, rr.Journal)
	require.NoError(t, Uninstall(rr, nil))
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
	require.NoError(t, Uninstall(r, nil))
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
	require.NoError(t, Uninstall(r, nil))
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

	require.NoError(t, Uninstall(r, nil))
	require.Equal(t, before, f.snap(t))
}

func TestALinkPutsBackTheFileOrLinkItReplaced(t *testing.T) {
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

			require.NoError(t, Uninstall(r, nil))
			require.Equal(t, before, f.snap(t), "the same kind of thing, with the same content or target")
		})
	}
}

func TestAnIconDirectoryLinkedOutsideDataIsRefused(t *testing.T) {
	f := newFixture(t)
	f.withIntegration()
	share := filepath.Join(f.home, ".local", "share")
	require.NoError(t, os.MkdirAll(share, 0o750))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(share, "icons")))
	_, err := NewPlan(f.m, Options{Env: f.env})
	require.ErrorContains(t, err, "outside the directory it belongs in")
}
