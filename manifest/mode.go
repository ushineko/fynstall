package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// DetectMode returns 0755 for an ELF or PE executable or a script with a
// "#!" line, and 0644 for anything else. embed.FS does not keep file modes,
// so the manifest has to carry them (spec 001, R3).
func DetectMode(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("detect mode: %w", err)
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 4)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("detect mode of %s: %w", path, err)
	}
	return ModeOf(head[:n]), nil
}

// ModeOf is DetectMode on the first bytes of a file.
func ModeOf(head []byte) uint32 {
	switch {
	case bytes.HasPrefix(head, []byte("\x7fELF")),
		bytes.HasPrefix(head, []byte("MZ")),
		bytes.HasPrefix(head, []byte("#!")):
		return 0o755
	default:
		return 0o644
	}
}
