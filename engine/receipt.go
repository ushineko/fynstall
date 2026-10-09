package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ushineko/fynstall/manifest"
)

// ReceiptSchema is the receipt format version. An uninstaller is built with
// the installer that wrote its receipt, so it only ever reads its own
// version; the number is there for whoever reads a receipt by hand, and for
// a future installer deciding whether it can read an old one.
const ReceiptSchema = 1

// Op is the kind of a journal entry.
type Op string

const (
	// OpMkdir is a directory Apply created. Undo: remove it if empty.
	OpMkdir Op = "mkdir"
	// OpCreate is a file Apply created where nothing was. Undo: remove it.
	OpCreate Op = "create"
	// OpReplace is an existing file Apply replaced, keeping a copy at
	// Backup, relative to the backup directory. Undo: put the copy back.
	OpReplace Op = "replace"
	// OpService is a service the install set up, its unit file at Path.
	// Undo: stop and remove it, and put back a service it replaced.
	OpService Op = "service"
	// OpRun is a run action. Undo: run its program with its undo
	// arguments.
	OpRun Op = "run"
	// OpMigrate is data moved from From to Path. Undo, only for a failed
	// install: move it back. Path is kept, so an uninstall leaves it.
	OpMigrate Op = "migrate"
)

// Entry is one change Apply made.
type Entry struct {
	Op     Op     `json:"op"`
	Path   string `json:"path"`
	Backup string `json:"backup,omitempty"`
	// OldLink is the target of a symlink that a replace removed. A link is
	// put back as a link, not as a copy of what it pointed at.
	OldLink string `json:"old_link,omitempty"`
	// Service, Run and From are set on the action entries of their Op.
	Service *ServiceEntry `json:"service,omitempty"`
	Run     *RunEntry     `json:"run,omitempty"`
	From    string        `json:"from,omitempty"`
}

// Receipt is what an install leaves at <root>/.fynstall/receipt.json, and
// what the uninstaller reads.
type Receipt struct {
	Schema         int          `json:"schema"`
	RuntimeVersion string       `json:"runtime_version"`
	App            manifest.App `json:"app"`
	Scope          string       `json:"scope"`
	Root           string       `json:"root"`
	Uninstaller    string       `json:"uninstaller"`
	Index          string       `json:"index"`
	Keep           []string     `json:"keep_on_uninstall,omitempty"`
	RefreshMenu    bool         `json:"refresh_menu,omitempty"`
	// Parameters are the non-secret values the install used, which an
	// upgrade reads back (spec 002 D3a). Secrets are named, never stored.
	Parameters map[string]string `json:"parameters,omitempty"`
	Secrets    []string          `json:"secrets,omitempty"`
	// Remove are the uninstall.remove patterns, relative to Root: files the
	// program makes, which the uninstaller removes without asking (spec 002
	// D5).
	Remove []string `json:"uninstall_remove,omitempty"`
	// Hooks run before the uninstaller removes anything.
	Hooks   []Hook  `json:"uninstall_hooks,omitempty"`
	Journal []Entry `json:"journal"`
}

// Index is the install index entry: where to find an install of an app.
type Index struct {
	Root        string `json:"root"`
	Uninstaller string `json:"uninstaller"`
	Version     string `json:"version"`
	Scope       string `json:"scope"`
}

// ReceiptPath is where the receipt for an install at root lives.
func ReceiptPath(root string) string {
	return filepath.Join(root, MetaDir, ReceiptName)
}

func backupDir(root string) string {
	return filepath.Join(root, MetaDir, BackupDir)
}

// ReadReceipt reads the receipt at path.
func ReadReceipt(path string) (*Receipt, error) {
	var r Receipt
	if err := readJSON(path, &r); err != nil {
		return nil, err
	}
	if r.Schema != ReceiptSchema {
		return nil, fmt.Errorf("%s: receipt schema %d, this uninstaller reads %d", path, r.Schema, ReceiptSchema)
	}
	return &r, nil
}

// ReadIndex reads the index entry for app id in scope. A missing entry is
// an error wrapping fs.ErrNotExist.
func ReadIndex(m *manifest.Manifest, scope string, env func(string) string) (*Index, string, error) {
	v, err := Vars(m, scope, env)
	if err != nil {
		return nil, "", err
	}
	p := indexPath(v, m.App.ID)
	var ix Index
	if err := readJSON(p, &ix); err != nil {
		return nil, p, err
	}
	return &ix, p, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func marshal(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return append(b, '\n'), nil
}
