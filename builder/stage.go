package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ushineko/fynstall/config"
	"github.com/ushineko/fynstall/manifest"
)

// staged is a payload file: where it comes from and its manifest entry.
type staged struct {
	src string
	manifest.File
}

// stage lists every payload file of c for target with its hash and mode,
// and returns the manifest and the generated files (the resized icons),
// keyed by their path in the embedded payload. Entries whose targets do not
// match are left out, and the build-time placeholders are resolved for
// target (spec 002 D1a). Files are sorted by destination, so the same tree
// always gives the same manifest (R2, R3).
func stage(c *config.Config, runtimeVersion, target string) (*manifest.Manifest, []staged, map[string][]byte, error) {
	vars := manifest.BuildVars(target)
	expand := func(tmpl string) (string, error) {
		s, err := manifest.Expand(tmpl, vars)
		if err != nil {
			return "", fmt.Errorf("target %s: %w", target, err)
		}
		return s, nil
	}
	var files []staged
	seen := map[string]string{}
	add := func(src, dst string, mode uint32) error {
		if msg := config.CheckDst(dst); msg != "" {
			return fmt.Errorf("%s: %s", src, msg)
		}
		if other, dup := seen[dst]; dup {
			return fmt.Errorf("%s and %s both install to %s", other, src, dst)
		}
		seen[dst] = src
		f, err := describe(src, dst, mode)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	}

	for _, e := range c.Payload {
		if !e.Applies(target) {
			continue
		}
		srcRel, err := expand(e.Src)
		if err != nil {
			return nil, nil, nil, err
		}
		if e.Dst, err = expand(e.Dst); err != nil {
			return nil, nil, nil, err
		}
		src := filepath.Join(c.Dir, filepath.FromSlash(srcRel))
		mode := config.ParseMode(e.Mode)
		fi, err := os.Lstat(src)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("payload: %w", err)
		}
		switch {
		case fi.Mode().IsRegular():
			dst := e.Dst
			if strings.HasSuffix(dst, "/") {
				dst += filepath.Base(src)
			}
			if err := add(src, path.Clean(dst), mode); err != nil {
				return nil, nil, nil, err
			}
		case fi.IsDir():
			if err := walk(src, e, func(file, rel string) error {
				return add(file, path.Join(e.Dst, rel), mode)
			}); err != nil {
				return nil, nil, nil, err
			}
		default:
			return nil, nil, nil, fmt.Errorf("payload: %s is not a regular file or directory", src)
		}
	}
	if len(files) == 0 {
		return nil, nil, nil, fmt.Errorf("payload: no files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	m := &manifest.Manifest{
		Schema: manifest.Schema, RuntimeVersion: runtimeVersion, Target: target,
		App:             manifest.App{ID: c.App.ID, Name: c.App.Name, Version: c.App.Version, Publisher: c.App.Publisher},
		Scopes:          c.Install.Scopes,
		Dirs:            c.Install.Dir,
		KeepOnUninstall: c.Integration.KeepOnUninstall,
	}
	for _, f := range files {
		m.Files = append(m.Files, f.File)
	}
	for _, p := range c.Parameters {
		m.Parameters = append(m.Parameters, manifest.Parameter(p))
	}
	for field, rel := range map[*string]string{&m.Licence: c.App.Licence, &m.Welcome: c.UI.Welcome} {
		if rel == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", rel, err)
		}
		*field = string(b)
	}
	if c.UI.Launch != "" {
		launch, err := expand(c.UI.Launch)
		if err != nil {
			return nil, nil, nil, err
		}
		if _, ok := seen[path.Clean(launch)]; !ok {
			return nil, nil, nil, fmt.Errorf("ui.launch: %s is not a payload file for %s", launch, target)
		}
		m.Launch = path.Clean(launch)
	}
	for _, a := range c.Actions {
		if a.ConfigFile != nil {
			m.ConfigFiles = append(m.ConfigFiles, manifest.ConfigFile{
				Path: a.ConfigFile.Path, Format: config.FileFormat(a.ConfigFile), Values: a.ConfigFile.Values,
			})
		}
	}
	if err := integrate(c, m, seen, expand); err != nil {
		return nil, nil, nil, err
	}
	var generated map[string][]byte
	if c.App.Icon != "" {
		var err error
		if m.Icons, generated, err = icons(filepath.Join(c.Dir, filepath.FromSlash(c.App.Icon))); err != nil {
			return nil, nil, nil, err
		}
	}
	return m, files, generated, nil
}

// integrate adds the links and desktop entries, each of which must name a
// payload file. Directory entries only expand here, so this is the first
// point at which that can be checked.
func integrate(c *config.Config, m *manifest.Manifest, payload map[string]string, expand func(string) (string, error)) error {
	for _, l := range c.Integration.PathLinks {
		l, err := expand(l)
		if err != nil {
			return err
		}
		target := path.Clean(l)
		if _, ok := payload[target]; !ok {
			return fmt.Errorf("integration.path_links: %s is not a payload file for %s", l, m.Target)
		}
		m.Links = append(m.Links, manifest.Link{Name: path.Base(target), Target: target})
	}
	for _, d := range c.Integration.Desktop {
		e, err := expand(d.Exec)
		if err != nil {
			return err
		}
		exec := path.Clean(e)
		if _, ok := payload[exec]; !ok {
			return fmt.Errorf("integration.desktop %s: exec %s is not a payload file for %s", d.ID, e, m.Target)
		}
		m.Desktop = append(m.Desktop, manifest.Desktop{
			ID: d.ID, Name: d.Name, Comment: d.Comment, Exec: exec, Args: d.Args,
			Categories: d.Categories, Terminal: d.Terminal, Icon: c.App.Icon != "",
		})
	}
	return nil
}

// walk calls fn for each regular file under root that no exclude pattern
// matches, with its slash path relative to root. A pattern matches a file's
// name or its relative path. Symlinks are refused rather than followed: a
// link's target is a decision the config should make visibly.
func walk(root string, e config.Entry, fn func(file, rel string) error) error {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && excluded(e.Exclude, d.Name(), rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			return nil
		case d.Type().IsRegular():
			return fn(p, rel)
		default:
			return fmt.Errorf("%s is not a regular file (symlinks are not followed)", p)
		}
	})
	if err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	return nil
}

func excluded(patterns []string, name, rel string) bool {
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, name); ok {
			return true
		}
		if ok, _ := path.Match(pat, rel); ok {
			return true
		}
	}
	return false
}

func describe(src, dst string, mode uint32) (staged, error) {
	f, err := os.Open(src)
	if err != nil {
		return staged{}, fmt.Errorf("payload: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return staged{}, fmt.Errorf("payload: read %s: %w", src, err)
	}
	if mode == 0 {
		if mode, err = manifest.DetectMode(src); err != nil {
			return staged{}, err
		}
	}
	return staged{src: src, File: manifest.File{Path: dst, Size: n, SHA256: hex.EncodeToString(h.Sum(nil)), Mode: mode}}, nil
}
