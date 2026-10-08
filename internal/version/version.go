// Package version holds the fynstall version, stamped by the Makefile.
//
// The builder's version is also the runtime version every installer it
// builds is pinned to (spec 001, R5), so a build that is not a release says
// so: `0.1.0-1a2b3c4-dev`.
package version

// Version is set with -ldflags "-X github.com/ushineko/fynstall/internal/version.Version=...".
var Version = "dev"
