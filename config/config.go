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
	Uninstall   Uninstall   `yaml:"uninstall"`
	UI          UI          `yaml:"ui"`
	CLI         CLI         `yaml:"cli"`
	Targets     []string    `yaml:"targets"`
}

// CLI is what the installer takes on its command line beyond its own flags.
type CLI struct {
	// Compat is nsis, to take the switches of an NSIS installer as well
	// (/S, /D=<dir> and /<Name>=<value>), or empty (spec 002 L1).
	Compat string `yaml:"compat"`
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
// set. Each is recorded with its undo, so the uninstaller reverses it.
type Action struct {
	ConfigFile *FileAction    `yaml:"config_file"`
	Service    *ServiceAction `yaml:"service"`
	Run        *RunAction     `yaml:"run"`
	Migrate    *MigrateAction `yaml:"migrate"`
}

// ServiceAction runs a payload program as a service: a systemd unit on
// Linux. The uninstaller stops and removes it, and puts back a service of
// the same name that was there before.
type ServiceAction struct {
	// Name names the service: <name>.service on Linux.
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Exec is a payload destination, which may use build placeholders.
	Exec string   `yaml:"exec"`
	Args []string `yaml:"args"`
	// Start starts the service after the install; the default is true.
	Start *bool `yaml:"start"`
	// Restart is no, on-failure (the default) or always.
	Restart string `yaml:"restart"`
}

// Starts reports whether the service is started after the install.
func (s *ServiceAction) Starts() bool { return s.Start == nil || *s.Start }

// RestartPolicy is Restart with its default.
func (s *ServiceAction) RestartPolicy() string {
	if s.Restart == "" {
		return "on-failure"
	}
	return s.Restart
}

// RunAction runs a payload program, never a shell: the last resort for a
// change no built-in action makes.
type RunAction struct {
	// On is install (the default), when the program runs during the
	// install and Undo at uninstall, or uninstall, when it runs before the
	// uninstaller removes anything.
	On string `yaml:"on"`
	// Exec is a payload destination, which may use build placeholders.
	Exec string   `yaml:"exec"`
	Args []string `yaml:"args"`
	// Undo are the arguments Exec is run with to undo the action, or the
	// word none. Required for on: install.
	Undo Undo `yaml:"undo"`
	// ContinueOnError lets an uninstall go on when an uninstall hook fails.
	ContinueOnError bool `yaml:"continue_on_error"`
}

// Hook reports whether the action runs at uninstall rather than install.
func (r *RunAction) Hook() bool { return r.On == "uninstall" }

// Undo is a run action's undo: arguments, or the word none.
type Undo struct {
	Set  bool
	None bool
	Args []string
}

// UnmarshalYAML reads a list of arguments or the scalar none.
func (u *Undo) UnmarshalYAML(n *yaml.Node) error {
	u.Set = true
	if n.Kind == yaml.ScalarNode {
		if n.Value != "none" {
			return fmt.Errorf("line %d: undo is a list of arguments or the word none, not %q", n.Line, n.Value)
		}
		u.None = true
		return nil
	}
	if err := n.Decode(&u.Args); err != nil {
		return fmt.Errorf("undo: %w", err)
	}
	return nil
}

// MigrateAction moves data an older version kept at From to To, which is
// kept on uninstall. A failed install moves it back; nothing else does.
type MigrateAction struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
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

// Uninstall is what the uninstaller does beyond replaying the install.
type Uninstall struct {
	// Remove are patterns, relative to the install directory, for files
	// the program makes, such as a bytecode cache. The uninstaller removes
	// them without asking (spec 002 D5). "**" matches any number of
	// directories.
	Remove []string `yaml:"remove"`
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
	// Licence is a text or Markdown file the wizard shows, and that must be
	// accepted before the install goes on.
	Licence string `yaml:"licence"`
}

// UI is what the wizard shows.
type UI struct {
	// Welcome is a Markdown file for the first page; without it, the page
	// says what is installed and by whom.
	Welcome string `yaml:"welcome"`
	// Launch is a payload destination, which may use build placeholders.
	// The finish page offers to start it.
	Launch string `yaml:"launch"`
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
	// GenericName says what kind of program it is, such as "Wallpaper
	// manager"; Keywords are what a launcher's search matches.
	GenericName string   `yaml:"generic_name"`
	Keywords    []string `yaml:"keywords"`
	// StartupNotify tells the launcher the program signals when its
	// window is up, so it shows a busy cursor until then. Not written when
	// unset.
	StartupNotify *bool `yaml:"startup_notify"`
	// StartupWMClass matches the window to the entry on X11 and XWayland.
	// The default is the entry's ID, which is the app ID for the main
	// entry, and what Fyne sets as the window's class.
	StartupWMClass string `yaml:"startup_wm_class"`
}

// MinIconSize is the smallest source icon accepted: the largest hicolor
// size, so every installed size is a reduction.
const MinIconSize = 512

// Scopes and targets this version accepts.
var (
	KnownScopes  = []string{"user", "system"}
	KnownTargets = []string{"linux/amd64", "linux/arm64", "windows/amd64"}
	DefaultDirs  = map[string]string{"user": "{programs}/{id}", "system": "{programs}/{id}"}
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
		if c.Integration.Desktop[i].StartupWMClass == "" && !c.Integration.Desktop[i].Terminal {
			c.Integration.Desktop[i].StartupWMClass = c.Integration.Desktop[i].ID
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
