/*
Package engine plans, applies and removes an install. It has no UI: both
front ends drive it and print or draw the events it reports (spec 001, R10).

Every change is in the Plan before any is made (R6). Apply records each
change in a journal as it makes it, and the journal is what the receipt
holds and what the uninstaller replays in reverse (R8, R9a, R9d).
*/
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// Names inside an install directory that fynstall owns.
const (
	MetaDir       = ".fynstall"
	ReceiptName   = "receipt.json"
	BackupDir     = "backup"
	UninstallName = "uninstall"
)

// Source says where Apply reads a planned file from.
type Source int

const (
	// FromPayload reads the file from the embedded payload.
	FromPayload Source = iota
	// FromUninstaller writes the uninstaller binary.
	FromUninstaller
	// FromContent writes PlannedFile.Content, made when the plan was: a
	// desktop entry, whose Exec names the install directory.
	FromContent
)

// PlannedFile is one file Apply will write.
type PlannedFile struct {
	manifest.File
	// Dst is the absolute destination.
	Dst string
	// Base is the directory Dst must stay inside: the install directory,
	// or {data} or {bin} for what goes outside it.
	Base string
	// Exists is true when something is already at Dst; Apply backs it up
	// and the uninstaller puts it back.
	Exists  bool
	Source  Source
	Content []byte
}

// PlannedLink is a symlink Apply will make.
type PlannedLink struct {
	Dst    string
	Target string
	Base   string
	Exists bool
}

// Plan is everything an install will change, computed without changing
// anything.
type Plan struct {
	Manifest *manifest.Manifest
	Scope    string
	// Root is the absolute install directory.
	Root string
	// Index is the install index file for this app and scope.
	Index string
	// Keep are the expanded keep_on_uninstall paths.
	Keep []string
	// Dirs are the directories that do not exist yet, parents first.
	Dirs  []string
	Files []PlannedFile
	Links []PlannedLink
	// RefreshMenu is true when the install adds launcher entries or icons,
	// so the front end asks the desktop to read them again.
	RefreshMenu bool
}

// Options are the choices a front end passes to NewPlan.
type Options struct {
	Scope string
	// Root overrides the manifest's install directory for Scope.
	Root string
	// Env reads environment variables; nil means os.Getenv.
	Env func(string) string
	// Uninstaller is the uninstaller binary Apply installs beside the program.
	Uninstaller []byte
}

// ErrInstalled is returned by NewPlan when the index already has this app.
var ErrInstalled = errors.New("already installed")

// NewPlan works out what installing m would change.
func NewPlan(m *manifest.Manifest, o Options) (*Plan, error) {
	if o.Env == nil {
		o.Env = os.Getenv
	}
	if o.Scope == "" && len(m.Scopes) > 0 {
		o.Scope = m.Scopes[0]
	}
	if !slices.Contains(m.Scopes, o.Scope) {
		return nil, fmt.Errorf("scope %q is not offered by this installer (offered: %s)", o.Scope, strings.Join(m.Scopes, ", "))
	}
	vars, err := Vars(m, o.Scope, o.Env)
	if err != nil {
		return nil, err
	}
	p := &Plan{Manifest: m, Scope: o.Scope}

	root := o.Root
	if root == "" {
		if root, err = manifest.Expand(m.Dirs[o.Scope], vars); err != nil {
			return nil, fmt.Errorf("install directory: %w", err)
		}
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("install directory %q is not an absolute path", root)
	}
	p.Root = filepath.Clean(root)
	p.Index = indexPath(vars, m.App.ID)
	if _, err := os.Lstat(p.Index); err == nil {
		return nil, fmt.Errorf("%s %w (index %s)", m.App.Name, ErrInstalled, p.Index)
	}
	if _, err := os.Lstat(filepath.Join(p.Root, MetaDir)); err == nil {
		return nil, fmt.Errorf("%s already holds a fynstall install that is not in the index; remove it first", p.Root)
	}
	for _, k := range m.KeepOnUninstall {
		path, err := manifest.Expand(k, vars)
		if err != nil {
			return nil, fmt.Errorf("keep_on_uninstall: %w", err)
		}
		p.Keep = append(p.Keep, filepath.Clean(path))
	}

	for _, f := range m.Files {
		if err := p.addFile(PlannedFile{File: f, Dst: p.inRoot(f.Path), Base: p.Root, Source: FromPayload}); err != nil {
			return nil, err
		}
	}
	if err := p.addFile(PlannedFile{
		File: contentFile(UninstallName, o.Uninstaller, 0o755), Dst: p.inRoot(UninstallName),
		Base: p.Root, Source: FromUninstaller,
	}); err != nil {
		return nil, err
	}
	if err := p.addIntegration(vars); err != nil {
		return nil, err
	}

	dirs := []string{p.Root, filepath.Join(p.Root, MetaDir), filepath.Join(p.Root, MetaDir, BackupDir), filepath.Dir(p.Index)}
	for _, f := range p.Files {
		dirs = append(dirs, filepath.Dir(f.Dst))
	}
	for _, l := range p.Links {
		dirs = append(dirs, filepath.Dir(l.Dst))
	}
	if p.Dirs, err = missingDirs(dirs); err != nil {
		return nil, err
	}
	return p, nil
}

// Vars are the placeholder values for m in scope: the platform's locations
// and the app's id, name and version.
func Vars(m *manifest.Manifest, scope string, env func(string) string) (map[string]string, error) {
	v, err := platform.Vars(scope, env)
	if err != nil {
		return nil, fmt.Errorf("resolve paths: %w", err)
	}
	v["id"], v["name"], v["version"] = m.App.ID, m.App.Name, m.App.Version
	return v, nil
}

func indexPath(vars map[string]string, id string) string {
	return filepath.Join(platform.IndexDir(vars), id+".json")
}

func (p *Plan) inRoot(rel string) string {
	return filepath.Join(p.Root, filepath.FromSlash(rel))
}

// addIntegration plans the icons, desktop entries and links, which go
// outside the install directory: under {data} and {bin}.
func (p *Plan) addIntegration(vars map[string]string) error {
	m := p.Manifest
	data, bin := vars["data"], vars["bin"]
	for _, ic := range m.Icons {
		f := manifest.File{Path: ic.Path(), Size: ic.Bytes, SHA256: ic.SHA256, Mode: 0o644}
		dst := filepath.Join(data, filepath.FromSlash(platform.IconPath(m.App.ID, ic.Size)))
		if err := p.addFile(PlannedFile{File: f, Dst: dst, Base: data, Source: FromPayload}); err != nil {
			return err
		}
	}
	for _, d := range m.Desktop {
		icon := ""
		if d.Icon {
			icon = m.App.ID
		}
		b := platform.RenderDesktop(d, p.inRoot(d.Exec), icon)
		dst := filepath.Join(data, filepath.FromSlash(platform.DesktopPath(d.ID)))
		if err := p.addFile(PlannedFile{File: contentFile(d.ID+".desktop", b, 0o644), Dst: dst, Base: data, Source: FromContent, Content: b}); err != nil {
			return err
		}
	}
	for _, l := range m.Links {
		pl := PlannedLink{Dst: filepath.Join(bin, l.Name), Target: p.inRoot(l.Target), Base: bin}
		exists, err := p.check(pl.Dst, pl.Base)
		if err != nil {
			return err
		}
		pl.Exists = exists
		p.Links = append(p.Links, pl)
	}
	p.RefreshMenu = len(m.Icons) > 0 || len(m.Desktop) > 0
	return nil
}

func contentFile(name string, b []byte, mode uint32) manifest.File {
	sum := sha256.Sum256(b)
	return manifest.File{Path: name, Size: int64(len(b)), SHA256: hex.EncodeToString(sum[:]), Mode: mode}
}

func (p *Plan) addFile(pf PlannedFile) error {
	exists, err := p.check(pf.Dst, pf.Base)
	if err != nil {
		return err
	}
	pf.Exists = exists
	p.Files = append(p.Files, pf)
	return nil
}

// check holds dst inside base and reports whether something is there
// already. A directory in the way is an error.
func (p *Plan) check(dst, base string) (bool, error) {
	if err := Contained(base, dst); err != nil {
		return false, err
	}
	switch fi, err := os.Lstat(dst); {
	case err == nil && fi.IsDir():
		return false, fmt.Errorf("%s is a directory, and the install has a file there", dst)
	case err == nil:
		return true, nil
	case !errors.Is(err, fs.ErrNotExist):
		return false, fmt.Errorf("check %s: %w", dst, err)
	}
	return false, nil
}

// Contained returns an error unless path, with every symlink in its
// existing part resolved, is inside root resolved the same way. A link in
// the install directory must not carry a write somewhere else.
func Contained(root, path string) error {
	r, err := resolve(root)
	if err != nil {
		return err
	}
	q, err := resolve(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(r, q)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%s resolves to %s, outside the directory it belongs in, %s", path, q, r)
	}
	return nil
}

// resolve is filepath.EvalSymlinks on the longest existing prefix of p,
// with the rest appended: p need not exist yet.
func resolve(p string) (string, error) {
	p = filepath.Clean(p)
	var rest []string
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(append([]string{p}, rest...)...), nil
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}

// missingDirs returns each directory in dirs, and each of their ancestors,
// that does not exist, parents first. Something that exists and is not a
// directory is an error.
func missingDirs(dirs []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		for d = filepath.Clean(d); !seen[d]; d = filepath.Dir(d) {
			seen[d] = true
			fi, err := os.Stat(d)
			if err == nil {
				if !fi.IsDir() {
					return nil, fmt.Errorf("%s is not a directory", d)
				}
				break
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("check %s: %w", d, err)
			}
			out = append(out, d)
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], string(filepath.Separator)), strings.Count(out[j], string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out, nil
}
