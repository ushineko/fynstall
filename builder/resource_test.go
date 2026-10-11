package builder

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/xml"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/ushineko/fynstall/manifest"
)

// ReadResources returns the resources in the .rsrc section of the PE file
// or COFF object at path, keyed by kind and number. It reads the tree as
// Windows does: from the root, through the directory of each kind and each
// number, to the leaf that names the bytes. The integration tests use it on
// a built installer.
func ReadResources(t *testing.T, path string) map[[2]uint16][]byte {
	t.Helper()
	f, err := pe.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	s := f.Section(".rsrc")
	require.NotNil(t, s, "%s has no .rsrc section", path)
	sec, err := s.Data()
	require.NoError(t, err)

	le := binary.LittleEndian
	// entries are the (number, offset) pairs of the directory at off.
	entries := func(off uint32) [][2]uint32 {
		require.Zero(t, le.Uint16(sec[off+12:]), "no named entries")
		var out [][2]uint32
		for i := range uint32(le.Uint16(sec[off+14:])) {
			e := sec[off+16+8*i:]
			out = append(out, [2]uint32{le.Uint32(e), le.Uint32(e[4:])})
		}
		return out
	}
	const subdir = 0x80000000
	out := map[[2]uint16][]byte{}
	for _, kind := range entries(0) {
		require.NotZero(t, kind[1]&subdir)
		for _, id := range entries(kind[1] &^ subdir) {
			require.NotZero(t, id[1]&subdir)
			langs := entries(id[1] &^ subdir)
			require.Len(t, langs, 1)
			require.Zero(t, langs[0][1]&subdir, "a language names a leaf")
			leaf := sec[langs[0][1]:]
			// In a linked file the leaf holds an address in the image; in an
			// object, where the section has no address, an offset.
			at, n := le.Uint32(leaf)-s.VirtualAddress, le.Uint32(leaf[4:])
			require.Zero(t, at%8, "resource bytes start on an 8-byte boundary")
			out[[2]uint16{uint16(kind[0]), uint16(id[0])}] = sec[at : at+n]
		}
	}
	return out
}

func testIco(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	p := filepath.Join(t.TempDir(), "icon.png")
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
	b, err := ico(p)
	require.NoError(t, err)
	return b
}

// RequireIcon checks that the resources of a file are the images of the
// .ico file ico, and one group that lists them in the file's order.
func RequireIcon(t *testing.T, ico []byte, got map[[2]uint16][]byte) {
	t.Helper()
	le := binary.LittleEndian
	count := int(le.Uint16(ico[4:]))
	// A C linker may add resources of its own, such as a manifest; the icon
	// is the images and one group.
	per := map[uint16]int{}
	for k := range got {
		per[k[0]]++
	}
	require.Equal(t, count, per[resIcon], "each image")
	require.Equal(t, 1, per[resGroupIcon], "one group")
	group := got[[2]uint16{resGroupIcon, 1}]
	require.Len(t, group, 6+14*count)
	require.Equal(t, ico[:6], group[:6])
	for i := range count {
		entry := ico[6+16*i:]
		in := group[6+14*i:]
		require.Equal(t, entry[:12], in[:12], "size, depth and length of image %d", i)
		require.Equal(t, uint16(i+1), le.Uint16(in[12:]))
		n, off := le.Uint32(entry[8:]), le.Uint32(entry[12:])
		require.Equal(t, ico[off:off+n], got[[2]uint16{resIcon, uint16(i + 1)}], "image %d", i)
	}
}

// The object that the linker reads holds each image of the icon and the
// group, and asks for one address for each: the place of its bytes.
func TestTheIconResourcesAreOneSectionTheLinkerCanPlace(t *testing.T) {
	b := testIco(t)
	list, err := iconResources(b)
	require.NoError(t, err)
	for arch, machine := range map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64} {
		obj, err := resourceObject(arch, list)
		require.NoError(t, err)
		again, err := resourceObject(arch, list)
		require.NoError(t, err)
		require.Equal(t, obj, again, "the same icon gives the same bytes")

		p := filepath.Join(t.TempDir(), sysoName(arch))
		require.NoError(t, os.WriteFile(p, obj, 0o600))
		f, err := pe.Open(p)
		require.NoError(t, err)
		require.Equal(t, machine, f.Machine)
		require.Len(t, f.Sections, 1)
		require.Len(t, f.Sections[0].Relocs, len(list), "one address for each resource")
		require.Len(t, f.COFFSymbols, 1)
		require.NoError(t, f.Close())
		RequireIcon(t, b, ReadResources(t, p))
	}

	_, err = resourceObject("386", list)
	require.ErrorContains(t, err, "windows/386")
	_, err = iconResources([]byte("not an icon"))
	require.ErrorContains(t, err, "not an .ico file")
	_, err = iconResources(b[:40])
	require.ErrorContains(t, err, "not an .ico file")
}

// ReadVersion reads a version resource as Windows lays it out: the four
// numbers of the file's version, the flags, and the strings by name.
func ReadVersion(t *testing.T, b []byte) (numbers [4]uint16, flags uint32, strs map[string]string) {
	t.Helper()
	le := binary.LittleEndian
	text := func(b []byte) (string, int) {
		var u []uint16
		for i := 0; ; i += 2 {
			if c := le.Uint16(b[i:]); c != 0 {
				u = append(u, c)
				continue
			}
			return string(utf16.Decode(u)), i + 2
		}
	}
	strs = map[string]string{}
	// walk reads the node at the start of b and the nodes inside it.
	var walk func(b []byte, depth int)
	walk = func(b []byte, depth int) {
		length, valueLen, kind := int(le.Uint16(b)), int(le.Uint16(b[2:])), le.Uint16(b[4:])
		key, n := text(b[6:])
		at := (6 + n + 3) &^ 3
		if kind == 1 {
			valueLen *= 2
		}
		switch {
		case key == "VS_VERSION_INFO":
			require.Equal(t, 52, valueLen)
			fixed := b[at:]
			require.Equal(t, uint32(0xfeef04bd), le.Uint32(fixed))
			ms, ls := le.Uint32(fixed[8:]), le.Uint32(fixed[12:])
			require.Equal(t, [2]uint32{ms, ls}, [2]uint32{le.Uint32(fixed[16:]), le.Uint32(fixed[20:])}, "the product's version is the file's")
			numbers = [4]uint16{uint16(ms >> 16), uint16(ms), uint16(ls >> 16), uint16(ls)}
			flags = le.Uint32(fixed[28:])
		case key == "Translation":
			require.Equal(t, []byte{0x09, 0x04, 0xb0, 0x04}, b[at:at+4], "US English, Unicode")
		case depth == 3:
			v, _ := text(b[at:])
			require.Equal(t, len(utf16.Encode([]rune(v)))+1, valueLen/2, "%s: the length counts the characters and the zero", key)
			strs[key] = v
			return
		case depth == 2 && key != "Translation":
			require.Equal(t, "040904b0", key, "the table is named for its language")
		}
		for at = (at + valueLen + 3) &^ 3; at < length; {
			walk(b[at:], depth+1)
			at = (at + int(le.Uint16(b[at:])) + 3) &^ 3
		}
	}
	walk(b, 0)
	require.Equal(t, int(le.Uint16(b)), len(b), "the first node is the whole resource")
	return numbers, flags, strs
}

// The version resource and the manifest come from the app block alone
// (spec 002 L7).
func TestAProgramSaysWhatItIsAndAsksForNoMoreRights(t *testing.T) {
	app := manifest.App{ID: "io.example.shell", Name: "Shell Example", Version: "1.2.3-rc.1+build5", Publisher: "Example Makers & Sons"}
	ico := testIco(t)
	res, err := exeResources("windows", "amd64", app, ico, "installer", "shell-installer.exe")
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), sysoName("amd64"))
	require.NoError(t, os.WriteFile(p, res[sysoName("amd64")], 0o600))
	got := ReadResources(t, p)

	numbers, flags, strs := ReadVersion(t, got[[2]uint16{resVersion, 1}])
	require.Equal(t, [4]uint16{1, 2, 3, 0}, numbers)
	require.Equal(t, uint32(2), flags, "a version with a prerelease part says so")
	require.Equal(t, map[string]string{
		"CompanyName": "Example Makers & Sons", "FileDescription": "Shell Example installer",
		"FileVersion": "1.2.3-rc.1+build5", "ProductVersion": "1.2.3-rc.1+build5",
		"ProductName": "Shell Example", "OriginalFilename": "shell-installer.exe",
	}, strs)

	var m struct {
		Identity struct {
			Name    string `xml:"name,attr"`
			Version string `xml:"version,attr"`
		} `xml:"assemblyIdentity"`
		Run struct {
			Level string `xml:"level,attr"`
		} `xml:"trustInfo>security>requestedPrivileges>requestedExecutionLevel"`
	}
	require.NoError(t, xml.Unmarshal(got[[2]uint16{resManifest, 1}], &m))
	require.Equal(t, "io.example.shell.installer", m.Identity.Name)
	require.Equal(t, "1.2.3.0", m.Identity.Version)
	require.Equal(t, "asInvoker", m.Run.Level, "the helper asks for an administrator, never the program itself")
	RequireIcon(t, ico, got)

	// No publisher and no icon: the string and the images are left out.
	app.Publisher, app.Version = "", "70000.2.3"
	plain := versionResource(app, "uninstaller", "u.exe")
	numbers, flags, strs = ReadVersion(t, plain)
	require.Equal(t, [4]uint16{0xffff, 2, 3, 0}, numbers, "a number too large for Windows is its largest")
	require.Zero(t, flags)
	require.NotContains(t, strs, "CompanyName")
	require.Equal(t, "70000.2.3", strs["FileVersion"], "the strings hold the version as written")

	res, err = exeResources("linux", "amd64", app, nil, "installer", "x")
	require.NoError(t, err)
	require.Empty(t, res, "only a Windows program has resources")
}

// RequireAsInvoker checks that the resources of a file hold one manifest,
// and that it asks for the rights of whoever starts the program.
func RequireAsInvoker(t *testing.T, got map[[2]uint16][]byte) {
	t.Helper()
	n := 0
	for k := range got {
		if k[0] == resManifest {
			n++
		}
	}
	require.Equal(t, 1, n, "one manifest")
	require.Contains(t, string(got[[2]uint16{resManifest, 1}]), `level="asInvoker"`)
}
