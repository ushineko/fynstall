package installer

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ushineko/fynstall/manifest"
)

// SideFile is the parameter file an installer reads from its own
// directory (spec 002 D3a): a flat map of parameter names to values.
const SideFile = "fynstall-params.yml"

// paramFlags registers --<name> for each parameter and returns the values
// given, keyed by name, once the flags are parsed.
func paramFlags(fl *flag.FlagSet, m *manifest.Manifest) func() map[string]string {
	vals := map[string]*string{}
	for _, p := range m.Parameters {
		usage := p.Label
		if p.Secret {
			usage += " (secret)"
		}
		vals[p.Name] = fl.String(p.Name, "", usage)
	}
	return func() map[string]string {
		given := map[string]string{}
		fl.Visit(func(f *flag.Flag) {
			if v, ok := vals[f.Name]; ok {
				given[f.Name] = *v
			}
		})
		return given
	}
}

// readSideFile reads the parameter file in dir. A missing file is no
// values; a name that is not a parameter is an error, so a misspelt name
// is not a value silently ignored.
func readSideFile(dir string, m *manifest.Manifest) (map[string]string, error) {
	path := filepath.Join(dir, SideFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var vals map[string]string
	if err := yaml.Unmarshal(b, &vals); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	known := map[string]bool{}
	for _, p := range m.Parameters {
		known[p.Name] = true
	}
	var unknown []string
	for k := range vals {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s: %s is not a parameter of this installer", path, strings.Join(unknown, ", "))
	}
	return vals, nil
}

// resolveParams gives every parameter a value, highest source first: a
// flag, the side file, the value the installed version was given (an
// upgrade, spec 002 D3a), a person (when ask is not nil), the default. A
// required parameter left empty is an error that names its flag, unless it
// is deferred: a secret the privileged helper reads back as root.
func resolveParams(m *manifest.Manifest, flags, side, previous map[string]string, deferred []string,
	ask func(p manifest.Parameter) (string, error),
) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range m.Parameters {
		v, ok := flags[p.Name]
		if !ok {
			v, ok = side[p.Name]
		}
		if !ok {
			v, ok = previous[p.Name]
		}
		if !ok && slices.Contains(deferred, p.Name) {
			out[p.Name] = ""
			continue
		}
		if !ok && ask != nil {
			var err error
			if v, err = ask(p); err != nil {
				return nil, err
			}
			ok = v != ""
		}
		if !ok || v == "" {
			v = p.Default
		}
		if p.Required && v == "" {
			return nil, fmt.Errorf("%s is required: pass --%s=…, or set %s in %s", label(p), p.Name, p.Name, SideFile)
		}
		out[p.Name] = v
	}
	return out, nil
}

// parameter asks a person for p's value. A secret is read without echo
// when the input is a terminal. An empty answer means the default.
func (a *asker) parameter(p manifest.Parameter) (string, error) {
	prompt := label(p)
	if p.Default != "" {
		prompt += " [" + p.Default + "]"
	}
	prompt += ": "
	if p.Secret {
		if f, ok := a.e.In.(*os.File); ok && a.e.Interactive {
			_, _ = fmt.Fprint(a.e.Out, prompt)
			v, err := readSecret(f)
			_, _ = fmt.Fprintln(a.e.Out)
			return v, err
		}
	}
	return a.line(prompt)
}

func label(p manifest.Parameter) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Name
}
