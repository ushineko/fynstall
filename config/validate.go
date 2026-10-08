package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ushineko/fynstall/manifest"
)

var (
	// A reverse-DNS ID: at least two dot-separated segments. It names the
	// desktop entry on Linux, so it must match what Wayland compositors
	// compare against the window's app_id.
	idRE      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+)+$`)
	versionRE = regexp.MustCompile(`^\d+\.\d+\.\d+([-+][0-9A-Za-z.+-]+)?$`)
	modeRE    = regexp.MustCompile(`^0?[0-7]{3}$`)
)

// embedForbidden are the characters go:embed refuses in a file name; a
// destination containing one could never be embedded.
const embedForbidden = "\"*<>?`'|\\:"

type checker struct {
	path string
	root *yaml.Node
	errs Errors
}

func (k *checker) fail(line int, field, format string, a ...any) {
	k.errs = append(k.errs, Error{File: k.path, Line: line, Field: field, Msg: fmt.Sprintf(format, a...)})
}

// line returns the line of the node at keys (strings for mapping keys, ints
// for sequence indexes), or of the deepest ancestor that exists, so a
// missing field is reported where its parent is.
func (k *checker) line(keys ...any) int {
	n := k.root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	best := n.Line
	for _, key := range keys {
		var next *yaml.Node
		switch key := key.(type) {
		case string:
			if n.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(n.Content); i += 2 {
					if n.Content[i].Value == key {
						next = n.Content[i+1]
						break
					}
				}
			}
		case int:
			if n.Kind == yaml.SequenceNode && key < len(n.Content) {
				next = n.Content[key]
			}
		}
		if next == nil {
			return best
		}
		// A key's line, not its value's: a mapping's value starts on the
		// line after its key, and "app: is missing version" belongs on app.
		if name, isKey := key.(string); isKey {
			best = keyLine(n, name)
		} else {
			best = next.Line
		}
		n = next
	}
	return best
}

func keyLine(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i].Line
		}
	}
	return mapping.Line
}

func validate(c *Config, root *yaml.Node) Errors {
	k := &checker{path: c.Path, root: root}

	required := func(v, field string, keys ...any) bool {
		if strings.TrimSpace(v) == "" {
			k.fail(k.line(keys...), field, "required")
			return false
		}
		return true
	}
	if required(c.App.ID, "app.id", "app", "id") && !idRE.MatchString(c.App.ID) {
		k.fail(k.line("app", "id"), "app.id", "%q is not a reverse-DNS ID such as io.example.app", c.App.ID)
	}
	required(c.App.Name, "app.name", "app", "name")
	if required(c.App.Version, "app.version", "app", "version") && !versionRE.MatchString(c.App.Version) {
		k.fail(k.line("app", "version"), "app.version", "%q is not a version such as 1.2.3", c.App.Version)
	}

	for i, s := range c.Install.Scopes {
		if !slices.Contains(KnownScopes, s) {
			k.fail(k.line("install", "scopes", i), "install.scopes", "unknown scope %q (want %s)", s, strings.Join(KnownScopes, " or "))
		}
	}
	for scope, dir := range c.Install.Dir {
		field := "install.dir." + scope
		if !slices.Contains(KnownScopes, scope) {
			k.fail(k.line("install", "dir", scope), field, "unknown scope %q", scope)
			continue
		}
		k.template(dir, field, "install", "dir", scope)
	}
	for i, p := range c.Integration.KeepOnUninstall {
		k.template(p, "integration.keep_on_uninstall", "integration", "keep_on_uninstall", i)
	}

	if len(c.Payload) == 0 {
		k.fail(k.line("payload"), "payload", "at least one entry is required")
	}
	for i, e := range c.Payload {
		k.entry(c, i, e)
	}

	for i, t := range c.Targets {
		if !slices.Contains(KnownTargets, t) {
			k.fail(k.line("targets", i), "targets", "unknown target %q (want one of %s)", t, strings.Join(KnownTargets, ", "))
		}
	}
	return k.errs
}

func (k *checker) template(tmpl, field string, keys ...any) {
	if unknown := manifest.Unknown(tmpl); len(unknown) > 0 {
		k.fail(k.line(keys...), field, "unknown placeholder {%s} (known: %s)",
			strings.Join(unknown, "}, {"), strings.Join(manifest.Placeholders, ", "))
	}
}

func (k *checker) entry(c *Config, i int, e Entry) {
	field := fmt.Sprintf("payload[%d]", i)
	if strings.TrimSpace(e.Src) == "" {
		k.fail(k.line("payload", i), field+".src", "required")
	} else if _, err := os.Lstat(filepath.Join(c.Dir, filepath.FromSlash(e.Src))); err != nil {
		k.fail(k.line("payload", i, "src"), field+".src", "%s does not exist", e.Src)
	}
	if strings.TrimSpace(e.Dst) == "" {
		k.fail(k.line("payload", i), field+".dst", "required")
	} else if msg := CheckDst(e.Dst); msg != "" {
		k.fail(k.line("payload", i, "dst"), field+".dst", "%s", msg)
	}
	if e.Mode != "" && !modeRE.MatchString(e.Mode) {
		k.fail(k.line("payload", i, "mode"), field+".mode", "%q is not an octal mode such as 0755", e.Mode)
	}
	for j, pat := range e.Exclude {
		if _, err := path.Match(pat, ""); err != nil {
			k.fail(k.line("payload", i, "exclude", j), field+".exclude", "bad pattern %q", pat)
		}
	}
}

// CheckDst returns why dst cannot be a payload destination, or "". A
// destination is relative to the install directory and must stay inside it.
func CheckDst(dst string) string {
	if strings.HasPrefix(dst, "/") || strings.Contains(dst, "\\") || filepath.IsAbs(dst) {
		return fmt.Sprintf("%q must be a relative path with forward slashes", dst)
	}
	clean := path.Clean(strings.TrimSuffix(dst, "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Sprintf("%q leaves the install directory", dst)
	}
	if strings.HasPrefix(clean, ".fynstall") {
		return fmt.Sprintf("%q is inside .fynstall, which fynstall owns", dst)
	}
	if path.Base(clean) == "go.mod" {
		return fmt.Sprintf("%q is named go.mod, which Go embedding treats as a module boundary", dst)
	}
	if strings.ContainsAny(dst, embedForbidden) {
		return fmt.Sprintf("%q contains one of %s, which Go embedding refuses", dst, embedForbidden)
	}
	return ""
}

// ParseMode reads an Entry.Mode; "" gives 0, meaning "detect".
func ParseMode(s string) uint32 {
	if s == "" {
		return 0
	}
	n, _ := strconv.ParseUint(s, 8, 32)
	return uint32(n)
}
