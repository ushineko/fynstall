package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGreetNamesItsTarget(t *testing.T) {
	var b bytes.Buffer
	greet(&b, filepath.Join(t.TempDir(), "none.json"))
	require.Equal(t, "greet from "+runtime.GOOS+"/"+runtime.GOARCH+"\n", b.String())
}

func TestGreetReadsItsConfigAndNeverPrintsTheToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"greeting":"hi","name":"Ada","token":"s3cr3t"}`), 0o600))
	var b bytes.Buffer
	greet(&b, p)
	require.Equal(t, "hi from "+runtime.GOOS+"/"+runtime.GOARCH+", Ada (with a token)\n", b.String())
}
