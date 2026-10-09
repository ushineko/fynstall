package config

import (
	"fmt"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

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
	targetRE  = regexp.MustCompile(`^(\*|[a-z0-9]+)/(\*|[a-z0-9]+)$`)
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
	if c.App.Icon != "" {
		k.icon(c)
	}
	k.links(c)
	k.desktop(c)
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
	wellFormed := true
	for j, t := range e.Targets {
		if !targetRE.MatchString(t) {
			k.fail(k.line("payload", i, "targets", j), field+".targets", "%q is not an os/arch pattern such as linux/amd64 or windows/*", t)
			wellFormed = false
		}
	}
	var applies []string
	for _, t := range c.BuildTargets() {
		if e.Applies(t) {
			applies = append(applies, t)
		}
	}
	if wellFormed && len(e.Targets) > 0 && len(applies) == 0 {
		k.fail(k.line("payload", i, "targets"), field+".targets", "matches none of the targets this config builds (%s)", strings.Join(c.BuildTargets(), ", "))
	}

	if strings.TrimSpace(e.Src) == "" {
		k.fail(k.line("payload", i), field+".src", "required")
	} else if !k.buildTemplate(e.Src, field+".src", "payload", i, "src") {
		k.sources(c, i, e, applies)
	}
	if strings.TrimSpace(e.Dst) == "" {
		k.fail(k.line("payload", i), field+".dst", "required")
	} else if !k.buildTemplate(e.Dst, field+".dst", "payload", i, "dst") {
		if msg := CheckDst(e.Dst); msg != "" {
			k.fail(k.line("payload", i, "dst"), field+".dst", "%s", msg)
		}
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

// sources checks that an entry's src exists. A src with placeholders exists
// per target, so it is checked for each target the entry applies to, and
// the error names the target that lacks it.
func (k *checker) sources(c *Config, i int, e Entry, applies []string) {
	field := fmt.Sprintf("payload[%d].src", i)
	if !manifest.HasPlaceholder(e.Src) {
		if _, err := os.Lstat(filepath.Join(c.Dir, filepath.FromSlash(e.Src))); err != nil {
			k.fail(k.line("payload", i, "src"), field, "%s does not exist", e.Src)
		}
		return
	}
	for _, t := range applies {
		src, err := manifest.Expand(e.Src, manifest.BuildVars(t))
		if err != nil {
			continue // buildTemplate has reported it
		}
		if _, err := os.Lstat(filepath.Join(c.Dir, filepath.FromSlash(src))); err != nil {
			k.fail(k.line("payload", i, "src"), field, "%s does not exist (target %s)", src, t)
		}
	}
}

// buildTemplate reports an unknown build-time placeholder in tmpl, and
// returns true when it did.
func (k *checker) buildTemplate(tmpl, field string, keys ...any) bool {
	unknown := manifest.UnknownIn(tmpl, manifest.BuildPlaceholders)
	if len(unknown) > 0 {
		k.fail(k.line(keys...), field, "unknown placeholder {%s} (a payload path may use {%s})",
			strings.Join(unknown, "}, {"), strings.Join(manifest.BuildPlaceholders, "}, {"))
	}
	return len(unknown) > 0
}

func (k *checker) icon(c *Config) {
	f, err := os.Open(filepath.Join(c.Dir, filepath.FromSlash(c.App.Icon)))
	if err != nil {
		k.fail(k.line("app", "icon"), "app.icon", "%s does not exist", c.App.Icon)
		return
	}
	defer func() { _ = f.Close() }()
	cfg, err := png.DecodeConfig(f)
	switch {
	case err != nil:
		k.fail(k.line("app", "icon"), "app.icon", "%s is not a PNG: %v", c.App.Icon, err)
	case cfg.Width != cfg.Height:
		k.fail(k.line("app", "icon"), "app.icon", "%s is %dx%d; it must be square", c.App.Icon, cfg.Width, cfg.Height)
	case cfg.Width < MinIconSize:
		k.fail(k.line("app", "icon"), "app.icon", "%s is %d px; it must be at least %d", c.App.Icon, cfg.Width, MinIconSize)
	}
}

func (k *checker) links(c *Config) {
	names := map[string]bool{}
	for i, l := range c.Integration.PathLinks {
		line := k.line("integration", "path_links", i)
		if k.buildTemplate(l, "integration.path_links", "integration", "path_links", i) {
			continue
		}
		if msg := CheckDst(l); msg != "" {
			k.fail(line, "integration.path_links", "%s", msg)
			continue
		}
		name := path.Base(l)
		if names[name] {
			k.fail(line, "integration.path_links", "two links are named %q", name)
		}
		names[name] = true
	}
}

var categoryRE = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

func (k *checker) desktop(c *Config) {
	ids := map[string]bool{}
	for i, d := range c.Integration.Desktop {
		field := fmt.Sprintf("integration.desktop[%d]", i)
		at := func(key string) int { return k.line("integration", "desktop", i, key) }
		if !idRE.MatchString(d.ID) {
			k.fail(at("id"), field+".id", "%q is not a reverse-DNS ID such as io.example.app", d.ID)
		}
		if ids[d.ID] {
			k.fail(at("id"), field+".id", "two entries have the ID %q", d.ID)
		}
		ids[d.ID] = true
		if strings.TrimSpace(d.Name) == "" {
			k.fail(at("name"), field+".name", "required")
		}
		for key, v := range map[string]string{"name": d.Name, "comment": d.Comment} {
			if strings.ContainsFunc(v, unicode.IsControl) {
				k.fail(at(key), field+"."+key, "must be one line of text")
			}
		}
		if strings.TrimSpace(d.Exec) == "" {
			k.fail(at("exec"), field+".exec", "required")
		} else if !k.buildTemplate(d.Exec, field+".exec", "integration", "desktop", i, "exec") {
			if msg := CheckDst(d.Exec); msg != "" {
				k.fail(at("exec"), field+".exec", "%s", msg)
			}
		}
		for j, cat := range d.Categories {
			if !categoryRE.MatchString(cat) {
				k.fail(k.line("integration", "desktop", i, "categories", j), field+".categories", "%q is not a category name", cat)
			}
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
