//go:build !windows

// Package regtest gives a test a registry key of its own on Windows. Here,
// where there is no registry, it does nothing.
package regtest

import "testing"

// Env is the variable that moves every registry key under a root.
const Env = "FYNSTALL_TEST_REGISTRY_ROOT"

// Root is "": there is no registry.
func Root(*testing.T) string { return "" }

// Snapshot is empty: there is no registry.
func Snapshot(*testing.T, string) map[string]string { return map[string]string{} }

// Get finds nothing: there is no registry.
func Get(*testing.T, string, string, string) (string, bool) { return "", false }
