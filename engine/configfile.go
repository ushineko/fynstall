package engine

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ushineko/fynstall/manifest"
)

// addConfigFiles plans each config_file action (spec 002 D2a): the file is
// rendered now, from the parameter values, and written by Apply like any
// other file, so the uninstaller removes it unless it is kept.
func (p *Plan) addConfigFiles(vars map[string]string, params map[string]string) error {
	all := map[string]string{}
	for k, v := range vars {
		all[k] = v
	}
	secret := map[string]bool{}
	for _, d := range p.Manifest.Parameters {
		all["param:"+d.Name] = params[d.Name]
		secret[d.Name] = d.Secret
	}
	for _, cf := range p.Manifest.ConfigFiles {
		baseName, _, _ := strings.Cut(cf.Path, "/")
		base := vars[strings.Trim(baseName, "{}")]
		path, err := manifest.Expand(cf.Path, vars)
		if err != nil {
			return fmt.Errorf("config_file: %w", err)
		}
		values := map[string]string{}
		holdsSecret := false
		for k, tmpl := range cf.Values {
			if values[k], err = manifest.Expand(tmpl, all); err != nil {
				return fmt.Errorf("config_file %s: %w", cf.Path, err)
			}
			for name, s := range secret {
				if s && strings.Contains(tmpl, "{param:"+name+"}") {
					holdsSecret = true
				}
			}
		}
		b, err := render(cf.Format, values)
		if err != nil {
			return fmt.Errorf("config_file %s: %w", cf.Path, err)
		}
		// A file that holds a secret is readable by its owner only.
		mode := uint32(0o644)
		if holdsSecret {
			mode = 0o600
		}
		f := contentFile(filepath.Base(path), b, mode)
		if err := p.addFile(PlannedFile{File: f, Dst: filepath.Clean(path), Base: base, Source: FromContent, Content: b, Secret: holdsSecret}); err != nil {
			return err
		}
	}
	return nil
}

// render writes values as one flat map. Both encoders sort the keys, so
// the same values give the same bytes, and both quote what needs quoting.
func render(format string, values map[string]string) ([]byte, error) {
	switch format {
	case "json":
		b, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("render json: %w", err)
		}
		return append(b, '\n'), nil
	case "yaml":
		b, err := yaml.Marshal(values)
		if err != nil {
			return nil, fmt.Errorf("render yaml: %w", err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("unknown format %q", format)
}
