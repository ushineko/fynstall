package builder

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
