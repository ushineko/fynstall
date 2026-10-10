package builder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Kinds of Windows resource (the RT_ numbers of winuser.h).
const (
	resIcon      = 3  // one image of an icon
	resGroupIcon = 14 // the list of an icon's images
)

// resource is one Windows resource: a kind, a number within the kind, and
// its bytes.
type resource struct {
	kind, id uint16
	data     []byte
}

// coffArch is what a COFF object says about its processor: the machine
// number, and the relocation that asks for an address relative to the start
// of the image, which is how a resource names its bytes.
type coffArch struct{ machine, reloc uint16 }

var coffArchs = map[string]coffArch{ //nolint:gochecknoglobals // a fixed table
	"amd64": {0x8664, 3}, // IMAGE_REL_AMD64_ADDR32NB
	"arm64": {0xaa64, 2}, // IMAGE_REL_ARM64_ADDR32NB
}

// sysoName is the file name the go command links into a Windows build for
// goarch, and into no other.
func sysoName(goarch string) string { return "fynstall_windows_" + goarch + ".syso" }

// iconResources are the resources that give an .exe the icon in the .ico
// file ico: each image, numbered from 1, and the group that lists them.
// Explorer shows the first group of a file as the file's icon.
func iconResources(ico []byte) ([]resource, error) {
	bad := errors.New("icon resource: not an .ico file")
	if len(ico) < 6 {
		return nil, bad
	}
	le := binary.LittleEndian
	count := int(le.Uint16(ico[4:]))
	if le.Uint16(ico[2:]) != 1 || count == 0 || len(ico) < 6+16*count {
		return nil, bad
	}
	// The group is the file's own directory, with the offset of each image
	// replaced by the number of its resource.
	group := append([]byte(nil), ico[:6]...)
	var out []resource
	for i := range count {
		entry := ico[6+16*i : 6+16*(i+1)]
		size, offset := int(le.Uint32(entry[8:])), int(le.Uint32(entry[12:]))
		if offset < 0 || size < 0 || offset+size > len(ico) {
			return nil, bad
		}
		id := uint16(i + 1) // #nosec G115 -- count is a 16-bit number
		group = append(group, entry[:12]...)
		group = le.AppendUint16(group, id)
		out = append(out, resource{kind: resIcon, id: id, data: ico[offset : offset+size]})
	}
	return append(out, resource{kind: resGroupIcon, id: 1, data: group}), nil
}

// resourceObject is a COFF object for goarch with one section, .rsrc, that
// holds list. The linker puts the section into the .exe (the go command
// takes any .syso file in a package's directory). The same list gives the
// same bytes.
//
// The section is a tree three levels deep (kind, number, language) whose
// leaves name the bytes of each resource. Every number is little-endian.
func resourceObject(goarch string, list []resource) ([]byte, error) {
	arch, ok := coffArchs[goarch]
	if !ok {
		return nil, fmt.Errorf("resources for windows/%s are not supported", goarch)
	}
	list = append([]resource(nil), list...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].kind != list[j].kind {
			return list[i].kind < list[j].kind
		}
		return list[i].id < list[j].id
	})
	var kinds []uint16
	count := map[uint16]int{}
	for _, r := range list {
		if count[r.kind] == 0 {
			kinds = append(kinds, r.kind)
		}
		count[r.kind]++
	}

	const (
		dirSize   = 16 // a directory's header
		entrySize = 8  // one entry of a directory
		leafSize  = 16 // where a resource's bytes are, and how many
		english   = 0x0409
	)
	// The layout: the root, a directory for each kind, a directory with one
	// language for each resource, the leaves, then the bytes.
	numberDirs := dirSize + entrySize*len(kinds)
	langDirs := numberDirs + dirSize*len(kinds) + entrySize*len(list)
	leaves := langDirs + (dirSize+entrySize)*len(list)
	data := leaves + leafSize*len(list)

	le := binary.LittleEndian
	u32 := func(b []byte, v int) []byte { return le.AppendUint32(b, uint32(v)) } // #nosec G115 -- offsets in a section of a few hundred kilobytes
	// sub is u32 with the mark that the offset is of a directory, not a leaf.
	sub := func(b []byte, v int) []byte { return le.AppendUint32(b, 0x80000000|uint32(v)) } // #nosec G115 -- as above
	dir := func(b []byte, entries int) []byte {
		b = append(b, make([]byte, 14)...)         // no flags, time, version or named entries
		return le.AppendUint16(b, uint16(entries)) // #nosec G115 -- a count of resources
	}

	sec := dir(nil, len(kinds))
	at := numberDirs
	for _, k := range kinds {
		sec = sub(u32(sec, int(k)), at)
		at += dirSize + entrySize*count[k]
	}
	i := 0
	for _, k := range kinds {
		sec = dir(sec, count[k])
		for range count[k] {
			sec = sub(u32(sec, int(list[i].id)), langDirs+(dirSize+entrySize)*i)
			i++
		}
	}
	for i := range list {
		sec = u32(u32(dir(sec, 1), english), leaves+leafSize*i)
	}
	var relocs []int
	at = data
	for _, r := range list {
		// The linker adds the section's address to this offset: see the
		// relocations below.
		relocs = append(relocs, len(sec))
		sec = u32(u32(sec, at), len(r.data))
		sec = append(sec, make([]byte, 8)...) // no code page
		at += (len(r.data) + 7) &^ 7
	}
	for _, r := range list {
		sec = append(sec, r.data...)
		sec = append(sec, make([]byte, (8-len(r.data)%8)%8)...)
	}

	// The object: a header, one section header, the section, a relocation
	// for each leaf, a symbol table with the section's symbol, and an empty
	// string table.
	const header, sectionHeader, relocSize = 20, 40, 10
	relocsAt := header + sectionHeader + len(sec)
	symbolsAt := relocsAt + relocSize*len(relocs)
	name := []byte(".rsrc\x00\x00\x00")

	out := le.AppendUint16(nil, arch.machine)
	out = le.AppendUint16(out, 1) // sections
	out = u32(out, 0)             // no time, so the bytes do not change
	out = u32(out, symbolsAt)
	out = u32(out, 1)             // symbols
	out = le.AppendUint16(out, 0) // no optional header
	out = le.AppendUint16(out, 0) // no flags

	out = append(out, name...)
	out = u32(u32(out, 0), 0) // no address yet
	out = u32(out, len(sec))
	out = u32(out, header+sectionHeader)
	out = u32(out, relocsAt)
	out = u32(out, 0)                               // no line numbers
	out = le.AppendUint16(out, uint16(len(relocs))) // #nosec G115 -- a count of resources
	out = le.AppendUint16(out, 0)                   // no line numbers
	out = le.AppendUint32(out, 0x40000040)          // initialised data, readable
	out = append(out, sec...)
	for _, off := range relocs {
		out = u32(u32(out, off), 0) // against symbol 0
		out = le.AppendUint16(out, arch.reloc)
	}
	out = append(out, name...)
	out = u32(out, 0)             // at the start of
	out = le.AppendUint16(out, 1) // section 1
	out = le.AppendUint16(out, 0) // no type
	out = append(out, 3, 0)       // a static symbol with nothing after it
	return u32(out, 4), nil       // the string table is its own length
}
