// Package atomicfile replaces a file whole, so that a reader never sees half
// of one.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to a new file beside path, with mode perm, and renames
// it over path. The folder must exist.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".companion-*") // created 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
