package builder

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

/*
Cross-building the variant with the wizard.

That variant links Fyne, which needs cgo, which needs a C compiler that
targets the machine the installer will run on. For a long time this builder
refused the whole idea: a full installer built only for the machine that built
it, and other targets were told to use --cli-only.

The refusal was a guess about cgo rather than a fact about it. Go sets GOOS,
GOARCH and CC per build; Fyne's Windows surface is -lgdi32 and -lopengl32, and
GLFW vendors the mingw headers it needs, so a Linux machine with mingw-w64
builds a Windows Fyne program with no SDK and no Windows machine. A consuming
program in this author's own tree had been doing exactly that for its own
binaries while this builder refused to do it for their installer.

So the rule is now the narrow true one: a cross-built wizard needs a C
compiler for the target. When there is one, the build goes ahead; when there
is not, the refusal names the compiler to install and the flag that avoids
needing one. Nothing is guessed at — if the compiler is there and the build
still fails, the toolchain's own error is what the caller sees.
*/

/*
crossCompiler is the C compiler for a target, and whether one is needed.

An explicit CC always wins: a caller who set it has a toolchain in mind, and
second-guessing it is how a build that worked yesterday stops working. The
defaults are the names the usual packages install under.
*/
func crossCompiler(target string, env []string) (cc, cxx string, needed bool) {
	host := runtime.GOOS + "/" + runtime.GOARCH
	if target == host {
		return "", "", false
	}
	if cc := valueOf("CC", env); cc != "" {
		return cc, valueOf("CXX", env), true
	}
	cc, cxx = defaultCompilers(target)
	return cc, cxx, true
}

// defaultCompilers are the usual names for a target's cross toolchain.
func defaultCompilers(target string) (cc, cxx string) {
	switch target {
	case "windows/amd64":
		return "x86_64-w64-mingw32-gcc", "x86_64-w64-mingw32-g++"
	case "linux/amd64":
		return "x86_64-linux-gnu-gcc", "x86_64-linux-gnu-g++"
	case "linux/arm64":
		return "aarch64-linux-gnu-gcc", "aarch64-linux-gnu-g++"
	default:
		return "", ""
	}
}

/*
checkCrossCompiler reports whether the wizard can be built for a target.

The message is the whole point of the check. "exec: x86_64-w64-mingw32-gcc:
not found", which is what the toolchain says on its own, leaves somebody to
work out what that program is and which package has it; this names the
package, and says that --cli-only needs none of it.
*/
func checkCrossCompiler(target string, env []string) error {
	cc, _, needed := crossCompiler(target, env)
	if !needed {
		return nil
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	if cc == "" {
		return fmt.Errorf("target %s: the wizard needs cgo, and this builder knows no C compiler "+
			"that targets it from %s; set CC to one, or build it with --cli-only, which needs none",
			target, host)
	}
	if _, err := exec.LookPath(cc); err != nil {
		return fmt.Errorf("target %s: the wizard needs cgo, so building it from %s needs %s on PATH (%s); "+
			"install it, set CC to another, or build it with --cli-only, which needs none",
			target, host, cc, packageFor(cc))
	}
	return nil
}

// packageFor names where a compiler usually comes from, so the message ends
// with something to type rather than something to search for.
func packageFor(cc string) string {
	switch {
	case strings.Contains(cc, "mingw32"):
		return "Debian and Ubuntu: gcc-mingw-w64-x86-64; Arch: mingw-w64-gcc; Fedora: mingw64-gcc"
	case strings.Contains(cc, "aarch64-linux-gnu"):
		return "Debian and Ubuntu: gcc-aarch64-linux-gnu; Fedora: gcc-aarch64-linux-gnu"
	default:
		return "from this platform's cross-compiler packages"
	}
}

// valueOf reads the last NAME=value for name, matching how a process reads
// its own environment: later entries win.
func valueOf(name string, env []string) string {
	prefix := name + "="
	value := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
		}
	}
	return value
}
