//go:build !windows

package installer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasDisplay(t *testing.T) {
	require.True(t, hasDisplay(func(k string) string { return map[string]string{"WAYLAND_DISPLAY": "wayland-0"}[k] }))
	require.True(t, hasDisplay(func(k string) string { return map[string]string{"DISPLAY": ":0"}[k] }))
	require.False(t, hasDisplay(func(string) string { return "" }))
}
