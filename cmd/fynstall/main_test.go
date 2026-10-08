package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionPrintsTheStampedVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run([]string{"version"}, &out, &errOut))
	require.Equal(t, "fynstall dev\n", out.String())
	require.Empty(t, errOut.String())
}

func TestUsageErrorsExitTwoAndWriteToStderr(t *testing.T) {
	for _, args := range [][]string{nil, {"frobnicate"}} {
		var out, errOut bytes.Buffer
		require.Equal(t, 2, run(args, &out, &errOut), "%q", args)
		require.Empty(t, out.String(), "%q", args)
		require.Contains(t, errOut.String(), "Usage: fynstall", "%q", args)
	}
}
