package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// RuntimeModule is fynstall's own module, which an extension imports.
const RuntimeModule = "github.com/ushineko/fynstall"

/*
ExtensionPackage is where a config's extension is (spec 003 R1): the Go
package the builder links into the installer and the uninstaller.
*/
type ExtensionPackage struct {
	// Dir is the package directory.
	Dir string
	// ModuleDir is the directory of the go.mod the package belongs to, and
	// ModulePath that module's path.
	ModuleDir  string
	ModulePath string
	// ImportPath is the package's import path.
	ImportPath string
	// Requires is the fynstall version the module's go.mod requires,
	// without its "v", or "" when it names none. Replaced is true when
	// that go.mod replaces fynstall with a directory, as a program
	// developed beside a fynstall checkout does.
	Requires string
	Replaced bool
}

var (
	moduleLine  = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	requireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(RuntimeModule) + `\s+v(\S+)`)
	replaceLine = regexp.MustCompile(`(?m)^\s*(?:replace\s+)?` + regexp.QuoteMeta(RuntimeModule) + `\s*(?:v\S+\s*)?=>`)
)

// FindExtension locates the package c.Extension names, or returns nil when
// the config has no extension.
func FindExtension(c *Config) (*ExtensionPackage, error) {
	if c.Extension == "" {
		return nil, nil
	}
	dir, err := filepath.Abs(filepath.Join(c.Dir, filepath.FromSlash(c.Extension)))
	if err != nil {
		return nil, fmt.Errorf("extension: %w", err)
	}
	fi, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("extension: %s does not exist", c.Extension)
	case err != nil:
		return nil, fmt.Errorf("extension: %w", err)
	case !fi.IsDir():
		return nil, fmt.Errorf("extension: %s is not a directory; name the directory of a Go package", c.Extension)
	}
	sources, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	if len(sources) == 0 {
		return nil, fmt.Errorf("extension: %s holds no Go files", c.Extension)
	}
	for mod := dir; ; mod = filepath.Dir(mod) {
		b, err := os.ReadFile(filepath.Join(mod, "go.mod")) // #nosec G304 -- a go.mod above the extension the config names
		if err == nil {
			m := moduleLine.FindSubmatch(b)
			if m == nil {
				return nil, fmt.Errorf("extension: %s names no module", filepath.Join(mod, "go.mod"))
			}
			rel, err := filepath.Rel(mod, dir)
			if err != nil {
				return nil, fmt.Errorf("extension: %w", err)
			}
			x := &ExtensionPackage{
				Dir: dir, ModuleDir: mod, ModulePath: string(m[1]),
				ImportPath: path.Join(string(m[1]), filepath.ToSlash(rel)),
				Replaced:   replaceLine.Match(b),
			}
			if r := requireLine.FindSubmatch(b); r != nil {
				x.Requires = strings.TrimSuffix(string(r[1]), "+incompatible")
			}
			return x, nil
		}
		if filepath.Dir(mod) == mod {
			return nil, fmt.Errorf("extension: %s is not inside a Go module: no go.mod in it or above it", c.Extension)
		}
	}
}
