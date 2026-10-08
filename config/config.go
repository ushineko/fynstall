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
	Targets     []string    `yaml:"targets"`
}

// App identifies the program.
type App struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	Version   string `yaml:"version"`
	Publisher string `yaml:"publisher"`
}

// Install says where the program goes.
type Install struct {
	// Scopes the user may choose; the first is the default. Default [user].
	Scopes []string `yaml:"scopes"`
	// Dir maps a scope to a path template. Missing scopes get DefaultDirs.
	Dir map[string]string `yaml:"dir"`
}

// Entry is one payload item: a file, or a directory copied recursively.
type Entry struct {
	Src     string   `yaml:"src"`
	Dst     string   `yaml:"dst"`
	Mode    string   `yaml:"mode"`
	Exclude []string `yaml:"exclude"`
}

// Integration is how the program meets the desktop. Phase 1 has only the
// paths an uninstaller leaves alone; links and launcher entries are phase 2.
type Integration struct {
	KeepOnUninstall []string `yaml:"keep_on_uninstall"`
}

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
