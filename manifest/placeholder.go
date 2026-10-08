package manifest

import (
	"fmt"
	"regexp"
	"strings"
)

// Placeholders are the names a path template may use. Each resolves per OS
// and scope at install time (package platform), except id, name and
// version, which come from the app.
var Placeholders = []string{"id", "name", "version", "home", "data", "config", "bin"}

var placeholder = regexp.MustCompile(`\{([^{}]*)\}`)

// Unknown returns the placeholder names in tmpl that are not in
// Placeholders, in order of appearance.
func Unknown(tmpl string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(tmpl, -1) {
		if !known(m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

func known(name string) bool {
	for _, p := range Placeholders {
		if p == name {
			return true
		}
	}
	return false
}

// Expand replaces each {name} in tmpl with vars[name]. A name with no value
// is an error, so a template never installs into a literal "{data}".
func Expand(tmpl string, vars map[string]string) (string, error) {
	var missing []string
	out := placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := m[1 : len(m)-1]
		v, ok := vars[name]
		if !ok || v == "" {
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
