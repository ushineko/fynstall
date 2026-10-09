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
)

// Event is a progress report.
type Event struct {
	Kind EventKind
	Text string
}

// Reporter receives events. It is called on the goroutine doing the work.
type Reporter func(Event)

func (r Reporter) emit(k EventKind, format string, a ...any) {
	if r != nil {
		r(Event{Kind: k, Text: fmt.Sprintf(format, a...)})
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
	defer func() {
		if err != nil {
			report.emit(Step, "Undoing the partial install")
			if uerr := j.undo(nil); uerr != nil {
				err = errors.Join(err, fmt.Errorf("undo: %w", uerr))
			}
		}
	}()

	report.emit(Step, "Creating directories")
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
		report.emit(Detail, "%s/", d)
	}

	report.emit(Step, "Copying files")
	for _, f := range p.Files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("cancelled: %w", err)
		}
		if err := j.writePlanned(f, payload, uninstaller); err != nil {
			return nil, err
		}
		report.emit(Detail, "%s", f.Dst)
	}

	if len(p.Links) > 0 {
		report.emit(Step, "Linking")
	}
	for _, l := range p.Links {
		if err := j.link(l.Dst, l.Target, l.Exists); err != nil {
			return nil, err
		}
		report.emit(Detail, "%s -> %s", l.Dst, l.Target)
	}

	report.emit(Step, "Recording the install")
	rcpt = &Receipt{
		Schema: ReceiptSchema, RuntimeVersion: p.Manifest.RuntimeVersion,
		App: p.Manifest.App, Scope: p.Scope, Root: p.Root,
		Uninstaller: filepath.Join(p.Root, UninstallName), Index: p.Index, Keep: p.Keep,
		RefreshMenu: p.RefreshMenu,
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
	root    string
	entries []Entry
	report  Reporter
}

func (j *journal) add(e Entry) { j.entries = append(j.entries, e) }

func (j *journal) writePlanned(f PlannedFile, payload fs.FS, uninstaller []byte) error {
	switch f.Source {
	case FromUninstaller:
		return j.write(f.Dst, bytes.NewReader(uninstaller), f.SHA256, os.FileMode(f.Mode), f.Exists)
	case FromContent:
		return j.write(f.Dst, bytes.NewReader(f.Content), f.SHA256, os.FileMode(f.Mode), f.Exists)
	}
	src, err := payload.Open(f.Path)
	if err != nil {
		return fmt.Errorf("open payload %s: %w", f.Path, err)
	}
	defer func() { _ = src.Close() }()
	return j.write(f.Dst, src, f.SHA256, os.FileMode(f.Mode), f.Exists)
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

// undo reverses the journal: files first, newest first, then directories,
// deepest first. A path at or under one of keep is never touched. Problems
// are collected, not fatal, so one stuck file does not leave the rest.
func (j *journal) undo(keep []string) error {
	var errs []error
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if e.Op == OpMkdir || kept(e.Path, keep) {
			continue
		}
		var err error
		switch e.Op {
		case OpCreate:
			err = os.Remove(e.Path)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
			j.report.emit(Detail, "removed %s", e.Path)
		case OpReplace:
			err = j.restore(e)
			j.report.emit(Detail, "restored %s", e.Path)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		// Directories stay until every file is dealt with, so a second run
		// still finds the receipt.
		return errors.Join(errs...)
	}
	_ = os.Remove(ReceiptPath(j.root))
	for i := len(j.entries) - 1; i >= 0; i-- {
		e := j.entries[i]
		if e.Op != OpMkdir || kept(e.Path, keep) {
			continue
		}
		if err := os.Remove(e.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			j.report.emit(Warn, "left %s: it is not empty", e.Path)
		}
	}
	return nil
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
