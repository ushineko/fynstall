package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/config"
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
	m1, _, err := stage(c, "test")
	require.NoError(t, err)
	b1, err := m1.Marshal()
	require.NoError(t, err)
	m2, _, err := stage(c, "test")
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
	m, _, err := stage(cfg(dir, config.Entry{Src: "bin/tool", Dst: "bin/tool", Mode: "0750"}), "test")
	require.NoError(t, err)
	require.Equal(t, uint32(0o750), m.Files[0].Mode)
}

func TestTwoSourcesForOneDestinationIsAnError(t *testing.T) {
	dir := tree(t, map[string]string{"a": "1", "b": "2"})
	_, _, err := stage(cfg(dir, config.Entry{Src: "a", Dst: "x"}, config.Entry{Src: "b", Dst: "x"}), "test")
	require.ErrorContains(t, err, "both install to x")
}

func TestSymlinksInThePayloadAreRefused(t *testing.T) {
	dir := tree(t, map[string]string{"share/a": "1"})
	require.NoError(t, os.Symlink("a", filepath.Join(dir, "share", "link")))
	_, _, err := stage(cfg(dir, config.Entry{Src: "share", Dst: "share"}), "test")
	require.ErrorContains(t, err, "symlinks are not followed")
}
