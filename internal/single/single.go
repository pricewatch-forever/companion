// Package single makes sure only one copy of the app runs: the first takes
// a lock on a file, and a second finds it held. The system lets go of the
// lock when the copy holding it ends, however it ends.
package single

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrHeld means another copy holds the lock.
var ErrHeld = errors.New("another copy holds the lock")

// Lock takes the lock on the file at path, making the file if needed. It
// returns ErrHeld if another copy holds it. Release lets go of it.
func Lock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
