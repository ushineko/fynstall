package config

import (
	"fmt"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	k.noHomeInSystem(c)
	k.parameters(c)
	k.actions(c)
	k.textFile(c, c.App.Licence, "app.licence", "app", "licence")
	k.textFile(c, c.UI.Welcome, "ui.welcome", "ui", "welcome")
	if c.UI.Launch != "" && !k.buildTemplate(c.UI.Launch, "ui.launch", "ui", "launch") {
		if msg := CheckDst(c.UI.Launch); msg != "" {
			k.fail(k.line("ui", "launch"), "ui.launch", "%s", msg)
		}
	}
	for i, p := range c.Integration.KeepOnUninstall {
		k.template(p, "integration.keep_on_uninstall", "integration", "keep_on_uninstall", i)
	}
	for i, p := range c.Uninstall.Remove {
		if msg := manifest.CheckPattern(p); msg != "" {
			k.fail(k.line("uninstall", "remove", i), "uninstall.remove", "%s", msg)
		}
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

// noHomeInSystem refuses {home} in a config that offers system scope: a
// system install is for every user, so it has no home (spec 001 R15).
func (k *checker) noHomeInSystem(c *Config) {
	if !slices.Contains(c.Install.Scopes, "system") {
		return
	}
	check := func(v, field string, keys ...any) {
		if strings.Contains(v, "{home}") {
			k.fail(k.line(keys...), field, "%q uses {home}, which a system install for every user does not have; use {data} or {config}", v)
		}
	}
	check(c.Install.Dir["system"], "install.dir.system", "install", "dir", "system")
	for i, p := range c.Integration.KeepOnUninstall {
		check(p, "integration.keep_on_uninstall", "integration", "keep_on_uninstall", i)
	}
	for i, a := range c.Actions {
		field := fmt.Sprintf("actions[%d]", i)
		switch {
		case a.ConfigFile != nil:
			check(a.ConfigFile.Path, field+".config_file.path", "actions", i, "config_file", "path")
		case a.Migrate != nil:
			check(a.Migrate.From, field+".migrate.from", "actions", i, "migrate", "from")
			check(a.Migrate.To, field+".migrate.to", "actions", i, "migrate", "to")
		case a.Service != nil:
			for j, arg := range a.Service.Args {
				check(arg, field+".service.args", "actions", i, "service", "args", j)
			}
		case a.Run != nil:
			for j, arg := range append(append([]string{}, a.Run.Args...), a.Run.Undo.Args...) {
				check(arg, field+".run.args", "actions", i, "run", "args", j)
			}
		}
	}
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
		for key, v := range map[string]string{"name": d.Name, "comment": d.Comment, "generic_name": d.GenericName, "startup_wm_class": d.StartupWMClass} {
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
		for j, kw := range d.Keywords {
			if strings.TrimSpace(kw) == "" || strings.ContainsAny(kw, ";") || strings.ContainsFunc(kw, unicode.IsControl) {
				k.fail(k.line("integration", "desktop", i, "keywords", j), field+".keywords", "%q is not a keyword: one word or phrase, without ;", kw)
			}
		}
		for j, cat := range d.Categories {
			if !categoryRE.MatchString(cat) {
				k.fail(k.line("integration", "desktop", i, "categories", j), field+".categories", "%q is not a category name", cat)
			}
		}
	}
}

// textFile checks that a text file the wizard shows exists, is not huge,
// and is UTF-8.
func (k *checker) textFile(c *Config, rel, field string, keys ...any) {
	if rel == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(rel)))
	switch {
	case err != nil:
		k.fail(k.line(keys...), field, "%s does not exist", rel)
	case len(b) > 256<<10:
		k.fail(k.line(keys...), field, "%s is larger than 256 KiB", rel)
	case !utf8.Valid(b):
		k.fail(k.line(keys...), field, "%s is not UTF-8 text", rel)
	}
}

var paramRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ReservedParameters are the installer's own flags, which a parameter
// cannot be named, since each parameter is also a flag --<name>.
var ReservedParameters = []string{"cli", "gui", "yes", "dry-run", "uninstall", "force-receipt-uninstall",
	"verbose", "version", "dir", "scope", "help", "h"}

func (k *checker) parameters(c *Config) {
	seen := map[string]bool{}
	for i, p := range c.Parameters {
		field := fmt.Sprintf("parameters[%d]", i)
		at := func(key string) int { return k.line("parameters", i, key) }
		switch {
		case !paramRE.MatchString(p.Name):
			k.fail(at("name"), field+".name", "%q is not a parameter name: lowercase letters, digits and dashes, starting with a letter", p.Name)
		case slices.Contains(ReservedParameters, p.Name):
			k.fail(at("name"), field+".name", "%q is one of the installer's own flags (--%s)", p.Name, p.Name)
		case seen[p.Name]:
			k.fail(at("name"), field+".name", "two parameters are named %q", p.Name)
		}
		seen[p.Name] = true
		if p.Secret && p.Default != "" {
			k.fail(at("default"), field+".default", "a secret has no default: it would be published with the config")
		}
		if p.Required && p.Default != "" {
			k.fail(at("default"), field+".default", "a required parameter has no default, or it would never be required")
		}
	}
}

// FileBases are the placeholders a config_file path may start with.
var FileBases = []string{"config", "data", "home"}

// FileFormat is a config_file's format: as given, else from the
// path's extension, else "".
func FileFormat(a *FileAction) string {
	if a.Format != "" {
		return a.Format
	}
	switch strings.ToLower(path.Ext(a.Path)) {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	}
	return ""
}

func (k *checker) actions(c *Config) {
	params := map[string]bool{}
	byName := map[string]Parameter{}
	for _, p := range c.Parameters {
		params[p.Name] = true
		byName[p.Name] = p
	}
	services := map[string]bool{}
	for i, a := range c.Actions {
		field := fmt.Sprintf("actions[%d]", i)
		n := 0
		for _, set := range []bool{a.ConfigFile != nil, a.Service != nil, a.Run != nil, a.Migrate != nil} {
			if set {
				n++
			}
		}
		if n != 1 {
			k.fail(k.line("actions", i), field, "an action is exactly one of config_file, service, run or migrate")
			continue
		}
		switch {
		case a.ConfigFile != nil:
			k.configFile(a.ConfigFile, field+".config_file", params, "actions", i, "config_file")
		case a.Service != nil:
			k.service(a.Service, field+".service", services, "actions", i, "service")
		case a.Run != nil:
			k.run(a.Run, field+".run", byName, "actions", i, "run")
		case a.Migrate != nil:
			k.migrate(c, a.Migrate, field+".migrate", "actions", i, "migrate")
		}
	}
}

var (
	serviceNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	restarts      = []string{"no", "on-failure", "always"}
)

func (k *checker) service(s *ServiceAction, field string, seen map[string]bool, keys ...any) {
	at := func(key string) int { return k.line(append(keys, key)...) }
	switch {
	case !serviceNameRE.MatchString(s.Name) || strings.HasSuffix(s.Name, ".service"):
		k.fail(at("name"), field+".name", "%q is not a service name such as hello (letters, digits, _ . -; no .service)", s.Name)
	case seen[s.Name]:
		k.fail(at("name"), field+".name", "two services are named %q", s.Name)
	}
	seen[s.Name] = true
	k.exec(s.Exec, field+".exec", append(keys, "exec")...)
	for j, a := range s.Args {
		if unknown := manifest.Unknown(a); len(unknown) > 0 {
			k.fail(k.line(append(keys, "args", j)...), field+".args", "unknown placeholder {%s}", strings.Join(unknown, "}, {"))
		}
	}
	if !slices.Contains(restarts, s.RestartPolicy()) {
		k.fail(at("restart"), field+".restart", "%q is not one of %s", s.Restart, strings.Join(restarts, ", "))
	}
	if strings.ContainsFunc(s.Description, unicode.IsControl) {
		k.fail(at("description"), field+".description", "must be one line of text")
	}
}

func (k *checker) run(r *RunAction, field string, params map[string]Parameter, keys ...any) {
	at := func(key string) int { return k.line(append(keys, key)...) }
	switch r.On {
	case "", "install":
		if !r.Undo.Set {
			k.fail(k.line(keys...), field+".undo", "required: the arguments that undo this run, or none")
		}
		if r.ContinueOnError {
			k.fail(at("continue_on_error"), field+".continue_on_error", "only an on: uninstall hook can go on after it fails; a failed install is undone")
		}
	case "uninstall":
		if r.Undo.Set {
			k.fail(at("undo"), field+".undo", "an on: uninstall hook has nothing to undo")
		}
	case "before_install", "after_install":
		k.fail(at("on"), field+".on", "%s hooks arrive with Go extensions (spec 003); this version runs install and uninstall", r.On)
	default:
		k.fail(at("on"), field+".on", "%q is not install or uninstall", r.On)
	}
	k.exec(r.Exec, field+".exec", append(keys, "exec")...)
	check := func(args []string, key string) {
		for j, a := range args {
			for _, name := range manifest.UnknownIn(a, manifest.Placeholders) {
				ref, ok := strings.CutPrefix(name, "param:")
				p, known := params[ref]
				switch {
				case !ok:
					k.fail(k.line(append(keys, key, j)...), field+"."+key, "unknown placeholder {%s}", name)
				case !known:
					k.fail(k.line(append(keys, key, j)...), field+"."+key, "{param:%s} names no parameter", ref)
				case p.Secret:
					k.fail(k.line(append(keys, key, j)...), field+"."+key,
						"{param:%s} is secret: arguments show in the process list and an undo is kept in the receipt; pass it through a config_file", ref)
				}
			}
		}
	}
	check(r.Args, "args")
	check(r.Undo.Args, "undo")
}

// exec checks a payload destination an action runs.
func (k *checker) exec(exec, field string, keys ...any) {
	if strings.TrimSpace(exec) == "" {
		k.fail(k.line(keys...), field, "required")
		return
	}
	if !k.buildTemplate(exec, field, keys...) {
		if msg := CheckDst(exec); msg != "" {
			k.fail(k.line(keys...), field, "%s", msg)
		}
	}
}

func (k *checker) migrate(c *Config, m *MigrateAction, field string, keys ...any) {
	at := func(key string) int { return k.line(append(keys, key)...) }
	for key, p := range map[string]string{"from": m.From, "to": m.To} {
		base, rest, _ := strings.Cut(p, "/")
		if !strings.HasPrefix(base, "{") || !slices.Contains(FileBases, strings.Trim(base, "{}")) || rest == "" {
			k.fail(at(key), field+"."+key, "%q must start with {config}/, {data}/ or {home}/", p)
		} else if unknown := manifest.Unknown(p); len(unknown) > 0 {
			k.fail(at(key), field+"."+key, "unknown placeholder {%s}", strings.Join(unknown, "}, {"))
		} else if clean := path.Clean(rest); clean == ".." || strings.HasPrefix(clean, "../") {
			k.fail(at(key), field+"."+key, "%q leaves the directory it starts in", p)
		}
	}
	kept := false
	for _, keep := range c.Integration.KeepOnUninstall {
		if m.To == keep || strings.HasPrefix(m.To, strings.TrimSuffix(keep, "/")+"/") {
			kept = true
		}
	}
	if !kept {
		k.fail(at("to"), field+".to", "%q must be at or under a keep_on_uninstall path, so the uninstaller leaves the moved data", m.To)
	}
}

func (k *checker) configFile(a *FileAction, field string, params map[string]bool, keys ...any) {
	at := func(key string) int { return k.line(append(keys, key)...) }
	base, rest, _ := strings.Cut(a.Path, "/")
	if !strings.HasPrefix(base, "{") || !slices.Contains(FileBases, strings.Trim(base, "{}")) || rest == "" {
		k.fail(at("path"), field+".path", "%q must start with {config}/, {data}/ or {home}/: configuration lives outside the install directory", a.Path)
	} else if unknown := manifest.Unknown(a.Path); len(unknown) > 0 {
		k.fail(at("path"), field+".path", "unknown placeholder {%s}", strings.Join(unknown, "}, {"))
	} else if msg := CheckDst(rest); msg != "" {
		k.fail(at("path"), field+".path", "%s", msg)
	}
	if f := FileFormat(a); f != "yaml" && f != "json" {
		k.fail(at("format"), field+".format", "say format: yaml or json (the path's extension does not say which)")
	}
	if len(a.Values) == 0 {
		k.fail(at("values"), field+".values", "at least one value is required")
	}
	keysSorted := make([]string, 0, len(a.Values))
	for key := range a.Values {
		keysSorted = append(keysSorted, key)
	}
	sort.Strings(keysSorted)
	for _, key := range keysSorted {
		for _, name := range manifest.UnknownIn(a.Values[key], manifest.Placeholders) {
			ref, ok := strings.CutPrefix(name, "param:")
			switch {
			case !ok:
				k.fail(at("values"), field+".values."+key, "unknown placeholder {%s}", name)
			case !params[ref]:
				k.fail(at("values"), field+".values."+key, "{param:%s} names no parameter", ref)
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
