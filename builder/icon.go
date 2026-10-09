package builder

import (
	"bytes"
	"crypto/sha256"
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
