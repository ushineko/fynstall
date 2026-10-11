package installer

import (
	"fmt"
	"strings"

	"github.com/ushineko/fynstall/manifest"
)

// CompatNSIS is the cli.compat value that makes an installer take the
// switches of an NSIS installer as well as its own (spec 002 L1).
const CompatNSIS = "nsis"

// nsisArgs rewrites the switches of an NSIS installer into the installer's
// own, so that the automation, the MSI wrapper and the self-update of a
// program that had an NSIS installer run this one unchanged. It adds no
// behaviour: each switch becomes a native flag, and every other argument is
// left as it is.
//
//	/S              --yes
//	/D=<dir>        --dir=<dir>; the last switch, and it takes the rest of
//	                the line, as NSIS reads a directory with spaces unquoted
//	/<Name>=<value> --<name>=<value> for a parameter; the name is compared
//	                without regard to case, - or _
//
// Any other argument that starts with / is an error, not a guess.
func nsisArgs(args []string, m *manifest.Manifest) ([]string, error) {
	plain := func(s string) string {
		return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s))
	}
	params := map[string]string{}
	for _, p := range m.Parameters {
		params[plain(p.Name)] = p.Name
	}
	var out []string
	for i, a := range args {
		if !strings.HasPrefix(a, "/") {
			out = append(out, a)
			continue
		}
		name, value, hasValue := strings.Cut(a[1:], "=")
		switch {
		case a == "/S":
			out = append(out, "--yes")
		case name == "D" && hasValue:
			return append(out, "--dir="+strings.Join(append([]string{value}, args[i+1:]...), " ")), nil
		case hasValue && params[plain(name)] != "":
			out = append(out, "--"+params[plain(name)]+"="+value)
		default:
			return nil, fmt.Errorf("%s is not a switch of this installer: it takes /S, /D=<directory> and /<Name>=<value> for each of its parameters, and its own flags (--help)", a)
		}
	}
	return out, nil
}
