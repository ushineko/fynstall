package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionPrintsTheStampedVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run(context.Background(), []string{"version"}, &out, &errOut))
	require.Equal(t, "fynstall dev\n", out.String())
	require.Empty(t, errOut.String())
}

func TestUsageErrorsExitTwoAndWriteToStderr(t *testing.T) {
	for _, args := range [][]string{nil, {"frobnicate"}} {
		var out, errOut bytes.Buffer
		require.Equal(t, 2, run(context.Background(), args, &out, &errOut), "%q", args)
		require.Empty(t, out.String(), "%q", args)
		require.Contains(t, errOut.String(), "Usage: fynstall", "%q", args)
	}
}

func TestInitWritesAConfigAndRefusesToOverwriteIt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fynstall.yaml")
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run(context.Background(), []string{"init", "-c", p}, &out, &errOut), errOut.String())
	require.Equal(t, 1, run(context.Background(), []string{"init", "-c", p}, &out, &errOut))

	// The template is valid apart from its placeholder payload, which does
	// not exist in an empty directory.
	errOut.Reset()
	require.Equal(t, 1, run(context.Background(), []string{"validate", "-c", p}, &out, &errOut))
	require.Contains(t, errOut.String(), "payload[0].src: bin/myapp does not exist")
	require.Equal(t, 1, strings.Count(strings.TrimSpace(errOut.String()), "\n")+1, errOut.String())
}

/*
A cross-built wizard is refused only when there is no compiler for it.

The rule used to be that a full installer built for this machine and nothing
else. That was a guess about cgo rather than a fact about it: with a compiler
that targets the other machine, the build goes ahead, which is what
builder/crosscompile.go is for. What is still refused is a build that would
hand the toolchain a compiler that is not there, and the refusal names it.
*/
func TestAFullBuildForAnotherTargetNeedsACompilerForIt(t *testing.T) {
	other := "linux/arm64"
	if runtime.GOARCH == "arm64" {
		other = "linux/amd64"
	}
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{
		"build", "-c", "../../examples/hello/fynstall.yaml", "-o", t.TempDir(),
		"--target", other,
	}, &out, &errOut)
	if code == 0 {
		// This machine has a cross compiler for that target, so the build was
		// the right answer. Nothing to assert about a refusal that did not
		// happen.
		return
	}
	require.Equal(t, 1, code)
	require.Contains(t, errOut.String(), "the wizard needs cgo")
	require.Contains(t, errOut.String(), "--cli-only")
	// It names the compiler it looked for, which is the half the toolchain's
	// own "not found" leaves somebody to work out.
	require.Contains(t, errOut.String(), "gcc")
}
