package installer

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

// An icon file as the builder writes one: a small bitmap, then a PNG.
func testIco(t *testing.T) (ico, want []byte) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 256, 256))))
	want = buf.Bytes()
	bitmap := make([]byte, 40)
	le := binary.LittleEndian
	ico = []byte{0, 0, 1, 0, 2, 0}
	offset := 6 + 16*2
	for _, im := range []struct {
		width byte
		data  []byte
	}{{16, bitmap}, {0, want}} {
		ico = append(ico, im.width, im.width, 0, 0, 1, 0, 32, 0)
		ico = le.AppendUint32(ico, uint32(len(im.data)))
		ico = le.AppendUint32(ico, uint32(offset))
		offset += len(im.data)
	}
	return append(append(ico, bitmap...), want...), want
}

func TestTheWindowTakesThePNGOfAnIconFile(t *testing.T) {
	ico, want := testIco(t)
	require.Equal(t, want, icoPNG(ico))
	require.Nil(t, icoPNG(ico[:30]), "a file cut short has no image")
	require.Nil(t, icoPNG([]byte("not an icon")))
	require.Nil(t, icoPNG(ico[:6+16*2+40]), "an image that ends outside the file")
}
