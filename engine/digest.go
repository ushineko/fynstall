package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Digest is the sha256 of everything p will change. The privileged helper
// makes the plan again as root and applies it only when its digest is the
// one the person approved (spec 001 R14). Content is in it by hash, so a
// secret's value is not.
func (p *Plan) Digest() (string, error) {
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
	d := struct {
		Scope, Root, Index string
		Keep, Dirs         []string
		Files              []file
		Links              []PlannedLink
		Actions            []action
		Hooks              []Hook
		Remove             []string
	}{Scope: p.Scope, Root: p.Root, Index: p.Index, Keep: p.Keep, Dirs: p.Dirs, Hooks: p.Hooks, Remove: p.Manifest.UninstallRemove}
	for _, f := range p.Files {
		d.Files = append(d.Files, file{Dst: f.Dst, SHA256: f.SHA256, Mode: f.Mode, Exists: f.Exists})
	}
	d.Links = append(append(d.Links, p.Symlinks...), p.Links...)
	for _, a := range p.Actions {
		switch {
		case a.Service != nil:
			s := a.Service
			sum := sha256.Sum256(s.Content)
			d.Actions = append(d.Actions, action{Service: s.Name, Unit: s.Unit, Content: hex.EncodeToString(sum[:]), Start: s.Start, Exists: s.Exists})
		case a.Run != nil:
			r := a.Run
			d.Actions = append(d.Actions, action{Exec: r.Exec, Args: r.Args, Undo: r.Undo, NoUndo: r.NoUndo})
		case a.Migrate != nil:
			m := a.Migrate
			d.Actions = append(d.Actions, action{From: m.From, To: m.To, Present: m.Present})
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("digest: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
