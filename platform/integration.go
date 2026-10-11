package platform

import "strings"

// The kinds of desktop integration a backend makes from the same config
// keys (spec 002 L10).
const (
	// XDG is launcher entries and icons under {data} and links in {bin}.
	XDG = "xdg"
	// WindowsShell is Start Menu shortcuts, the Uninstall registry entry
	// that Settings > Apps reads, and an entry on PATH.
	WindowsShell = "windows"
	// NoIntegration is an OS without a backend.
	NoIntegration = ""
)

// Shortcut is a Start Menu shortcut to Target, started in Dir with Args.
// Icon is an .ico file, or "" for the target's own icon.
type Shortcut struct {
	Target      string
	Args        []string
	Description string
	Icon        string
	Dir         string
}

// The kinds of registry value the installer writes and restores.
const (
	RegString       = "sz"
	RegExpandString = "expand_sz"
	RegNumber       = "dword"
)

// RegValue is a registry value. Data is the text of a string, or a number
// in decimal.
type RegValue struct {
	Name string
	Kind string
	Data string
}

// The registry keys the Windows backend writes for a per-user install.
const (
	UninstallKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall`
	EnvironmentKey = `HKCU\Environment`
	// PathValue is the value of EnvironmentKey that holds the user's PATH.
	PathValue = "Path"
)

// RegKey is key as the installer writes it: key itself, or, in the tests,
// key moved under the registry root they name (see RegistryRoot).
func RegKey(v map[string]string, key string) string {
	if v[keyRegRoot] == "" {
		return key
	}
	return `HKCU\` + v[keyRegRoot] + `\` + key
}

// StartMenu is the directory Start Menu shortcuts go in, or "" on an OS
// that has none.
func StartMenu(v map[string]string) string { return v[keyStartMenu] }

// PathList adds dir to the PATH-style list, and reports whether it was
// missing. Entries are compared as Windows compares paths: without regard
// to case or a trailing separator.
func PathList(list, dir string) (string, bool) {
	for _, e := range strings.Split(list, ";") {
		if samePathEntry(e, dir) {
			return list, false
		}
	}
	if list == "" || strings.HasSuffix(list, ";") {
		return list + dir, true
	}
	return list + ";" + dir, true
}

// PathListWithout removes every entry equal to dir from the PATH-style
// list and leaves the others as they were written.
func PathListWithout(list, dir string) string {
	var keep []string
	for _, e := range strings.Split(list, ";") {
		if !samePathEntry(e, dir) {
			keep = append(keep, e)
		}
	}
	return strings.Join(keep, ";")
}

func samePathEntry(a, b string) bool {
	trim := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), `\/`) }
	return strings.EqualFold(trim(a), trim(b))
}

// ShortcutName is name as a file name for a shortcut: the characters
// Windows does not allow in one become "_".
func ShortcutName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) || r < ' ' {
			return '_'
		}
		return r
	}, name)
	return strings.Trim(name, " .") + ".lnk"
}
