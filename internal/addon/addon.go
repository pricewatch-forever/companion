// Package addon writes the Companion's own data addon, PriceWatch_Companion,
// into the game install. The PriceWatch addon reads it at login to learn
// whether the Companion is linked and up to when it has uploaded.
package addon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Name is the data addon's folder name under Interface/AddOns.
const Name = "PriceWatch_Companion"

// State is what the Companion tells the addon.
type State struct {
	Linked bool
	// UploadedUpTo is the server time up to which everything is uploaded,
	// or nil until the first upload has been answered. Ignored unlinked.
	UploadedUpTo *int64
}

const toc = `## Interface: 16001
## Title: PriceWatch Companion
## Notes: Written by the PriceWatch Companion app: tells PriceWatch what it has uploaded.
## Author: PriceWatch Forever
` + Name + `.lua
`

// Dir is the data addon's folder in the install at game.
func Dir(game string) string {
	return filepath.Join(game, "Interface", "AddOns", Name)
}

// Write writes the data addon into the install at game, and reports whether
// its folder was new (the game only finds a new addon when it starts).
func Write(game string, s State) (created bool, err error) {
	addons := filepath.Join(game, "Interface", "AddOns")
	if info, err := os.Stat(addons); err != nil || !info.IsDir() {
		return false, fmt.Errorf("no Interface/AddOns folder in %s: is it the game's folder?", game)
	}
	dir := Dir(game)
	switch err := os.Mkdir(dir, 0o755); {
	case err == nil:
		created = true
	case errors.Is(err, fs.ErrExist):
	default:
		return false, err
	}
	if err := writeFile(filepath.Join(dir, Name+".toc"), []byte(toc)); err != nil {
		return created, err
	}
	return created, writeFile(filepath.Join(dir, Name+".lua"), []byte(Lua(s)))
}

// Lua is the data addon's one line.
func Lua(s State) string {
	if !s.Linked {
		return "PriceWatchCompanion = { linked = false }\n"
	}
	if s.UploadedUpTo == nil {
		return "PriceWatchCompanion = { linked = true }\n"
	}
	return fmt.Sprintf("PriceWatchCompanion = { linked = true, uploadedUpTo = %d }\n", *s.UploadedUpTo)
}

// writeFile replaces a file whole, so the game never reads half of one.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".companion-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
