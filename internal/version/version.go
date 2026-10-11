// Package version holds the fynstall version, stamped by the Makefile.
//
// The builder's version is also the runtime version every installer it
// builds is pinned to (spec 001, R5), so a build that is not a release says
// so: `0.1.0-1a2b3c4-dev`.
package version

import (
	"regexp"
	"runtime/debug"
)

// Version is set with -ldflags "-X github.com/ushineko/fynstall/internal/version.Version=...".
var Version = unstamped

// unstamped is the version of a build that nothing stamped.
const unstamped = "dev"

// module is the module a builder is the main program of.
const module = "github.com/ushineko/fynstall"

/*
A builder made with `go install module/cmd/fynstall@v0.1.1` is not stamped
by the Makefile, but the Go toolchain records which version of the module
it built. That version is the release, so the builder takes it (#36). The
stamp wins when there is one.
*/
func init() {
	if Version != unstamped {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v, ok := release(bi.Main.Path, bi.Main.Version); ok {
			Version = v
		}
	}
}

// A release is vX.Y.Z and nothing more.
var releaseTag = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

/*
release returns the fynstall release that a program's build information
names, without the v. There is none when the main module is another one,
as in an installer, whose version the builder stamps; when the program was
built in a checkout that is not on a tag ("(devel)", or a version that ends
in a commit); or when the checkout was changed ("+dirty"). Such a version
is not one that an installer can be pinned to and be the same for everybody.
*/
func release(path, v string) (string, bool) {
	if path != module {
		return "", false
	}
	m := releaseTag.FindStringSubmatch(v)
	if m == nil {
		return "", false
	}
	return m[1], true
}
