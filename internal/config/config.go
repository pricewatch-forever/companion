// Package config keeps the Companion's own settings and state, the device
// token among them, in one file readable by the user only.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/atomicfile"
)

// Config is the Companion's config file.
type Config struct {
	// Server is the address of the server the Companion is linked to.
	Server string `json:"server,omitempty"`
	// Token is the device token, empty while not linked.
	Token api.Token `json:"token,omitempty"`
	// LastUpload is when the last upload was answered.
	LastUpload time.Time `json:"lastUpload,omitzero"`
	// UploadedUpTo is, by PriceWatch.lua path, the last uploaded-up-to time
	// the server answered for that file. A file it never answered a time
	// for has none.
	UploadedUpTo map[string]int64 `json:"uploadedUpTo,omitempty"`
}

// Linked reports whether the Companion has a device token.
func (c Config) Linked() bool { return c.Token != "" }

// ErrRelinked means the config was linked again since it was read.
var ErrRelinked = errors.New("the Companion was linked again meanwhile")

// Store is the config file at one path.
type Store struct {
	path string
}

// NewStore returns the store for the config file at path.
func NewStore(path string) *Store { return &Store{path: path} }

// DefaultPath is the config file in the user's config folder.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "PriceWatch Companion", "config.json"), nil
}

// Load reads the config file. With none, it is an empty, unlinked config.
func (s *Store) Load() (Config, error) {
	var c Config
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, errors.New("the config file " + s.path + " can't be read: " + err.Error())
	}
	return c, nil
}

// Save replaces the config file with c, readable by the user only.
func (s *Store) Save(c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.path, append(data, '\n'), 0o600)
}

// Update reads the config, changes it with change and saves it, as long as
// it still holds token: a link made meanwhile is never overwritten. It
// returns the config as saved.
func (s *Store) Update(token api.Token, change func(*Config)) (Config, error) {
	c, err := s.Load()
	if err != nil {
		return c, err
	}
	if c.Token != token {
		return c, ErrRelinked
	}
	change(&c)
	return c, s.Save(c)
}
