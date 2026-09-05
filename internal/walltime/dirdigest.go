package walltime

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirectoryDigest is the identity of an action directory: the SHA-256 over a
// lexicographically sorted listing of `relative path`, file mode, and the file's
// own SHA-256.
//
// A symlink is REJECTED rather than followed or recorded. Following one would
// let a directory's identity depend on something outside it; recording the link
// text would let two different trees share a digest. Neither is an identity.
func DirectoryDigest(dir string) (Digest, error) {
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("walltime: %s is a symlink; an action directory's identity cannot depend on a link target", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s\x00%04o\x00%s", filepath.ToSlash(rel), info.Mode().Perm(), DigestBytes(b)))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("walltime: %s contains no files", dir)
	}
	sort.Strings(lines)
	return DigestBytes([]byte(strings.Join(lines, "\n"))), nil
}

// FileDigest is the SHA-256 of a file's exact bytes — the binary identity the
// delivery manifest records.
func FileDigest(path string) (Digest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return DigestBytes(b), nil
}
