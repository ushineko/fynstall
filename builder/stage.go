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

// stage lists every payload file in c with its hash and mode, and returns
// the manifest. Files are sorted by destination, so the same tree always
// gives the same manifest (R2, R3).
func stage(c *config.Config, runtimeVersion string) (*manifest.Manifest, []staged, error) {
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
		src := filepath.Join(c.Dir, filepath.FromSlash(e.Src))
		mode := config.ParseMode(e.Mode)
		fi, err := os.Lstat(src)
		if err != nil {
			return nil, nil, fmt.Errorf("payload: %w", err)
		}
		switch {
		case fi.Mode().IsRegular():
			dst := e.Dst
			if strings.HasSuffix(dst, "/") {
				dst += filepath.Base(src)
			}
			if err := add(src, path.Clean(dst), mode); err != nil {
				return nil, nil, err
			}
		case fi.IsDir():
			if err := walk(src, e, func(file, rel string) error {
				return add(file, path.Join(e.Dst, rel), mode)
			}); err != nil {
				return nil, nil, err
			}
		default:
			return nil, nil, fmt.Errorf("payload: %s is not a regular file or directory", src)
		}
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("payload: no files")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	m := &manifest.Manifest{
		Schema: manifest.Schema, RuntimeVersion: runtimeVersion,
		App:             manifest.App{ID: c.App.ID, Name: c.App.Name, Version: c.App.Version, Publisher: c.App.Publisher},
		Scopes:          c.Install.Scopes,
		Dirs:            c.Install.Dir,
		KeepOnUninstall: c.Integration.KeepOnUninstall,
	}
	for _, f := range files {
		m.Files = append(m.Files, f.File)
	}
	return m, files, nil
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
			return fmt.Errorf("payload: %w", err)
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
			return fmt.Errorf("payload: %s is not a regular file (symlinks are not followed)", p)
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
