package installer

import (
	"bytes"
	"encoding/binary"
)

// icoPNG is the largest image of the .ico file ico that is stored as a
// PNG, or nil when it has none. The builder stores the 256-pixel image that
// way (builder, icon.go), and a window takes its icon as a PNG.
func icoPNG(ico []byte) []byte {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico) != 0 || le.Uint16(ico[2:]) != 1 {
		return nil
	}
	var best []byte
	bestWidth := 0
	for i := range int(le.Uint16(ico[4:])) {
		at := 6 + 16*i
		if at+16 > len(ico) {
			return nil
		}
		entry := ico[at : at+16]
		width := int(entry[0])
		if width == 0 {
			width = 256 // the field is one byte, and 0 stands for 256
		}
		size, offset := int(le.Uint32(entry[8:])), int(le.Uint32(entry[12:]))
		if size < 0 || offset < 0 || offset+size > len(ico) {
			return nil
		}
		data := ico[offset : offset+size]
		if width > bestWidth && bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			best, bestWidth = data, width
		}
	}
	return best
}
