package manifest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Placeholders are the names a path template may use. Each resolves per OS
// and scope at install time (package platform), except id, name and
// version, which come from the app.
var Placeholders = []string{"id", "name", "version", "home", "data", "config", "bin", "programs"}

// BuildPlaceholders are the names a payload src or dst, a link or a desktop
// exec may use. They resolve per target when the installer is built, so one
// config serves every target (spec 002 D1a).
var BuildPlaceholders = []string{"os", "arch", "exe"}

// BuildVars are the values of BuildPlaceholders for target ("os/arch").
func BuildVars(target string) map[string]string {
	os, arch, _ := strings.Cut(target, "/")
	exe := ""
	if os == "windows" {
		exe = ".exe"
	}
	return map[string]string{"os": os, "arch": arch, "exe": exe}
}

var placeholder = regexp.MustCompile(`\{([^{}]*)\}`)

// Unknown returns the placeholder names in tmpl that are not in
// Placeholders, in order of appearance.
func Unknown(tmpl string) []string { return UnknownIn(tmpl, Placeholders) }

// UnknownIn returns the placeholder names in tmpl that are not in names.
func UnknownIn(tmpl string, names []string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(tmpl, -1) {
		if !slices.Contains(names, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// HasPlaceholder reports whether tmpl names any placeholder.
func HasPlaceholder(tmpl string) bool { return placeholder.MatchString(tmpl) }

// Expand replaces each {name} in tmpl with vars[name]. A name that vars
// does not hold is an error, so a template never installs into a literal
// "{data}". An empty value is a value: {exe} is "" outside Windows.
func Expand(tmpl string, vars map[string]string) (string, error) {
	var missing []string
	out := placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%q: no value for {%s}", tmpl, strings.Join(missing, "}, {"))
	}
	return out, nil
}
