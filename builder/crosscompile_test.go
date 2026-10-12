package builder

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// host is the machine running the test, which is the one target that needs no
// cross compiler.
func host() string { return runtime.GOOS + "/" + runtime.GOARCH }

/*
The machine's own target needs no compiler of its own.

The host build is what every caller does, and giving it a CC would override a
toolchain somebody chose for reasons this builder cannot see.
*/
func TestTheHostTargetNeedsNoCrossCompiler(t *testing.T) {
	cc, cxx, needed := crossCompiler(host(), nil)
	if needed || cc != "" || cxx != "" {
		t.Errorf("crossCompiler(host) = %q, %q, %v", cc, cxx, needed)
	}
	if err := checkCrossCompiler(host(), nil); err != nil {
		t.Errorf("the host target was refused: %v", err)
	}
}

/*
An explicit CC wins over the default.

A caller who set CC has a toolchain in mind — a different mingw, a wrapper, a
compiler in a container — and a builder that silently replaced it would break
a build that worked yesterday for no reason it could explain.
*/
func TestAnExplicitCompilerWins(t *testing.T) {
	other := "windows/amd64"
	if host() == other {
		other = "linux/arm64"
	}
	cc, cxx, needed := crossCompiler(other, []string{"CC=my-own-gcc", "CXX=my-own-g++"})
	if !needed {
		t.Fatal("a cross target reported needing no compiler")
	}
	if cc != "my-own-gcc" || cxx != "my-own-g++" {
		t.Errorf("compilers = %q, %q", cc, cxx)
	}
}

// The last assignment wins, as it does when a process reads its environment.
func TestTheLastAssignmentWins(t *testing.T) {
	if got := valueOf("CC", []string{"CC=first", "CC=second"}); got != "second" {
		t.Errorf("CC = %q", got)
	}
	if got := valueOf("CC", []string{"CXX=other"}); got != "" {
		t.Errorf("an absent name gave %q", got)
	}
}

// Each target this builder knows has a default compiler, so the usual case
// needs no CC at all.
func TestEveryKnownTargetHasADefaultCompiler(t *testing.T) {
	for _, target := range []string{"windows/amd64", "linux/amd64", "linux/arm64"} {
		if cc, cxx := defaultCompilers(target); cc == "" || cxx == "" {
			t.Errorf("%s has no default compiler: %q, %q", target, cc, cxx)
		}
	}
}

/*
A missing compiler is refused by name, with the package to install.

The message is the reason this check exists at all. The toolchain's own
failure is "exec: x86_64-w64-mingw32-gcc: not found", which leaves somebody
to work out what that program is and which package carries it.
*/
func TestAMissingCompilerIsRefusedByName(t *testing.T) {
	other := "windows/amd64"
	if host() == other {
		other = "linux/arm64"
	}
	err := checkCrossCompiler(other, []string{"CC=a-compiler-that-is-not-installed"})
	if err == nil {
		t.Fatal("a missing compiler was accepted")
	}
	for _, want := range []string{"a-compiler-that-is-not-installed", "--cli-only", other} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

/*
A compiler that is there is accepted.

Written against a program this test makes, rather than against whatever the
machine happens to have installed: a test that passed only on a machine with
mingw would be a test that reports on the machine.
*/
func TestACompilerOnPathIsAccepted(t *testing.T) {
	dir := t.TempDir()
	cc := filepath.Join(dir, "pretend-gcc")
	if err := os.WriteFile(cc, []byte("#!/bin/sh\nexit 0\n"), 0o750); err != nil { //nolint:gosec // a test's own stub
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	other := "windows/amd64"
	if host() == other {
		other = "linux/arm64"
	}
	if err := checkCrossCompiler(other, []string{"CC=pretend-gcc"}); err != nil {
		t.Errorf("a compiler on PATH was refused: %v", err)
	}
}

/*
A target with no default and no CC is refused, rather than built without one.

Going ahead would hand the toolchain an empty CC, and the failure a person
would see is a linker error from a build that should never have started.
*/
func TestAnUnknownTargetWithNoCompilerIsRefused(t *testing.T) {
	err := checkCrossCompiler("plan9/386", nil)
	if err == nil {
		t.Fatal("a target with no compiler was accepted")
	}
	if !strings.Contains(err.Error(), "set CC") {
		t.Errorf("the refusal does not say what to do: %v", err)
	}
}
