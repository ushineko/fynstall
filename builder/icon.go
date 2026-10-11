package builder

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"

	"github.com/ushineko/fynstall/manifest"
	"github.com/ushineko/fynstall/platform"
)

// icons resizes the PNG at src to every hicolor size (R4). The scaler and
// the encoder are deterministic, so the same source gives the same bytes.
func icons(src string) ([]manifest.Icon, map[string][]byte, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, nil, fmt.Errorf("icon: %w", err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		return nil, nil, fmt.Errorf("icon %s: %w", src, err)
	}
	var list []manifest.Icon
	files := map[string][]byte{}
	for _, size := range platform.HicolorSizes {
		dst := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, nil, fmt.Errorf("icon %s at %d px: %w", src, size, err)
		}
		sum := sha256.Sum256(buf.Bytes())
		ic := manifest.Icon{Size: size, Bytes: int64(buf.Len()), SHA256: hex.EncodeToString(sum[:])}
		list = append(list, ic)
		files[ic.Path()] = buf.Bytes()
	}
	return list, files, nil
}

// icoSizes are the sizes in a Windows icon file. Explorer, the Start Menu
// and Settings > Apps each ask for one of them, at 100% to 200% scaling.
var icoSizes = []int{16, 32, 48, 64, 256} //nolint:gochecknoglobals // a fixed table

// ico makes the PNG at src into a Windows .ico file with every size in
// icoSizes. The 256 px image is stored as a PNG, as Windows does it; the
// smaller ones are plain 32-bit bitmaps, which every reader of the format
// takes. The same source gives the same bytes.
func ico(src string) ([]byte, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, fmt.Errorf("icon: %w", err)
	}
	defer func() { _ = f.Close() }()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("icon %s: %w", src, err)
	}
	images := make([][]byte, len(icoSizes))
	for i, size := range icoSizes {
		scaled := image.NewNRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Src, nil)
		if size == 256 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, scaled); err != nil {
				return nil, fmt.Errorf("icon %s at %d px: %w", src, size, err)
			}
			images[i] = buf.Bytes()
			continue
		}
		images[i] = icoBitmap(scaled)
	}

	// The file is a 6-byte header, a 16-byte entry for each image, then
	// the images. Every number is little-endian. The counts and sizes are
	// those of five small images, far inside the fields that hold them.
	le := binary.LittleEndian
	out := le.AppendUint16(nil, 0)
	out = le.AppendUint16(out, 1)                   // 1: icons
	out = le.AppendUint16(out, uint16(len(images))) // #nosec G115 -- see above
	offset := 6 + 16*len(images)
	for i, size := range icoSizes {
		out = append(out, byte(size), byte(size), 0, 0)    // #nosec G115 -- 256 is written as 0, which is what the cut gives
		out = le.AppendUint16(out, 1)                      // colour planes
		out = le.AppendUint16(out, 32)                     // bits for each pixel
		out = le.AppendUint32(out, uint32(len(images[i]))) // #nosec G115 -- see above
		out = le.AppendUint32(out, uint32(offset))         // #nosec G115 -- see above
		offset += len(images[i])
	}
	for _, b := range images {
		out = append(out, b...)
	}
	return out, nil
}

// icoBitmap is img as an icon file stores a bitmap: a BITMAPINFOHEADER
// whose height counts the image and its mask, the pixels as BGRA from the
// bottom row up, then a 1-bit mask, which is all zero because the alpha
// channel carries the transparency.
func icoBitmap(img *image.NRGBA) []byte {
	size := img.Bounds().Dx()
	le := binary.LittleEndian
	out := le.AppendUint32(nil, 40)            // header size
	out = le.AppendUint32(out, uint32(size))   // #nosec G115 -- at most 64
	out = le.AppendUint32(out, uint32(2*size)) // #nosec G115 -- as above
	out = le.AppendUint16(out, 1)
	out = le.AppendUint16(out, 32)
	out = append(out, make([]byte, 24)...) // no compression; the rest may be zero
	for y := size - 1; y >= 0; y-- {
		row := img.Pix[y*img.Stride : y*img.Stride+4*size]
		for x := 0; x < len(row); x += 4 {
			out = append(out, row[x+2], row[x+1], row[x], row[x+3])
		}
	}
	maskRow := (size + 31) / 32 * 4
	return append(out, make([]byte, maskRow*size)...)
}
