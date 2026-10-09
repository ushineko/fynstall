/*
Package config reads and validates fynstall.yaml.

Every error names the file, the line and the field, and all of them are
reported at once, so a developer fixes a config in one pass (spec 001, R1).
An unknown key is an error: a typo in an optional key would otherwise be a
silently ignored setting.
*/
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is a parsed fynstall.yaml. Paths in Payload are relative to Dir.
type Config struct {
	// Path and Dir are the file read and its directory.
	Path string `yaml:"-"`
	Dir  string `yaml:"-"`

	App         App         `yaml:"app"`
	Install     Install     `yaml:"install"`
	Payload     []Entry     `yaml:"payload"`
	Integration Integration `yaml:"integration"`
	Parameters  []Parameter `yaml:"parameters"`
	Actions     []Action    `yaml:"actions"`
	Targets     []string    `yaml:"targets"`
}

// Parameter is a value the installer asks for or is given (spec 002 D3a):
// on the command line as --<name>=<value>, in fynstall-params.yml beside
// the installer, or by a person. Actions use it as {param:<name>}.
type Parameter struct {
	Name        string `yaml:"name"`
	Label       string `yaml:"label"`
	Description string `yaml:"description"`
	Default     string `yaml:"default"`
	// Secret values are never logged, printed or recorded.
	Secret   bool `yaml:"secret"`
	Required bool `yaml:"required"`
}

// Action is one install-time action (spec 002 D2a). Exactly one field is
// set. Only config_file exists so far; the others are accepted by the
// parser so that a config written for a later version says which phase it
// needs rather than "unknown key".
type Action struct {
	ConfigFile *FileAction `yaml:"config_file"`
	// The later types are held as plain values: a strict decode into a
	// yaml.Node still reports the keys inside it as unknown.
	Service any `yaml:"service"`
	Run     any `yaml:"run"`
	Migrate any `yaml:"migrate"`
}

// FileAction writes a configuration file from parameters. The engine
// writes it, so the installer does not run the program to produce it.
type FileAction struct {
	// Path starts with {config}, {data} or {home}: configuration lives
	// outside the install directory, so it can be kept across upgrades.
	Path string `yaml:"path"`
	// Format is yaml or json; the default comes from Path's extension.
	Format string `yaml:"format"`
	// Values are written as one flat map, keys sorted.
	Values map[string]string `yaml:"values"`
}

// App identifies the program.
type App struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	Version   string `yaml:"version"`
	Publisher string `yaml:"publisher"`
	// Icon is a square PNG of at least MinIconSize pixels, relative to the
	// config. The build resizes it to the hicolor sizes.
	Icon string `yaml:"icon"`
}

// Install says where the program goes.
type Install struct {
	// Scopes the user may choose; the first is the default. Default [user].
	Scopes []string `yaml:"scopes"`
	// Dir maps a scope to a path template. Missing scopes get DefaultDirs.
	Dir map[string]string `yaml:"dir"`
}

// Entry is one payload item: a file, or a directory copied recursively.
// Src and Dst may use the build-time placeholders {os}, {arch} and {exe}.
type Entry struct {
	Src     string   `yaml:"src"`
	Dst     string   `yaml:"dst"`
	Mode    string   `yaml:"mode"`
	Exclude []string `yaml:"exclude"`
	// Targets limits the entry to these "os/arch" patterns, where either
	// part may be "*". Empty means every target.
	Targets []string `yaml:"targets"`
}

// Applies reports whether the entry is part of target's payload.
func (e Entry) Applies(target string) bool { return Matches(e.Targets, target) }

// Matches reports whether target ("os/arch") matches any of patterns, or
// patterns is empty.
func Matches(patterns []string, target string) bool {
	if len(patterns) == 0 {
		return true
	}
	os, arch, _ := strings.Cut(target, "/")
	for _, p := range patterns {
		po, pa, _ := strings.Cut(p, "/")
		if (po == "*" || po == os) && (pa == "*" || pa == arch) {
			return true
		}
	}
	return false
}

// BuildTargets are the targets the config is built for: its own targets,
// or this machine's when it names none.
func (c *Config) BuildTargets() []string {
	if len(c.Targets) > 0 {
		return c.Targets
	}
	return []string{runtime.GOOS + "/" + runtime.GOARCH}
}

// Integration is how the program meets the desktop.
type Integration struct {
	// PathLinks are payload destinations to link into {bin}, under their
	// own base names.
	PathLinks []string `yaml:"path_links"`
	// Desktop entries go in {data}/applications.
	Desktop         []Desktop `yaml:"desktop"`
	KeepOnUninstall []string  `yaml:"keep_on_uninstall"`
}

// Desktop is one launcher entry.
type Desktop struct {
	// ID names the file, <id>.desktop. The default is app.id: Wayland
	// compositors match a window's app_id to the entry of that name, so the
	// program's main window needs an entry with the app ID.
	ID         string   `yaml:"id"`
	Name       string   `yaml:"name"`
	Comment    string   `yaml:"comment"`
	Exec       string   `yaml:"exec"`
	Args       []string `yaml:"args"`
	Categories []string `yaml:"categories"`
	Terminal   bool     `yaml:"terminal"`
}

// MinIconSize is the smallest source icon accepted: the largest hicolor
// size, so every installed size is a reduction.
const MinIconSize = 512

// Scopes and targets this version accepts.
var (
	KnownScopes  = []string{"user", "system"}
	KnownTargets = []string{"linux/amd64", "linux/arm64", "windows/amd64"}
	DefaultDirs  = map[string]string{"user": "{data}/{id}", "system": "/opt/{id}"}
)

// Error is one problem in a config, at a line when the line is known.
type Error struct {
	File  string
	Line  int
	Field string
	Msg   string
}

func (e Error) Error() string {
	loc := e.File
	if e.Line > 0 {
		loc += ":" + strconv.Itoa(e.Line)
	}
	if e.Field != "" {
		return fmt.Sprintf("%s: %s: %s", loc, e.Field, e.Msg)
	}
	return fmt.Sprintf("%s: %s", loc, e.Msg)
}

// Errors is every problem found, sorted by line.
type Errors []Error

func (es Errors) Error() string {
	lines := make([]string, len(es))
	for i, e := range es {
		lines[i] = e.Error()
	}
	return strings.Join(lines, "\n")
}

// Load reads and validates the config at path. A non-nil error is an
// Errors when the file was read, so a caller can list each problem.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(b, abs)
}

// Parse validates the config in b, read from path.
func Parse(b []byte, path string) (*Config, error) {
	c := &Config{Path: path, Dir: filepath.Dir(path)}
	var errs Errors

	// The strict decode finds unknown keys and wrong types, with lines.
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		var te *yaml.TypeError
		if !errors.As(err, &te) {
			return nil, Errors{{File: path, Line: lineOf(err.Error()), Msg: stripLine(err.Error())}}
		}
		for _, msg := range te.Errors {
			errs = append(errs, typeError(path, msg))
		}
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, Errors{{File: path, Msg: err.Error()}}
	}
	c.defaults()
	errs = append(errs, validate(c, &root)...)
	if len(errs) > 0 {
		sort.SliceStable(errs, func(i, j int) bool { return errs[i].Line < errs[j].Line })
		return nil, errs
	}
	return c, nil
}

func (c *Config) defaults() {
	if len(c.Install.Scopes) == 0 {
		c.Install.Scopes = []string{"user"}
	}
	if c.Install.Dir == nil {
		c.Install.Dir = map[string]string{}
	}
	for scope, dir := range DefaultDirs {
		if _, ok := c.Install.Dir[scope]; !ok {
			c.Install.Dir[scope] = dir
		}
	}
	for i := range c.Integration.Desktop {
		if c.Integration.Desktop[i].ID == "" {
			c.Integration.Desktop[i].ID = c.App.ID
		}
	}
}

var (
	lineRE    = regexp.MustCompile(`line (\d+): `)
	unknownRE = regexp.MustCompile(`field (\S+) not found in type`)
)

func lineOf(msg string) int {
	if m := lineRE.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func stripLine(msg string) string {
	return strings.TrimPrefix(lineRE.ReplaceAllString(msg, ""), "yaml: ")
}

func typeError(path, msg string) Error {
	if m := unknownRE.FindStringSubmatch(msg); m != nil {
		return Error{File: path, Line: lineOf(msg), Field: m[1], Msg: "unknown key"}
	}
	return Error{File: path, Line: lineOf(msg), Msg: stripLine(msg)}
}
