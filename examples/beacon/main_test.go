package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupThenTeardownLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "beacon", "setup-done")
	var out bytes.Buffer
	require.NoError(t, run([]string{"setup", file}, &out))
	require.FileExists(t, file)
	require.NoError(t, run([]string{"teardown", file}, &out))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestSetupFailsWhenAskedTo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fail-setup"), nil, 0o600))
	require.ErrorContains(t, run([]string{"setup", filepath.Join(dir, "x")}, &bytes.Buffer{}), "setup failed")
}

func TestGoodbyeFailsWhenAskedTo(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	require.NoError(t, run([]string{"goodbye"}, &out))
	require.Contains(t, out.String(), "the uninstall hook ran")
	require.NoError(t, os.WriteFile("fail-goodbye", nil, 0o600))
	require.ErrorContains(t, run([]string{"goodbye"}, &out), "goodbye failed")
}
