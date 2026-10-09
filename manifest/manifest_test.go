package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectModeMarksProgramsExecutable(t *testing.T) {
	self, err := os.Executable() // an ELF file on Linux
	require.NoError(t, err)
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}
	for path, want := range map[string]uint32{
		self:                           0o755,
		write("run.sh", "#!/bin/sh\n"): 0o755,
		write("app.exe", "MZ\x90\x00"): 0o755,
		write("README", "hello\n"):     0o644,
		write("empty", ""):             0o644,
		write("short", "#"):            0o644,
	} {
		got, err := DetectMode(path)
		require.NoError(t, err)
		require.Equal(t, want, got, path)
	}
}

func TestExpandNeedsAValueForEveryPlaceholder(t *testing.T) {
	got, err := Expand("{data}/{id}", map[string]string{"data": "/d", "id": "io.x.y"})
	require.NoError(t, err)
	require.Equal(t, "/d/io.x.y", got)
	_, err = Expand("{data}/{id}", map[string]string{"data": "/d"})
	require.ErrorContains(t, err, "{id}")
	require.Equal(t, []string{"datum"}, Unknown("{datum}/{id}"))
}

func TestParseRefusesAnotherSchema(t *testing.T) {
	_, err := Parse([]byte(`{"schema": 2}`))
	require.ErrorContains(t, err, "schema 2")
	_, err = Parse([]byte(`{"schema": 1, "surprise": true}`))
	require.Error(t, err)
}

func TestBasename(t *testing.T) {
	require.Equal(t, "hello", App{ID: "io.ushineko.hello"}.Basename())
}

func TestAPatternMatchesAnyDepthWithDoubleStar(t *testing.T) {
	for _, c := range []struct {
		pat, rel string
		want     bool
	}{
		{"python/**/__pycache__", "python/__pycache__", true},
		{"python/**/__pycache__", "python/lib/x/__pycache__", true},
		{"python/**/__pycache__", "python/lib/__pycache__/a.pyc", false},
		{"python/**/__pycache__", "other/__pycache__", false},
		{"**/*.log", "a.log", true},
		{"**/*.log", "logs/b/a.log", true},
		{"cache/*", "cache/x", true},
		{"cache/*", "cache/x/y", false},
		{"**", "anything/at/all", true},
	} {
		require.Equal(t, c.want, MatchPattern(c.pat, c.rel), "%s ~ %s", c.pat, c.rel)
	}
}

func TestAPatternCannotReachOutsideTheInstallDirectory(t *testing.T) {
	require.Empty(t, CheckPattern("python/**/__pycache__"))
	for p, want := range map[string]string{
		"../x":      "outside",
		"a/../../b": "outside",
		"/etc/x":    "absolute",
		`C:\x`:      "absolute",
		"a//b":      "empty",
		"a/**b":     "whole segment",
		"[":         "bad pattern",
		"  ":        "empty",
	} {
		require.Contains(t, CheckPattern(p), want, p)
	}
}
