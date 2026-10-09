/*
Package manifest is the normalised form of a fynstall.yaml that an installer
carries: the app, where it installs, and every payload file with its
destination, size, sha256 and mode.

The builder writes it and the installer reads it, so it holds nothing that
needs the YAML parser or the developer's filesystem: a CLI-only installer
links only this, the engine and the standard library.
*/
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Schema is the manifest format version. An installer refuses a manifest
// with a different one rather than guessing at its fields.
const Schema = 1

// Manifest is what an installer embeds as manifest.json.
type Manifest struct {
	Schema int `json:"schema"`
	// RuntimeVersion is the fynstall version the installer was built with.
	RuntimeVersion string `json:"runtime_version"`
	// Target is the "os/arch" the installer was built for. The payload is
	// chosen per target (spec 002 D1a), so an installer refuses to run
	// anywhere else.
	Target string `json:"target"`
	App    App    `json:"app"`
	// Scopes the user may choose, the first being the default.
	Scopes []string `json:"scopes"`
	// Dirs maps a scope to its install directory template, such as
	// "{data}/{id}". See Expand.
	Dirs map[string]string `json:"dirs"`
	// KeepOnUninstall are path templates an uninstaller never touches.
	KeepOnUninstall []string `json:"keep_on_uninstall,omitempty"`
	// Files are sorted by Path, so the same input gives the same bytes.
	Files []File `json:"files"`
	// Icons are the app icon at each hicolor size, sorted by size.
	Icons []Icon `json:"icons,omitempty"`
	// Links go in {bin}, each pointing at a payload file.
	Links []Link `json:"links,omitempty"`
	// Desktop entries go in {data}/applications.
	Desktop []Desktop `json:"desktop,omitempty"`
	// Parameters are the values the installer asks for or is given.
	Parameters []Parameter `json:"parameters,omitempty"`
	// ConfigFiles are written from parameters at install time.
	ConfigFiles []ConfigFile `json:"config_files,omitempty"`
}

// Parameter is a declared parameter (spec 002 D3a). A manifest holds the
// declaration, never a value other than a non-secret default.
type Parameter struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ConfigFile is a config_file action: Path is a template that starts with
// {config}, {data} or {home}; Values may use {param:<name>}.
type ConfigFile struct {
	Path   string            `json:"path"`
	Format string            `json:"format"`
	Values map[string]string `json:"values"`
}

// IconDir is where the builder puts the resized icons in the embedded
// payload. Payload destinations can never be inside .fynstall, so the two
// cannot collide.
const IconDir = ".fynstall/icons"

// Icon is the app icon at one size, stored in the embedded payload at
// IconDir/<size>.png.
type Icon struct {
	Size   int    `json:"size"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Path is the icon's path in the embedded payload.
func (i Icon) Path() string { return fmt.Sprintf("%s/%d.png", IconDir, i.Size) }

// Link is a symlink in {bin} named Name, pointing at the payload file
// Target (a File.Path).
type Link struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// Desktop is a launcher entry. Exec is a File.Path; the installer makes it
// absolute. Icon names the app's icon when there is one.
type Desktop struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Comment    string   `json:"comment,omitempty"`
	Exec       string   `json:"exec"`
	Args       []string `json:"args,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Terminal   bool     `json:"terminal,omitempty"`
	Icon       bool     `json:"icon,omitempty"`
}

// App identifies the program being installed.
type App struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Publisher string `json:"publisher,omitempty"`
}

// File is one payload file. Path is slash-separated and relative to the
// install directory; it is also the file's path in the embedded payload.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

// Marshal returns the manifest as indented JSON with a trailing newline.
// Files must already be sorted; the builder sorts them.
func (m *Manifest) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return append(b, '\n'), nil
}

// Parse reads a manifest and checks its schema.
func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if m.Schema != Schema {
		return nil, fmt.Errorf("manifest schema %d, this runtime reads %d", m.Schema, Schema)
	}
	return &m, nil
}

// Basename is the short name used for output files: the last segment of
// the app ID, so io.ushineko.hello gives "hello".
func (a App) Basename() string {
	for i := len(a.ID) - 1; i >= 0; i-- {
		if a.ID[i] == '.' {
			return a.ID[i+1:]
		}
	}
	return a.ID
}
