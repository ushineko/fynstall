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
