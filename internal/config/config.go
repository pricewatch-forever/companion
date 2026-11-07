// Package config keeps the Companion's own settings and state, the device
// token among them, in one file readable by the user only.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
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
	// StartAtLogin is the player's choice of "Start when I log in", nil
	// until they make one. A link keeps it.
	StartAtLogin *bool `json:"startAtLogin,omitempty"`
}

// Linked reports whether the Companion has a device token.
func (c Config) Linked() bool { return c.Token != "" }

// ErrRelinked means the config was linked again since it was read.
var ErrRelinked = errors.New("the Companion was linked again meanwhile")

// Store is the config file at one path. Its changes are made one at a
// time within this program; another program's change made meanwhile can
// still be overwritten, except a link (see Update).
type Store struct {
	path string
	mu   sync.Mutex
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
	return s.change(func(c Config) error {
		if c.Token != token {
			return ErrRelinked
		}
		return nil
	}, change)
}

// Change reads the config, changes it with change and saves it, whatever
// link it holds (unlike Update).
func (s *Store) Change(change func(*Config)) error {
	_, err := s.change(func(Config) error { return nil }, change)
	return err
}

// change changes the config as long as check passes, and returns it.
func (s *Store) change(check func(Config) error, change func(*Config)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.Load()
	if err != nil {
		return c, err
	}
	if err := check(c); err != nil {
		return c, err
	}
	change(&c)
	return c, s.Save(c)
}

// Link saves a new link, to server with token. It forgets the earlier
// link's state and keeps the player's choices. A config that can't be read
// is replaced.
func (s *Store) Link(server string, token api.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, _ := s.Load()
	return s.Save(Config{Server: server, Token: token, StartAtLogin: old.StartAtLogin})
}
