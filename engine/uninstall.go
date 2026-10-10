package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ushineko/fynstall/manifest"
)

// Leftover is something still inside the install directory after an
// uninstall, which the program made and the install did not (spec 002 D5).
type Leftover struct {
	Path string
	Dir  bool
}

// Reason is why an uninstall runs. The programs it runs (uninstall hooks
// and the undos of run actions) see it as FYNSTALL_UNINSTALL_REASON, so a
// program can keep its registered state across an upgrade (spec 001 R17).
type Reason string

// The reasons an uninstall runs.
const (
	ReasonUninstall Reason = "uninstall"
	ReasonUpgrade   Reason = "upgrade"
)

// Uninstall replays r's journal in reverse: it removes what the install
// created and puts back what it replaced. Then it removes what r.Remove
// matches, the receipt, and the directories the install created that are
// now empty (R9d). Paths in r.Keep are never touched.
//
// It returns the leftovers: what is still inside a directory the install
// created under r.Root. They are listed, not deleted, because the install
// did not create them; RemoveLeftovers deletes them when a person says so.
//
// It can be run again after a failure: a file already removed or restored
// is skipped, and the receipt stays until every file is dealt with.
func Uninstall(r *Receipt, why Reason, report Reporter) ([]Leftover, error) {
	env := []string{"FYNSTALL_UNINSTALL_REASON=" + string(why)}
	j := &journal{root: r.Root, entries: r.Journal, report: report, env: env}
	if len(r.Hooks) > 0 {
		report.emit(Step, "Running the uninstall hooks of %s", r.App.Name)
		if err := runHooks(r.Hooks, env, report); err != nil {
			return nil, fmt.Errorf("uninstall %s: %w", r.App.Name, err)
		}
	}
	report.emit(Step, "Removing %s %s", r.App.Name, r.App.Version)
	if err := j.undoFiles(r.Keep); err != nil {
		return nil, fmt.Errorf("uninstall %s: %w", r.App.Name, err)
	}
	_ = os.Remove(ReceiptPath(r.Root))
	created := createdIn(r)
	var left []Leftover
	if t := openTree(r.Root); t != nil {
		if len(r.Remove) > 0 {
			t.removeMatching(r, created, report)
		}
		left = t.leftovers(r, created)
		t.close()
	}
	j.removeDirs(r.Keep, func(d string) bool { return within(d, created) })
	return left, nil
}

// RemoveLeftovers deletes exactly the leftovers Uninstall listed for r,
// deepest first, then the directories the install created that are now
// empty. A path that is not inside a directory the install created under
// r.Root, or that is kept, is refused. A directory that is no longer empty
// is left: it holds something made since the list was.
func RemoveLeftovers(r *Receipt, left []Leftover, report Reporter) error {
	created := createdIn(r)
	sorted := append([]Leftover(nil), left...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Path > sorted[b].Path })
	var errs []error
	if t := openTree(r.Root); t != nil {
		for _, l := range sorted {
			if !inside(l.Path, created) || kept(l.Path, r.Keep) {
				errs = append(errs, fmt.Errorf("%s is not a leftover of %s", l.Path, r.App.Name))
				continue
			}
			fi, err := t.lstat(l.Path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err == nil && fi.IsDir() != l.Dir {
				report.emit(Warn, "left %s: it changed since it was listed", l.Path)
				continue
			}
			if err == nil {
				err = t.remove(l.Path)
			}
			if err != nil {
				report.emit(Warn, "left %s: %v", l.Path, err)
				continue
			}
			report.emit(Detail, "removed %s", l.Path)
		}
		t.close()
	}
	j := &journal{root: r.Root, entries: r.Journal, report: report}
	j.removeDirs(r.Keep, nil)
	return errors.Join(errs...)
}

// createdIn returns the directories the install created at or under r.Root,
// outermost first, leaving out kept ones. Only these are searched for
// leftovers and for r.Remove: a directory the install found already there
// holds the person's own files.
func createdIn(r *Receipt) []string {
	var out []string
	for _, e := range r.Journal {
		if e.Op == OpMkdir && within(e.Path, []string{r.Root}) && !kept(e.Path, r.Keep) {
			out = append(out, e.Path)
		}
	}
	sort.Strings(out)
	var top []string
	for _, d := range out {
		if !inside(d, top) {
			top = append(top, d)
		}
	}
	return top
}

// inside reports whether path is strictly inside one of dirs.
func inside(path string, dirs []string) bool {
	for _, d := range dirs {
		if rel, err := filepath.Rel(d, path); err == nil && rel != "." && filepath.IsLocal(rel) {
			return true
		}
	}
	return false
}

// within reports whether path is one of dirs or inside one.
func within(path string, dirs []string) bool {
	return slices.Contains(dirs, path) || inside(path, dirs)
}

// tree is the install directory opened as an os.Root. Every walk and
// removal after the journal's goes through it, so a link put in place of a
// directory while the uninstaller runs cannot carry a removal outside the
// install directory.
type tree struct {
	root *os.Root
	dir  string
}

// openTree opens dir, or returns nil when there is nothing left of it.
func openTree(dir string) *tree {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil
	}
	return &tree{root: root, dir: dir}
}

func (t *tree) close() { _ = t.root.Close() }

func (t *tree) rel(p string) string {
	rel, err := filepath.Rel(t.dir, p)
	if err != nil {
		return p // refused by the Root as outside it
	}
	return rel
}

func (t *tree) lstat(p string) (fs.FileInfo, error) {
	fi, err := t.root.Lstat(t.rel(p))
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", p, err)
	}
	return fi, nil
}

// remove removes the file, link or empty directory p. A link is removed,
// never followed.
func (t *tree) remove(p string) error {
	if err := t.root.Remove(t.rel(p)); err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}

// walk calls fn for top and everything under it, with absolute paths.
// Links are reported, not followed. A directory that cannot be read is
// reported once and not entered.
func (t *tree) walk(top string, fn func(p string, d fs.DirEntry) error) {
	_ = fs.WalkDir(t.root.FS(), filepath.ToSlash(t.rel(top)), func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // reported by the call before, for the directory itself
		}
		return fn(filepath.Join(t.dir, filepath.FromSlash(rel)), d)
	})
}

// removeMatching removes what r.Remove matches inside the directories the
// install created (spec 002 D5). A matching directory is emptied file by
// file, deepest first, and removed when empty. Kept paths and fynstall's
// own directory are never touched.
func (t *tree) removeMatching(r *Receipt, created []string, report Reporter) {
	matches := func(q string) bool {
		rel := filepath.ToSlash(t.rel(q))
		for _, p := range r.Remove {
			if manifest.MatchPattern(p, rel) {
				return true
			}
		}
		return false
	}
	for _, top := range created {
		t.walk(top, func(q string, d fs.DirEntry) error {
			switch {
			case q == filepath.Join(r.Root, MetaDir) || kept(q, r.Keep):
				return skip(d)
			case !matches(q):
				return nil
			case d.IsDir():
				t.removeTree(q, r.Keep, report)
				return filepath.SkipDir
			}
			t.removeOne(q, report)
			return nil
		})
	}
}

func skip(d fs.DirEntry) error {
	if d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// removeTree empties dir file by file, deepest first, and removes each
// directory once it is empty. It is not os.RemoveAll: it leaves kept paths
// and anything it cannot remove.
func (t *tree) removeTree(dir string, keep []string, report Reporter) {
	var paths []string
	t.walk(dir, func(q string, d fs.DirEntry) error {
		if kept(q, keep) {
			return skip(d)
		}
		paths = append(paths, q)
		return nil
	})
	for i := len(paths) - 1; i >= 0; i-- {
		t.removeOne(paths[i], report)
	}
}

func (t *tree) removeOne(p string, report Reporter) {
	err := t.remove(p)
	switch {
	case err == nil:
		report.emit(Detail, "removed %s", p)
	case !errors.Is(err, fs.ErrNotExist):
		report.emit(Warn, "left %s: %v", p, err)
	}
}

// leftovers lists what is still inside the directories the install created
// under r.Root, other than those directories and kept paths, sorted.
func (t *tree) leftovers(r *Receipt, created []string) []Leftover {
	mine := map[string]bool{}
	for _, e := range r.Journal {
		if e.Op == OpMkdir {
			mine[e.Path] = true
		}
	}
	var out []Leftover
	for _, top := range created {
		t.walk(top, func(q string, d fs.DirEntry) error {
			switch {
			case kept(q, r.Keep):
				return skip(d)
			case !mine[q]:
				out = append(out, Leftover{Path: q, Dir: d.IsDir()})
			}
			return nil
		})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Path < out[b].Path })
	return out
}

// LeftoverFiles counts the leftovers that are not directories.
func LeftoverFiles(left []Leftover) int {
	n := 0
	for _, l := range left {
		if !l.Dir {
			n++
		}
	}
	return n
}

// String is the leftover's path, with a trailing separator for a directory.
func (l Leftover) String() string {
	if l.Dir {
		return strings.TrimSuffix(l.Path, string(filepath.Separator)) + string(filepath.Separator)
	}
	return l.Path
}
