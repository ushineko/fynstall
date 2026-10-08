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
	App            App    `json:"app"`
	// Scopes the user may choose, the first being the default.
	Scopes []string `json:"scopes"`
	// Dirs maps a scope to its install directory template, such as
	// "{data}/{id}". See Expand.
	Dirs map[string]string `json:"dirs"`
	// KeepOnUninstall are path templates an uninstaller never touches.
	KeepOnUninstall []string `json:"keep_on_uninstall,omitempty"`
	// Files are sorted by Path, so the same input gives the same bytes.
	Files []File `json:"files"`
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
