/*
Package snapshot records a directory tree as path → type, mode and content
hash, so a test can say "the system is as it was before the install" with
one comparison (spec 001, R9d).
*/
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Take walks root and returns one line per entry, keyed by slash path
// relative to root. Directories, files and symlinks are told apart, and a
// file's mode and sha256 are part of its line.
func Take(root string) (map[string]string, error) {
	out := map[string]string{}
	// A test helper over a temporary directory the test owns, so the
	// symlink races gosec warns about in WalkDir callbacks do not apply.
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("rel: %w", err)
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", p, err)
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("readlink: %w", err)
			}
			out[rel] = "link " + target
		case d.IsDir():
			out[rel] = fmt.Sprintf("dir %o", info.Mode().Perm())
		default:
			b, err := os.ReadFile(p) // #nosec G122 -- see above
			if err != nil {
				return fmt.Errorf("read: %w", err)
			}
			sum := sha256.Sum256(b)
			out[rel] = fmt.Sprintf("file %o %s", info.Mode().Perm(), hex.EncodeToString(sum[:]))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", root, err)
	}
	return out, nil
}
