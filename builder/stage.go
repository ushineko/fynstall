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
	s := &stager{target: target, seen: map[string]string{}}
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
			if err := s.add(src, path.Clean(dst), mode); err != nil {
				return nil, nil, nil, err
			}
		case fi.IsDir():
			if err := s.dir(src, e, mode); err != nil {
				return nil, nil, nil, err
			}
		case fi.Mode()&os.ModeSymlink != 0:
			return nil, nil, nil, fmt.Errorf("payload: %s is a symlink; links are kept only inside a directory entry", src)
		default:
			return nil, nil, nil, fmt.Errorf("payload: %s is not a regular file or directory", src)
		}
	}
	files, links := s.files, s.links
	if err := underLinks(files, links); err != nil {
		return nil, nil, nil, err
	}
	seen := s.seen
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
	sort.Slice(links, func(i, j int) bool { return links[i].Path < links[j].Path })
	m.Symlinks = links
	m.UninstallRemove = c.Uninstall.Remove
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
		if err := stageAction(a, m, seen, expand); err != nil {
			return nil, nil, nil, err
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

// stageAction adds a to the manifest. A program it runs must be a payload
// file for this target, which is known only once directories expand.
func stageAction(a config.Action, m *manifest.Manifest, payload map[string]string, expand func(string) (string, error)) error {
	exec := func(kind, tmpl string) (string, error) {
		e, err := expand(tmpl)
		if err != nil {
			return "", err
		}
		e = path.Clean(e)
		if _, ok := payload[e]; !ok {
			return "", fmt.Errorf("%s: exec %s is not a payload file for %s", kind, tmpl, m.Target)
		}
		return e, nil
	}
	switch {
	case a.ConfigFile != nil:
		m.ConfigFiles = append(m.ConfigFiles, manifest.ConfigFile{
			Path: a.ConfigFile.Path, Format: config.FileFormat(a.ConfigFile), Values: a.ConfigFile.Values,
		})
	case a.Service != nil:
		s := a.Service
		e, err := exec("service "+s.Name, s.Exec)
		if err != nil {
			return err
		}
		m.Actions = append(m.Actions, manifest.Action{Service: &manifest.Service{
			Name: s.Name, Description: s.Description, Exec: e, Args: s.Args, Start: s.Starts(), Restart: s.RestartPolicy(),
		}})
	case a.Run != nil:
		r := a.Run
		e, err := exec("run", r.Exec)
		if err != nil {
			return err
		}
		m.Actions = append(m.Actions, manifest.Action{Run: &manifest.Run{
			Hook: r.Hook(), Exec: e, Args: r.Args, Undo: r.Undo.Args, NoUndo: r.Undo.None, ContinueOnError: r.ContinueOnError,
		}})
	case a.Migrate != nil:
		m.Actions = append(m.Actions, manifest.Action{Migrate: &manifest.Migrate{From: a.Migrate.From, To: a.Migrate.To}})
	}
	return nil
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

// walk calls file for each regular file and link for each symlink under
// root that no exclude pattern matches, with its slash path relative to
// root after prefix. A pattern matches a file's name or that path. Links
// are not followed: link decides what each one becomes.
func walk(root, prefix string, e config.Entry, file, link func(p, rel string) error) error {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}
		rel = path.Join(prefix, filepath.ToSlash(rel))
		if rel != prefix && excluded(e.Exclude, d.Name(), rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			return nil
		case d.Type().IsRegular():
			return file(p, rel)
		case d.Type()&fs.ModeSymlink != 0:
			return link(p, rel)
		default:
			return fmt.Errorf("%s is not a regular file, directory or symlink", p)
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

// stager collects the payload of one target.
type stager struct {
	target string
	files  []staged
	links  []manifest.Symlink
	// seen maps each destination to its source, to find two sources for one
	// destination.
	seen map[string]string
}

func (s *stager) claim(src, dst string) error {
	if msg := config.CheckDst(dst); msg != "" {
		return fmt.Errorf("%s: %s", src, msg)
	}
	if other, dup := s.seen[dst]; dup {
		return fmt.Errorf("%s and %s both install to %s", other, src, dst)
	}
	s.seen[dst] = src
	return nil
}

func (s *stager) add(src, dst string, mode uint32) error {
	if err := s.claim(src, dst); err != nil {
		return err
	}
	f, err := describe(src, dst, mode)
	if err != nil {
		return err
	}
	s.files = append(s.files, f)
	return nil
}

// dir stages a directory entry.
func (s *stager) dir(root string, e config.Entry, mode uint32) error {
	file := func(p, rel string) error { return s.add(p, path.Join(e.Dst, rel), mode) }
	var link func(p, rel string) error
	link = func(p, rel string) error {
		resolved, isDir, err := checkLink(root, p, e)
		if err != nil {
			return err // walk adds the prefix
		}
		dst := path.Join(e.Dst, rel)
		if !strings.HasPrefix(s.target, "windows/") {
			if err := s.claim(p, dst); err != nil {
				return err
			}
			target, _ := os.Readlink(p) // checkLink has read it
			s.links = append(s.links, manifest.Symlink{Path: dst, Target: filepath.ToSlash(target)})
			return nil
		}
		// Making a link on Windows needs a privilege a normal user does not
		// have, so the target gets a copy of what the link points at. The
		// linker stores identical files once, so the installer does not
		// grow (spec 002 D4a).
		if !isDir {
			return s.add(resolved, dst, mode)
		}
		return walk(resolved, rel, e, file, link)
	}
	return walk(root, "", e, file, link)
}

// checkLink holds the symlink p, in the entry rooted at root, to the
// rules of spec 002 D4a, and returns what it resolves to and whether that
// is a directory. Its target is relative, exists, is inside the entry and
// not excluded from it. A directory it points at holds no directory link of
// its own, so a copy of it is finite.
func checkLink(root, p string, e config.Entry) (string, bool, error) {
	target, err := os.Readlink(p)
	if err != nil {
		return "", false, fmt.Errorf("read link %s: %w", p, err)
	}
	if filepath.IsAbs(target) || path.IsAbs(filepath.ToSlash(target)) {
		return "", false, fmt.Errorf("%s links to %s, an absolute path; a link in the payload must point inside its entry", p, target)
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false, fmt.Errorf("%s links to %s, which does not exist", p, target)
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve %s: %w", root, err)
	}
	in, err := filepath.Rel(base, resolved)
	if err != nil || !filepath.IsLocal(in) {
		return "", false, fmt.Errorf("%s links to %s, which is outside the payload entry %s", p, target, root)
	}
	in = filepath.ToSlash(in)
	for sub := in; sub != "."; sub = path.Dir(sub) {
		if excluded(e.Exclude, path.Base(sub), sub) {
			return "", false, fmt.Errorf("%s links to %s, which the entry excludes", p, target)
		}
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", resolved, err)
	}
	if !fi.IsDir() {
		return resolved, false, nil
	}
	var nested string
	err = filepath.WalkDir(resolved, func(q string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return err
		}
		if st, err := os.Stat(q); err == nil && st.IsDir() {
			nested = q
			return filepath.SkipAll
		}
		return nil
	})
	switch {
	case err != nil:
		return "", false, fmt.Errorf("check %s: %w", p, err)
	case nested != "":
		return "", false, fmt.Errorf("%s links to the directory %s, which holds the directory link %s; a linked directory may not hold another", p, target, nested)
	}
	return resolved, true, nil
}

// underLinks refuses a file installed under a payload link: the link would
// carry the write into whatever it points at.
func underLinks(files []staged, links []manifest.Symlink) error {
	for _, l := range links {
		for _, f := range files {
			if strings.HasPrefix(f.Path, l.Path+"/") {
				return fmt.Errorf("payload: %s installs under the link %s", f.Path, l.Path)
			}
		}
	}
	return nil
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
