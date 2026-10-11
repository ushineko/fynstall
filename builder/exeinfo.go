package builder

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/ushineko/fynstall/manifest"
)

// More kinds of Windows resource (the RT_ numbers of winuser.h).
const (
	resVersion  = 16 // what Properties > Details shows
	resManifest = 24 // the application manifest
)

// exeResources is the resource object for one generated program of a
// Windows target, keyed by its file name: the version resource and the
// application manifest (spec 002 L7), and the icon when the app has one.
// kind is installer or uninstaller, and file is what the program is called;
// both go into the version resource. Another OS gets nothing.
func exeResources(goos, goarch string, app manifest.App, ico []byte, kind, file string) (map[string][]byte, error) {
	if goos != "windows" {
		return nil, nil
	}
	list := []resource{
		{kind: resVersion, id: 1, data: versionResource(app, kind, file)},
		{kind: resManifest, id: 1, data: exeManifest(app, kind)},
	}
	if ico != nil {
		icons, err := iconResources(ico)
		if err != nil {
			return nil, err
		}
		list = append(list, icons...)
	}
	obj, err := resourceObject(goarch, list)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{sysoName(goarch): obj}, nil
}

// versionNumbers are the four numbers Windows keeps for a version, from an
// app version X.Y.Z: the fourth is 0. A number that does not fit in 16 bits
// becomes the largest that does; the version as written is in the strings.
func versionNumbers(v string) [4]uint16 {
	v, _, _ = strings.Cut(v, "+")
	v, _, _ = strings.Cut(v, "-")
	var out [4]uint16
	for i, part := range strings.SplitN(v, ".", 4) {
		n, err := strconv.ParseUint(part, 10, 64)
		switch {
		case err != nil:
		case n > 0xffff:
			out[i] = 0xffff
		default:
			out[i] = uint16(n)
		}
	}
	return out
}

// versionResource is the VS_VERSIONINFO of a program of app: the numbers,
// and the strings in US English that Explorer and Task Manager show. All of
// it comes from the config's app block, so there is no key for it.
func versionResource(app manifest.App, kind, file string) []byte {
	le := binary.LittleEndian
	n := versionNumbers(app.Version)
	var flags uint32
	if strings.Contains(strings.SplitN(app.Version, "+", 2)[0], "-") {
		flags = 2 // VS_FF_PRERELEASE
	}
	ms, ls := uint32(n[0])<<16|uint32(n[1]), uint32(n[2])<<16|uint32(n[3])
	var fixed []byte
	for _, v := range []uint32{
		0xfeef04bd, 0x00010000, // the signature, and the version of this structure
		ms, ls, ms, ls, // the file's version, then the product's
		0x3f, flags, // which flags mean something, and the flags
		0x00040004, 1, 0, // for 32-bit and 64-bit Windows; an application; no subtype
		0, 0, // no date, so the bytes do not change
	} {
		fixed = le.AppendUint32(fixed, v)
	}

	strs := [][2]string{
		{"CompanyName", app.Publisher},
		{"FileDescription", app.Name + " " + kind},
		{"FileVersion", app.Version},
		{"OriginalFilename", file},
		{"ProductName", app.Name},
		{"ProductVersion", app.Version},
	}
	var table [][]byte
	for _, s := range strs {
		if s[1] != "" {
			table = append(table, verNode(s[0], true, wide(s[1])))
		}
	}
	// 0409 is US English and 04b0 is Unicode; the table's name and the
	// translation say the same thing.
	translation := le.AppendUint16(le.AppendUint16(nil, 0x0409), 0x04b0)
	return verNode("VS_VERSION_INFO", false, fixed,
		verNode("StringFileInfo", true, nil, verNode("040904b0", true, nil, table...)),
		verNode("VarFileInfo", true, nil, verNode("Translation", false, translation)))
}

// wide is s as UTF-16 with its terminating zero.
func wide(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return append(b, 0, 0)
}

// verNode is one node of a version resource: its length, the length of its
// value (in characters for text, in bytes otherwise), whether the value is
// text, its name, the value, and its children. The value and each child
// start on a 32-bit boundary.
func verNode(key string, text bool, value []byte, children ...[]byte) []byte {
	le := binary.LittleEndian
	pad := func(b []byte) []byte { return append(b, make([]byte, (4-len(b)%4)%4)...) }
	valueLen, kind := len(value), uint16(0)
	if text {
		valueLen, kind = len(value)/2, 1
	}
	b := make([]byte, 2, 64)                 // the length, set below
	b = le.AppendUint16(b, uint16(valueLen)) // #nosec G115 -- a string from the config's app block
	b = append(le.AppendUint16(b, kind), wide(key)...)
	if len(value) > 0 {
		b = append(pad(b), value...)
	}
	for _, c := range children {
		b = append(pad(b), c...)
	}
	le.PutUint16(b, uint16(len(b))) // #nosec G115 -- a few hundred bytes
	return b
}

// exeManifest is the application manifest of a program of app. It says
// three things. The program runs with the rights of whoever starts it: a
// system install asks for an administrator through its helper, and the
// window never runs as one (spec 001 R13), so no installer asks Windows to
// elevate it. Without the statement, Windows guesses from a file's name.
// The program knows Windows 10 and 11, so Windows tells it the real version
// (spec 002 L2). And the name and version identify it.
func exeManifest(app manifest.App, kind string) []byte {
	n := versionNumbers(app.Version)
	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="%s.%s" version="%d.%d.%d.%d"/>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
</assembly>
`, app.ID, kind, n[0], n[1], n[2], n[3])
}
