package engine

import (
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/ushineko/fynstall/manifest"
)

// Previous returns the parameter values the install r was given, so an
// upgrade does not ask again (spec 002 D3a). The non-secret ones come from
// the receipt. A secret is never in the receipt; it is read back from the
// config file m writes it into, through the same config_file definition,
// when a value there is exactly {param:<name>}. A secret that cannot be
// read, such as one in a system install's file read by the person's own
// process, is left out and named in unread.
func Previous(m *manifest.Manifest, r *Receipt, env func(string) string) (values map[string]string, unread []string) {
	values = map[string]string{}
	declared := map[string]bool{}
	for _, p := range m.Parameters {
		declared[p.Name] = true
	}
	for k, v := range r.Parameters {
		if declared[k] {
			values[k] = v
		}
	}
	vars, err := Vars(m, r.Scope, env)
	for _, name := range r.Secrets {
		if !declared[name] {
			continue
		}
		var value string
		ok := false
		for _, cf := range m.ConfigFiles {
			if err != nil {
				break
			}
			key := keyFor(cf, name)
			if key == "" {
				continue
			}
			path, xerr := manifest.Expand(cf.Path, vars)
			if xerr != nil {
				continue
			}
			b, rerr := os.ReadFile(filepath.Clean(path))
			if rerr != nil {
				continue
			}
			var doc map[string]any
			if yaml.Unmarshal(b, &doc) != nil {
				continue
			}
			if s, isString := doc[key].(string); isString {
				value, ok = s, true
				break
			}
		}
		if ok {
			values[name] = value
		} else {
			unread = append(unread, name)
		}
	}
	slices.Sort(unread)
	return values, unread
}

// keyFor is the key of cf whose value is exactly {param:name}, or "".
func keyFor(cf manifest.ConfigFile, name string) string {
	keys := make([]string, 0, len(cf.Values))
	for k := range cf.Values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if cf.Values[k] == "{param:"+name+"}" {
			return k
		}
	}
	return ""
}
