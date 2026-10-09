package platform

import (
	"fmt"
	"strings"

	"github.com/ushineko/fynstall/manifest"
)

// HicolorSizes are the icon sizes installed into the hicolor theme. KDE
// picks the directory that matches the size it asks for and only scales
// within the sizes the theme declares, so a lone 512 px icon shows as a
// placeholder in a 48 px menu.
var HicolorSizes = []int{16, 32, 48, 64, 128, 256, 512}

// IconPath is where the app icon of one size goes, relative to {data}.
func IconPath(id string, size int) string {
	return fmt.Sprintf("icons/hicolor/%dx%d/apps/%s.png", size, size, id)
}

// DesktopPath is where a desktop entry goes, relative to {data}.
func DesktopPath(id string) string {
	return "applications/" + id + ".desktop"
}

// UninstallAction is the desktop action id of the Uninstall entry.
const UninstallAction = "uninstall"

// RenderDesktop returns the desktop entry for d, with exec the absolute
// path of the program and icon the icon name ("" for none). A non-empty
// uninstall is the uninstaller's path: the entry then has an Uninstall
// action that runs it with --gui, so it needs no terminal.
func RenderDesktop(d manifest.Desktop, exec, icon, uninstall string) []byte {
	var b strings.Builder
	line := func(k, v string) { fmt.Fprintf(&b, "%s=%s\n", k, v) }
	b.WriteString("[Desktop Entry]\n")
	line("Type", "Application")
	line("Version", "1.5")
	line("Name", escapeString(d.Name))
	if d.Comment != "" {
		line("Comment", escapeString(d.Comment))
	}
	args := []string{exec}
	args = append(args, d.Args...)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteExecArg(a)
	}
	line("Exec", escapeString(strings.Join(quoted, " ")))
	if icon != "" {
		line("Icon", escapeString(icon))
	}
	line("Terminal", fmt.Sprint(d.Terminal))
	if len(d.Categories) > 0 {
		line("Categories", strings.Join(d.Categories, ";")+";")
	}
	if uninstall != "" {
		line("Actions", UninstallAction+";")
		b.WriteString("\n[Desktop Action " + UninstallAction + "]\n")
		line("Name", escapeString("Uninstall "+d.Name))
		line("Exec", escapeString(quoteExecArg(uninstall)+" --gui"))
	}
	return []byte(b.String())
}

// execReserved are the characters that make an Exec argument need quotes,
// from the Desktop Entry Specification's "The Exec key".
const execReserved = " \t\n\"'\\><~|&;$*?#()`"

// quoteExecArg quotes one Exec argument. Inside double quotes, ", `, $ and
// \ take a backslash. A literal % is written %%, so it is not a field code.
func quoteExecArg(a string) string {
	a = strings.ReplaceAll(a, "%", "%%")
	if !strings.ContainsAny(a, execReserved) {
		return a
	}
	r := strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`, `\`, `\\`)
	return `"` + r.Replace(a) + `"`
}

// escapeString applies the escapes for a value of type string. For Exec it
// runs after quoting, as the specification says, so a backslash inside
// quotes ends up as four.
func escapeString(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}
