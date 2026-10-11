package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/config"
	"github.com/ushineko/fynstall/manifest"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return dir
}

func cfg(dir string, entries ...config.Entry) *config.Config {
	return &config.Config{
		Dir:     dir,
		App:     config.App{ID: "io.example.hello", Name: "Hello", Version: "0.1.0"},
		Install: config.Install{Scopes: []string{"user"}, Dir: map[string]string{"user": "{data}/{id}"}},
		Payload: entries,
	}
}

func TestTheSameTreeGivesTheSameManifestBytes(t *testing.T) {
	dir := tree(t, map[string]string{
		"share/z.txt": "z", "share/a.txt": "a", "share/sub/run.sh": "#!/bin/sh\n", "share/x.tmp": "x", "bin/hello": "\x7fELF",
	})
	c := cfg(dir,
		config.Entry{Src: "share", Dst: "share", Exclude: []string{"*.tmp"}},
		config.Entry{Src: "bin/hello", Dst: "bin/"},
	)
	m1, _, _, err := stage(c, "test", "linux/amd64")
	require.NoError(t, err)
	b1, err := m1.Marshal()
	require.NoError(t, err)
	m2, _, _, err := stage(c, "test", "linux/amd64")
	require.NoError(t, err)
	b2, err := m2.Marshal()
	require.NoError(t, err)
	require.Equal(t, b1, b2)

	var paths []string
	modes := map[string]uint32{}
	for _, f := range m1.Files {
		paths = append(paths, f.Path)
		modes[f.Path] = f.Mode
	}
	require.Equal(t, []string{"bin/hello", "share/a.txt", "share/sub/run.sh", "share/z.txt"}, paths, "sorted, *.tmp excluded")
	require.Equal(t, uint32(0o755), modes["bin/hello"])
	require.Equal(t, uint32(0o755), modes["share/sub/run.sh"])
	require.Equal(t, uint32(0o644), modes["share/a.txt"])
}

func TestAConfigModeOverridesDetection(t *testing.T) {
	dir := tree(t, map[string]string{"bin/tool": "data"})
	m, _, _, err := stage(cfg(dir, config.Entry{Src: "bin/tool", Dst: "bin/tool", Mode: "0750"}), "test", "linux/amd64")
	require.NoError(t, err)
	require.Equal(t, uint32(0o750), m.Files[0].Mode)
}

func TestTwoSourcesForOneDestinationIsAnError(t *testing.T) {
	dir := tree(t, map[string]string{"a": "1", "b": "2"})
	_, _, _, err := stage(cfg(dir, config.Entry{Src: "a", Dst: "x"}, config.Entry{Src: "b", Dst: "x"}), "test", "linux/amd64")
	require.ErrorContains(t, err, "both install to x")
}

// links builds a runtime-shaped tree: a library with its version links, a
// directory link, and a program linked from bin.
func links(t *testing.T) string {
	t.Helper()
	dir := tree(t, map[string]string{
		"rt/lib/libffi.so.8.4.0": "ELF ffi", "rt/lib/python/os.py": "# os", "rt/bin/python3.14": "\x7fELF py",
	})
	for link, target := range map[string]string{
		"rt/lib/libffi.so.8": "libffi.so.8.4.0", "rt/lib/libffi.so": "libffi.so.8",
		"rt/lib64": "lib", "rt/bin/python3": "python3.14", "rt/bin/os.py": "../lib/python/os.py",
	} {
		require.NoError(t, os.Symlink(target, filepath.Join(dir, filepath.FromSlash(link))))
	}
	return dir
}

func TestALinkInsideItsEntryIsKeptAsALink(t *testing.T) {
	m, files, _, err := stage(cfg(links(t), config.Entry{Src: "rt", Dst: "python"}), "test", "linux/amd64")
	require.NoError(t, err)
	require.Equal(t, []manifest.Symlink{
		{Path: "python/bin/os.py", Target: "../lib/python/os.py"},
		{Path: "python/bin/python3", Target: "python3.14"},
		{Path: "python/lib/libffi.so", Target: "libffi.so.8"},
		{Path: "python/lib/libffi.so.8", Target: "libffi.so.8.4.0"},
		{Path: "python/lib64", Target: "lib"},
	}, m.Symlinks, "as written, sorted by path")
	require.Len(t, files, 3, "the links are not files")
}

func TestAWindowsTargetGetsACopyOfWhatALinkPointsAt(t *testing.T) {
	m, files, _, err := stage(cfg(links(t), config.Entry{Src: "rt", Dst: "python"}), "test", "windows/amd64")
	require.NoError(t, err)
	require.Empty(t, m.Symlinks)
	sums := map[string]string{}
	for _, f := range files {
		sums[f.Path] = f.SHA256
	}
	require.Len(t, sums, 11, "3 files, 4 file links, and the 4 files and links under lib64")
	require.Equal(t, sums["python/lib/libffi.so.8.4.0"], sums["python/lib/libffi.so"], "a chain resolves to the file at its end")
	require.Equal(t, sums["python/lib/python/os.py"], sums["python/lib64/python/os.py"], "a directory link is a copied tree")
	require.Equal(t, sums["python/lib/libffi.so.8.4.0"], sums["python/lib64/libffi.so.8"], "a link inside a copied tree is a copy too")
	require.Equal(t, sums["python/bin/python3.14"], sums["python/bin/python3"])
}

func TestALinkThatLeavesItsEntryIsRefused(t *testing.T) {
	for name, c := range map[string]struct{ link, target, want string }{
		"absolute":     {"share/abs", "/etc/hostname", "an absolute path"},
		"outside":      {"share/out", "../secret", "outside the payload entry"},
		"dangling":     {"share/gone", "nothing", "does not exist"},
		"excluded":     {"share/tmp", "x.tmp", "the entry excludes"},
		"own ancestor": {"share/sub/up", "..", "holds the directory link"},
		"deep":         {"share/sub/deep/esc", "../../../secret", "outside the payload entry"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := tree(t, map[string]string{"share/a": "1", "share/x.tmp": "x", "share/sub/deep/f": "f", "secret": "s"})
			require.NoError(t, os.Symlink(c.target, filepath.Join(dir, filepath.FromSlash(c.link))))
			for _, target := range []string{"linux/amd64", "windows/amd64"} {
				_, _, _, err := stage(cfg(dir, config.Entry{Src: "share", Dst: "share", Exclude: []string{"*.tmp"}}), "test", target)
				require.ErrorContains(t, err, c.want, "%s refuses it as the other targets do", target)
				require.ErrorContains(t, err, filepath.Join(dir, filepath.FromSlash(c.link)), "the error names the link")
				require.Equal(t, 1, strings.Count(err.Error(), "payload:"), "one prefix: %v", err)
			}
		})
	}
}

// A target that stays inside as written can still leave through a link on
// its way: the link is checked by where it resolves, not by its text.
func TestALinkThatLeavesThroughAnotherLinkIsRefused(t *testing.T) {
	dir := tree(t, map[string]string{"share/t/f": "f", "share/d1/d2/g": "g"})
	require.NoError(t, os.Symlink("../../t", filepath.Join(dir, "share", "d1", "d2", "x")))
	require.NoError(t, os.Symlink("x/../../secret", filepath.Join(dir, "share", "d1", "d2", "l")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret"), []byte("s"), 0o600))
	_, _, _, err := stage(cfg(dir, config.Entry{Src: "share", Dst: "share"}), "test", "linux/amd64")
	// Windows keeps the target with its own separator.
	require.ErrorContains(t, err, filepath.FromSlash("x/../../secret")+", which is outside the payload entry")
}

func TestAFileUnderAPayloadLinkIsRefused(t *testing.T) {
	dir := links(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra"), []byte("e"), 0o600))
	_, _, _, err := stage(cfg(dir, config.Entry{Src: "rt", Dst: "python"}, config.Entry{Src: "extra", Dst: "python/lib64/extra"}), "test", "linux/amd64")
	require.ErrorContains(t, err, "python/lib64/extra installs under the link python/lib64")
}

func TestASingleFileEntryThatIsALinkIsRefused(t *testing.T) {
	dir := links(t)
	_, _, _, err := stage(cfg(dir, config.Entry{Src: "rt/bin/python3", Dst: "bin/"}), "test", "linux/amd64")
	require.ErrorContains(t, err, "links are kept only inside a directory entry")
}

func TestOneConfigStagesADifferentPayloadPerTarget(t *testing.T) {
	dir := tree(t, map[string]string{
		"build/linux-amd64/greet":       "\x7fELF amd64",
		"build/linux-arm64/greet":       "\x7fELF arm64",
		"build/windows-amd64/greet.exe": "MZ amd64",
		"notes/arm64.txt":               "arm64 only",
	})
	c := cfg(dir,
		config.Entry{Src: "build/{os}-{arch}/greet{exe}", Dst: "bin/greet{exe}"},
		config.Entry{Src: "notes/arm64.txt", Dst: "share/arm64.txt", Targets: []string{"*/arm64"}},
	)
	c.Integration.PathLinks = []string{"bin/greet{exe}"}

	files := func(target string) map[string]string {
		m, staged, _, err := stage(c, "test", target)
		require.NoError(t, err)
		require.Equal(t, target, m.Target)
		out := map[string]string{}
		for _, s := range staged {
			out[s.Path] = filepath.ToSlash(s.src[len(dir)+1:])
		}
		require.Equal(t, "greet"+map[bool]string{true: ".exe"}[target == "windows/amd64"], m.Links[0].Name)
		return out
	}
	require.Equal(t, map[string]string{"bin/greet": "build/linux-amd64/greet"}, files("linux/amd64"))
	require.Equal(t, map[string]string{"bin/greet": "build/linux-arm64/greet", "share/arm64.txt": "notes/arm64.txt"}, files("linux/arm64"))
	require.Equal(t, map[string]string{"bin/greet.exe": "build/windows-amd64/greet.exe"}, files("windows/amd64"))
}
