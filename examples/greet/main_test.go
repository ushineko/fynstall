package main

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGreetNamesItsTarget(t *testing.T) {
	var b bytes.Buffer
	greet(&b)
	require.Equal(t, "greet from "+runtime.GOOS+"/"+runtime.GOARCH+"\n", b.String())
}
