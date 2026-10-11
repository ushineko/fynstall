package engine

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

/*
What the same config keys become on Windows (spec 002 L8 and L10). A
launcher entry is a Start Menu shortcut. A link is the program's directory
on the user's PATH. Every install has an Uninstall registry entry, which is
what Settings > Apps lists and runs; it has no config key, because every
field is derived.

Each registry change is in the plan, and each is journalled with what was
there before, so the uninstaller puts the registry back (spec 001 R9a,
R9d). PATH is the exception to "what was there before": other installers
change it too, so the uninstaller takes out the one entry this install
added and leaves the rest of the value as it finds it, as Windows Installer
does.
*/

// PlannedShortcut is a Start Menu shortcut Apply will write at Dst.
type PlannedShortcut struct {
	platform.Shortcut
	Dst string
	// Base is the Start Menu directory Dst must stay inside.
	Base   string
	Exists bool
}

// PlannedKey is a registry key Apply will make, with its values.
type PlannedKey struct {
	Key    string
	Values []platform.RegValue
}

// RegEntry is the registry part of an OpRegValue or OpPath entry.
type RegEntry struct {
	// Name is the value's name.
	Name string `json:"name"`
	// Had is true when the value was there before the install; Kind and
	// Data are then what it held. On an OpPath entry, Data is instead the
	// directory the install added.
	Had  bool   `json:"had,omitempty"`
	Kind string `json:"kind,omitempty"`
	Data string `json:"data,omitempty"`
}

// addShellIntegration plans the shortcuts, the Uninstall entry and the PATH
// entries of a Windows install.
func (p *Plan) addShellIntegration(vars map[string]string) error {
	m := p.Manifest
	icon := ""
	if slices.ContainsFunc(m.Files, func(f manifest.File) bool { return f.Path == manifest.WindowsIcon }) {
		icon = p.inRoot(manifest.WindowsIcon)
	}
	menu := platform.StartMenu(vars)
	for _, d := range m.Desktop {
		ps := PlannedShortcut{
			Shortcut: platform.Shortcut{Target: p.inRoot(d.Exec), Args: d.Args, Description: d.Comment, Dir: p.Root},
			Dst:      filepath.Join(menu, platform.ShortcutName(d.Name)), Base: menu,
		}
		if d.Icon {
			ps.Icon = icon
		}
		exists, err := p.check(ps.Dst, ps.Base)
		if err != nil {
			return err
		}
		ps.Exists = exists
		p.Shortcuts = append(p.Shortcuts, ps)
	}

	// The uninstaller, as Settings > Apps starts it. Started there it has
	// no console of its own, so a full build is told to open its window.
	uninstall := `"` + p.inRoot(UninstallName) + `"`
	open := uninstall
	if m.GUI {
		open += " --gui"
	}
	var size int64
	for _, f := range p.Files {
		size += f.Size
	}
	text := func(name, data string) platform.RegValue {
		return platform.RegValue{Name: name, Kind: platform.RegString, Data: data}
	}
	number := func(name string, n int64) platform.RegValue {
		return platform.RegValue{Name: name, Kind: platform.RegNumber, Data: strconv.FormatInt(n, 10)}
	}
	values := []platform.RegValue{text("DisplayName", m.App.Name), text("DisplayVersion", m.App.Version)}
	if m.App.Publisher != "" {
		values = append(values, text("Publisher", m.App.Publisher))
	}
	values = append(values, text("InstallLocation", p.Root))
	if icon != "" {
		values = append(values, text("DisplayIcon", icon))
	}
	values = append(values,
		number("EstimatedSize", (size+1023)/1024), // in kilobytes
		text("UninstallString", open),
		text("QuietUninstallString", uninstall+" --quiet"),
		number("NoModify", 1), number("NoRepair", 1),
	)
	p.Registry = append(p.Registry, PlannedKey{Key: platform.UninstallKeyOf(vars) + `\` + m.App.ID, Values: values})

	for _, l := range m.Links {
		if dir := filepath.Dir(p.inRoot(l.Target)); !slices.Contains(p.PathDirs, dir) {
			p.PathDirs = append(p.PathDirs, dir)
		}
	}
	if len(p.PathDirs) > 0 {
		p.PathKey = platform.EnvironmentKeyOf(vars)
		p.RefreshMenu = true
	}
	return nil
}

// HasShellIntegration reports whether p has shortcuts, registry keys or
// PATH entries, which Apply writes in their own step.
func (p *Plan) HasShellIntegration() bool {
	return len(p.Shortcuts)+len(p.Registry)+len(p.PathDirs) > 0
}

// applyShell writes the shortcuts, the registry keys and the PATH entries.
// Each change is journalled as it is made, so a failure part-way is undone.
func (j *journal) applyShell(p *Plan) error {
	for _, s := range p.Shortcuts {
		if err := j.replace(s.Dst, s.Exists, func() error { return platform.WriteShortcut(s.Dst, s.Shortcut) }); err != nil {
			return err
		}
		j.report.emit(Detail, "%s -> %s", s.Dst, s.Target)
	}
	for _, k := range p.Registry {
		if err := j.makeKey(k.Key); err != nil {
			return err
		}
		for _, v := range k.Values {
			old, had, err := platform.RegGet(k.Key, v.Name)
			if err != nil {
				return err //nolint:wrapcheck // names the key and the value
			}
			j.add(Entry{Op: OpRegValue, Path: k.Key, Reg: &RegEntry{Name: v.Name, Had: had, Kind: old.Kind, Data: old.Data}})
			if err := platform.RegSet(k.Key, v); err != nil {
				return err //nolint:wrapcheck // as above
			}
		}
		j.report.emit(Detail, "%s", k.Key)
	}
	for _, dir := range p.PathDirs {
		if err := j.addToPath(p.PathKey, dir); err != nil {
			return err
		}
	}
	return nil
}

// makeKey makes a registry key and journals each key that had to be made
// on the way to it.
func (j *journal) makeKey(key string) error {
	made, err := platform.RegCreateKey(key)
	for _, k := range made {
		j.add(Entry{Op: OpRegKey, Path: k})
	}
	return err //nolint:wrapcheck // names the key
}

// addToPath puts dir on the PATH value of key. A directory that is already
// there is left, and nothing is journalled: the uninstaller must not take
// out an entry the install did not add.
func (j *journal) addToPath(key, dir string) error {
	if err := j.makeKey(key); err != nil {
		return err
	}
	old, had, err := platform.RegGet(key, platform.PathValue)
	if err != nil {
		return err //nolint:wrapcheck // names the key and the value
	}
	list, added := platform.PathList(old.Data, dir)
	if !added {
		j.report.emit(Detail, "%s is already on PATH", dir)
		return nil
	}
	v := platform.RegValue{Name: platform.PathValue, Kind: old.Kind, Data: list}
	if !had {
		// The kind Windows itself gives a new PATH.
		v.Kind = platform.RegExpandString
	}
	j.add(Entry{Op: OpPath, Path: key, Reg: &RegEntry{Name: platform.PathValue, Had: had, Data: dir}})
	if err := platform.RegSet(key, v); err != nil {
		return err //nolint:wrapcheck // as above
	}
	j.report.emit(Detail, "added %s to PATH", dir)
	return nil
}

// undoRegistry reverses an OpRegKey, OpRegValue or OpPath entry.
func (j *journal) undoRegistry(e Entry) error {
	switch e.Op {
	case OpRegKey:
		gone, err := platform.RegDeleteKeyIfEmpty(e.Path)
		if err != nil {
			return err //nolint:wrapcheck // names the key
		}
		if !gone {
			j.report.emit(Warn, "left the registry key %s: it is not empty", e.Path)
		}
	case OpRegValue:
		if !e.Reg.Had {
			return platform.RegDelete(e.Path, e.Reg.Name) //nolint:wrapcheck // names the key and the value
		}
		if _, err := platform.RegCreateKey(e.Path); err != nil {
			return err //nolint:wrapcheck // names the key
		}
		if err := platform.RegSet(e.Path, platform.RegValue{Name: e.Reg.Name, Kind: e.Reg.Kind, Data: e.Reg.Data}); err != nil {
			return err //nolint:wrapcheck // as above
		}
	case OpPath:
		cur, has, err := platform.RegGet(e.Path, e.Reg.Name)
		if err != nil || !has {
			return err //nolint:wrapcheck // names the key and the value
		}
		rest := platform.PathListWithout(cur.Data, e.Reg.Data)
		if rest == "" && !e.Reg.Had {
			// The install made the value, and nothing else is in it.
			return platform.RegDelete(e.Path, e.Reg.Name) //nolint:wrapcheck // as above
		}
		if rest == cur.Data {
			return nil
		}
		cur.Data = rest
		if err := platform.RegSet(e.Path, cur); err != nil {
			return err //nolint:wrapcheck // as above
		}
		j.report.emit(Detail, "removed %s from PATH", e.Reg.Data)
	default:
		return fmt.Errorf("journal entry %q for %s is not one this uninstaller knows", e.Op, e.Path)
	}
	return nil
}
