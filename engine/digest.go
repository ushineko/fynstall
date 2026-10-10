package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Digest is the sha256 of everything p will change. The privileged helper
// makes the plan again as root and applies it only when its digest is the
// one the person approved (spec 001 R14). Content is in it by hash. A file
// that holds a secret is in it by path and mode only, so the helper can
// read an existing secret the person's process cannot (an upgrade of a
// system install).
func (p *Plan) Digest() (string, error) { return p.digest(true) }

// ContentDigest is Digest without what depends on the state of the disk:
// whether a path exists, and which directories must be made. An upgrade
// plans for after the old version is gone, removes it, plans again, and
// installs only when the two content digests agree.
func (p *Plan) ContentDigest() (string, error) { return p.digest(false) }

func (p *Plan) digest(state bool) (string, error) {
	type file struct {
		Dst, SHA256 string
		Mode        uint32
		Exists      bool
	}
	type action struct {
		Service, Unit, Content string
		Start, Exists          bool
		Exec                   string
		Args, Undo             []string
		NoUndo                 bool
		From, To               string
		Present                bool
	}
	type link struct {
		Dst, Target, Base string
		Exists            bool
	}
	d := struct {
		Scope, Root, Index string
		Keep, Dirs         []string
		Files              []file
		Links              []link
		Actions            []action
		Hooks              []Hook
		Remove             []string
		Replaces           string
	}{Scope: p.Scope, Root: p.Root, Index: p.Index, Keep: p.Keep, Hooks: p.Hooks, Remove: p.Manifest.UninstallRemove, Replaces: p.Replaces}
	if state {
		d.Dirs = p.Dirs
	}
	for _, f := range p.Files {
		e := file{Dst: f.Dst, SHA256: f.SHA256, Mode: f.Mode, Exists: state && f.Exists}
		if f.Secret {
			e.SHA256 = "secret"
		}
		d.Files = append(d.Files, e)
	}
	for _, l := range append(append([]PlannedLink{}, p.Symlinks...), p.Links...) {
		d.Links = append(d.Links, link{Dst: l.Dst, Target: l.Target, Base: l.Base, Exists: state && l.Exists})
	}
	for _, a := range p.Actions {
		switch {
		case a.Service != nil:
			s := a.Service
			sum := sha256.Sum256(s.Content)
			d.Actions = append(d.Actions, action{Service: s.Name, Unit: s.Unit, Content: hex.EncodeToString(sum[:]), Start: s.Start, Exists: state && s.Exists})
		case a.Run != nil:
			r := a.Run
			d.Actions = append(d.Actions, action{Exec: r.Exec, Args: r.Args, Undo: r.Undo, NoUndo: r.NoUndo})
		case a.Migrate != nil:
			m := a.Migrate
			d.Actions = append(d.Actions, action{From: m.From, To: m.To, Present: state && m.Present})
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("digest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
