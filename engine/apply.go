package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// EventKind sorts events for a front end.
type EventKind int

const (
	// Step starts a stage of the work.
	Step EventKind = iota
	// Detail is one file or directory within a stage.
	Detail
	// Warn is a problem that did not stop the work.
	Warn
	// Progress is how far the copying is: Counts and the item in Text.
	Progress
)

// Counts are how far an install's copying is.
type Counts struct {
	Files, FilesTotal int
	Bytes, BytesTotal int64
}

// progressStep is how many bytes of one large file pass between Progress
// events, so a big file moves the bar while it is copied.
const progressStep = 1 << 20

// Event is a progress report.
type Event struct {
	Kind EventKind
	Text string
	// Step is a Step event's position in Steps, or -1 for a step outside
	// it (the undo of a failed install).
	Step int
	// Counts is set on a Progress event.
	Counts Counts
}

// The steps of an install, in order. Linking is there only when the
// install makes links.
const (
	// StepReplace is the installed version's uninstaller removing it, in
	// an upgrade, repair or downgrade. The front end runs it, before Apply.
	StepReplace = "Removing the installed version"
	StepDirs    = "Creating directories"
	StepFiles   = "Copying files"
	StepLinks   = "Linking"
	// StepShell is the Start Menu, the Uninstall entry and PATH, on Windows.
	StepShell   = "Registering with Windows"
	StepActions = "Configuring"
	StepRecord  = "Recording the install"
)

// Steps names the steps Apply will report for p, so a front end can show
// them before the install starts.
func Steps(p *Plan) []string {
	return stepNames(p.Replaces != "", len(p.Links) > 0, p.HasShellIntegration(), len(p.Actions) > 0)
}

// ManifestSteps is Steps before there is a plan: the steps depend only on
// whether an install is replaced, and whether the manifest has links and
// actions other than uninstall hooks.
func ManifestSteps(m *manifest.Manifest, replaces bool) []string {
	actions := false
	for _, a := range m.Actions {
		actions = actions || a.Run == nil || !a.Run.Hook
	}
	// Windows has no links, and every install there has its registry entry.
	shell := platform.Integration == platform.WindowsShell
	return stepNames(replaces, len(m.Links) > 0 && platform.Integration == platform.XDG, shell, actions)
}

func stepNames(replaces, links, shell, actions bool) []string {
	var s []string
	if replaces {
		s = append(s, StepReplace)
	}
	s = append(s, StepDirs, StepFiles)
	if links {
		s = append(s, StepLinks)
	}
	if shell {
		s = append(s, StepShell)
	}
	if actions {
		s = append(s, StepActions)
	}
	return append(s, StepRecord)
}

// Reporter receives events. It is called on the goroutine doing the work.
type Reporter func(Event)

func (r Reporter) emit(k EventKind, format string, a ...any) {
	if r != nil {
		r(Event{Kind: k, Text: fmt.Sprintf(format, a...), Step: -1})
	}
}

// step reports the start of the step named name in steps.
func (r Reporter) step(steps []string, name string) {
	if r != nil {
		r(Event{Kind: Step, Text: name, Step: slices.Index(steps, name)})
	}
}

// Apply makes the changes in p. payload holds the files named by the
// manifest and uninstaller is the uninstaller binary; every file's sha256 is
// checked before it is moved into place (R7).
//
// On any failure, Apply undoes what it did, so the system is as it was, and
// returns the error, joined with any problem the undo had (R7).
func Apply(ctx context.Context, p *Plan, payload fs.FS, uninstaller []byte, report Reporter) (rcpt *Receipt, err error) {
	j := &journal{root: p.Root, report: report}
	steps := Steps(p)
	j.counts.FilesTotal = len(p.Files)
	for _, f := range p.Files {
		j.counts.BytesTotal += f.Size
	}
	defer func() {
		if err != nil {
			report.emit(Step, "Undoing the partial install")
			if uerr := j.undo(nil); uerr != nil {
				err = errors.Join(err, fmt.Errorf("undo: %w", uerr))
			}
		}
	}()

	report.step(steps, StepDirs)
	for _, d := range p.Dirs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cancelled: %w", err)
		}
		perm := os.FileMode(0o755)
		if d == backupDir(p.Root) {
			perm = 0o700
		}
		if err := os.Mkdir(d, perm); err != nil {
			return nil, fmt.Errorf("create directory: %w", err)
		}
		j.add(Entry{Op: OpMkdir, Path: d})
		report.emit(Detail, "%s%c", d, filepath.Separator)
	}

	report.step(steps, StepFiles)
	for _, f := range p.Files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cancelled: %w", err)
		}
		if err := j.writePlanned(f, payload, uninstaller); err != nil {
			return nil, err
		}
		j.counts.Files++
		j.progress(f.Dst)
		report.emit(Detail, "%s", f.Dst)
	}
	for _, l := range p.Symlinks {
		if err := j.link(l.Dst, l.Target, l.Exists); err != nil {
			return nil, err
		}
		report.emit(Detail, "%s -> %s", l.Dst, l.Target)
	}

	if len(p.Links) > 0 {
		report.step(steps, StepLinks)
	}
	for _, l := range p.Links {
		if err := j.link(l.Dst, l.Target, l.Exists); err != nil {
			return nil, err
		}
		report.emit(Detail, "%s -> %s", l.Dst, l.Target)
	}

	if p.HasShellIntegration() {
		report.step(steps, StepShell)
		if err := j.applyShell(p); err != nil {
			return nil, err
		}
	}

	if len(p.Actions) > 0 {
		report.step(steps, StepActions)
	}
	for _, a := range p.Actions {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cancelled: %w", err)
		}
		if err := j.apply(ctx, a); err != nil {
			return nil, err
		}
	}

	report.step(steps, StepRecord)
	rcpt = &Receipt{
		Schema: ReceiptSchema, RuntimeVersion: p.Manifest.RuntimeVersion,
		App: p.Manifest.App, Scope: p.Scope, Root: p.Root,
		Uninstaller: filepath.Join(p.Root, UninstallName), Index: p.Index, Keep: p.Keep,
		RefreshMenu: p.RefreshMenu, Remove: p.Manifest.UninstallRemove, Hooks: p.Hooks,
	}
	for _, d := range p.Manifest.Parameters {
		if d.Secret {
			rcpt.Secrets = append(rcpt.Secrets, d.Name)
			continue
		}
		if rcpt.Parameters == nil {
			rcpt.Parameters = map[string]string{}
		}
		rcpt.Parameters[d.Name] = p.Params[d.Name]
	}
	ix, err := marshal(Index{Root: p.Root, Uninstaller: rcpt.Uninstaller, Version: p.Manifest.App.Version, Scope: p.Scope})
	if err != nil {
		return nil, err
	}
	if err := j.write(p.Index, bytes.NewReader(ix), "", 0o644, false); err != nil {
		return nil, err
	}
	rcpt.Journal = j.entries
	b, err := marshal(rcpt)
	if err != nil {
		return nil, err
	}
	// The receipt is not in its own journal: the uninstaller removes it
	// explicitly, after everything it lists.
	if err := atomicWrite(ReceiptPath(p.Root), bytes.NewReader(b), "", 0o644); err != nil {
		return nil, err
	}
	return rcpt, nil
}

// journal records changes as they are made and can undo them.
type journal struct {
	// env is added to the environment of the programs an undo runs.
	env     []string
	root    string
	entries []Entry
	report  Reporter
	// counts is how far the copying is, for Progress events.
	counts Counts
}

func (j *journal) progress(item string) {
	if j.report != nil {
		j.report(Event{Kind: Progress, Text: item, Counts: j.counts, Step: -1})
	}
}

// counting passes a file's bytes through and counts them into the journal,
// reporting every progressStep bytes so a large file moves the bar.
type counting struct {
	r     io.Reader
	j     *journal
	item  string
	since int
}

func (c *counting) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.j.counts.Bytes += int64(n)
	c.since += n
	if c.since >= progressStep {
		c.since = 0
		c.j.progress(c.item)
	}
	return n, err //nolint:wrapcheck // a Read must return the reader's own io.EOF
}

func (j *journal) add(e Entry) { j.entries = append(j.entries, e) }

func (j *journal) writePlanned(f PlannedFile, payload fs.FS, uninstaller []byte) error {
	count := func(r io.Reader) io.Reader { return &counting{r: r, j: j, item: f.Dst} }
	switch f.Source {
	case FromUninstaller:
		return j.write(f.Dst, count(bytes.NewReader(uninstaller)), f.SHA256, os.FileMode(f.Mode), f.Exists)
	case FromContent:
		return j.write(f.Dst, count(bytes.NewReader(f.Content)), f.SHA256, os.FileMode(f.Mode), f.Exists)
	}
	src, err := payload.Open(f.Path)
	if err != nil {
		return fmt.Errorf("open payload %s: %w", f.Path, err)
	}
	defer func() { _ = src.Close() }()
	return j.write(f.Dst, count(src), f.SHA256, os.FileMode(f.Mode), f.Exists)
}

// write puts r at dst. What is already there is saved first and the change
// is journalled as a replace.
func (j *journal) write(dst string, r io.Reader, sum string, mode os.FileMode, exists bool) error {
	return j.replace(dst, exists, func() error { return atomicWrite(dst, r, sum, mode) })
}

// link makes dst a symlink to target, saving what was there.
func (j *journal) link(dst, target string, exists bool) error {
	return j.replace(dst, exists, func() error { return atomicSymlink(target, dst) })
}

func (j *journal) replace(dst string, exists bool, do func() error) error {
	e := Entry{Op: OpCreate, Path: dst}
	if exists {
		e.Op = OpReplace
		if err := j.save(dst, &e); err != nil {
			return fmt.Errorf("back up %s: %w", dst, err)
		}
	}
	if err := do(); err != nil {
		if e.Backup != "" {
			_ = os.Remove(filepath.Join(backupDir(j.root), e.Backup))
		}
		return err
	}
	j.add(e)
	return nil
}

// save records what is at dst so it can be put back: a symlink by its
// target, anything else as a copy in the backup directory.
func (j *journal) save(dst string, e *Entry) error {
	fi, err := os.Lstat(dst)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		if e.OldLink, err = os.Readlink(dst); err != nil {
			return fmt.Errorf("readlink: %w", err)
		}
		return nil
	}
	e.Backup = fmt.Sprintf("%04d", len(j.entries))
	return copyFile(dst, filepath.Join(backupDir(j.root), e.Backup))
}

// undo reverses the journal: files first, newest first, then the receipt,
// then directories, deepest first. A path at or under one of keep is never
// touched.
func (j *journal) undo(keep []string) error {
	if err := j.undoFiles(keep); err != nil {
		return err
	}
	_ = os.Remove(ReceiptPath(j.root))
	j.removeDirs(keep, nil)
	return nil
}

// undoFiles reverses every entry but the directories, newest first: it
// removes the files the journal created, puts back those it replaced, and
// undoes the actions, which come after the files and so go first.
// Problems are collected, not fatal, so one stuck file does not leave the
// rest.
func (j *journal) undoFiles(keep []string) error {
	var errs []error
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if e.Op == OpMkdir || kept(e.Path, keep) {
			continue
		}
		var err error
		switch e.Op {
		case OpCreate:
			err = removeCreated(e.Path)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
			j.report.emit(Detail, "removed %s", e.Path)
		case OpReplace:
			err = j.restore(e)
			j.report.emit(Detail, "restored %s", e.Path)
		case OpRegKey, OpRegValue, OpPath:
			err = j.undoRegistry(e)
		default:
			err = j.undoAction(e)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	// Directories stay until every file is dealt with, so a second run
	// still finds the receipt.
	return errors.Join(errs...)
}

// removeDirs removes the directories the journal created that are empty,
// deepest first. One that is not empty is left with a warning, unless
// listed says its contents are reported another way.
func (j *journal) removeDirs(keep []string, listed func(string) bool) {
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if e.Op != OpMkdir || kept(e.Path, keep) {
			continue
		}
		if err := os.Remove(e.Path); err != nil && !errors.Is(err, fs.ErrNotExist) && (listed == nil || !listed(e.Path)) {
			j.report.emit(Warn, "left %s: it is not empty", e.Path)
		}
	}
}

func (j *journal) restore(e Entry) error {
	if e.OldLink != "" {
		if err := atomicSymlink(e.OldLink, e.Path); err != nil {
			return fmt.Errorf("restore %s: %w", e.Path, err)
		}
		return nil
	}
	b := filepath.Join(backupDir(j.root), e.Backup)
	f, err := os.Open(b)
	if errors.Is(err, fs.ErrNotExist) {
		// Restored by an earlier run that stopped part-way.
		return nil
	}
	if err != nil {
		return fmt.Errorf("restore %s: %w", e.Path, err)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("restore %s: %w", e.Path, err)
	}
	if err := atomicWrite(e.Path, f, "", fi.Mode().Perm()); err != nil {
		return fmt.Errorf("restore %s: %w", e.Path, err)
	}
	_ = f.Close()
	if err := os.Remove(b); err != nil {
		return fmt.Errorf("remove backup of %s: %w", e.Path, err)
	}
	return nil
}

func kept(path string, keep []string) bool {
	for _, k := range keep {
		if rel, err := filepath.Rel(k, path); err == nil && (rel == "." || filepath.IsLocal(rel)) {
			return true
		}
	}
	return false
}

// atomicWrite writes r to a temporary file beside dst, checks its sha256
// when sum is set, and renames it over dst. dst is never half written.
func atomicWrite(dst string, r io.Reader, sum string, mode os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".fynstall-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); sum != "" && got != sum {
		return fmt.Errorf("write %s: sha256 is %s, the manifest says %s", dst, got, sum)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

// atomicSymlink makes dst a symlink to target, replacing what is there in
// one rename.
func atomicSymlink(target, dst string) error {
	tmp := filepath.Join(filepath.Dir(dst), fmt.Sprintf(".fynstall-link-%d", os.Getpid()))
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("link %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("link %s: %w", dst, err)
	}
	return nil
}

// copyFile copies src to dst, keeping its permission bits.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	return atomicWrite(dst, in, "", fi.Mode().Perm())
}
