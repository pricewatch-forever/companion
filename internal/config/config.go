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
)

// Token is the device token. It prints as [hidden] so that it never ends up
// in the terminal or a log by accident.
type Token string

func (Token) String() string   { return "[hidden]" }
func (Token) GoString() string { return `"[hidden]"` }

// Account is what the server has answered about one PriceWatch.lua.
type Account struct {
	// Answered is true once an upload of the file has been answered.
	Answered bool `json:"answered"`
	// UploadedUpTo is the last time the server answered, or nil if it
	// answered none.
	UploadedUpTo *int64 `json:"uploadedUpTo,omitempty"`
}

// Config is the Companion's config file.
type Config struct {
	// Server is the address of the server the Companion is linked to.
	Server string `json:"server,omitempty"`
	// Token is the device token, empty while not linked.
	Token Token `json:"token,omitempty"`
	// LastUpload is when the last upload was answered.
	LastUpload time.Time `json:"lastUpload,omitzero"`
	// Accounts is the server's answers by PriceWatch.lua path.
	Accounts map[string]Account `json:"accounts,omitempty"`
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

// Path is where the config file is.
func (s *Store) Path() string { return s.path }

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
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*") // created 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Update reads the config, changes it with change and saves it, as long as
// it still holds token: a link made meanwhile is never overwritten.
func (s *Store) Update(token Token, change func(*Config)) error {
	c, err := s.Load()
	if err != nil {
		return err
	}
	if c.Token != token {
		return ErrRelinked
	}
	change(&c)
	return s.Save(c)
}
