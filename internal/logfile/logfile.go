// Package logfile is the Companion's log: what it did, for when something
// goes wrong. It is rotated so it stays small: once the log is full, it
// becomes the older log, replacing the one before, and a new log starts.
package logfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// MaxSize is the most a log file holds, in bytes.
const MaxSize = 512 << 10

// DefaultPath is the log in the user's log folder: on macOS,
// ~/Library/Logs/PriceWatch Companion/companion.log.
func DefaultPath() (string, error) {
	var dir string
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "Library", "Logs")
	} else {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = cache
	}
	return filepath.Join(dir, "PriceWatch Companion", "companion.log"), nil
}

// File is a log file that is rotated once it holds max bytes.
type File struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

// Open opens the log at path to add to it, making its folder if needed.
func Open(path string, max int64) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l := &File{path: path, max: max}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, info.Size()
	return nil
}

// Older is where the log before this one is kept: companion.old.log.
func (l *File) Older() string {
	ext := filepath.Ext(l.path)
	return strings.TrimSuffix(l.path, ext) + ".old" + ext
}

// Path is the log file's path.
func (l *File) Path() string { return l.path }

// Write adds p to the log, rotating it first if p would make it too large.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *File) rotate() error {
	if err := l.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(l.path, l.Older()); err != nil {
		return err
	}
	return l.open()
}

// Close closes the log.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
