// Package wow finds the PriceWatch addon's saved data in a World of Warcraft
// install and watches it for the game's saves.
package wow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/fsnotify/fsnotify"
)

// DefaultInstall is where the game's Forever client is installed on macOS.
const DefaultInstall = "/Applications/World of Warcraft/_forever_"

// FileName is the PriceWatch addon's SavedVariables file.
const FileName = "PriceWatch.lua"

func accountsDir(game string) string { return filepath.Join(game, "WTF", "Account") }

// Files returns every account's PriceWatch.lua in the install at game,
// WTF/Account/*/SavedVariables/PriceWatch.lua, sorted.
func Files(game string) ([]string, error) {
	if info, err := os.Stat(accountsDir(game)); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no WTF/Account folder in %s: is it the game's folder, and has the game been played there?", game)
	}
	files, err := filepath.Glob(filepath.Join(accountsDir(game), "*", "SavedVariables", FileName))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// Account is the name of the account folder a PriceWatch.lua is in.
func Account(file string) string {
	return filepath.Base(filepath.Dir(filepath.Dir(file)))
}

// Watch reports the path of an account's PriceWatch.lua each time it is
// written, until ctx is done. Accounts made after it starts are watched too.
// Watching errors that don't stop it go to warn.
func Watch(ctx context.Context, game string, warn func(error)) (<-chan string, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	root := accountsDir(game)
	if err := w.Add(root); err != nil {
		w.Close()
		return nil, err
	}
	out := make(chan string, 16)
	send := func(path string) {
		select {
		case out <- path:
		case <-ctx.Done():
		}
	}
	// addAccount watches an account folder and its SavedVariables, and
	// reports a PriceWatch.lua already there (made before it was watched).
	addAccount := func(account string) {
		if err := w.Add(account); err != nil {
			warn(err)
			return
		}
		sv := filepath.Join(account, "SavedVariables")
		if info, err := os.Stat(sv); err == nil && info.IsDir() {
			addSavedVariables(w, sv, warn, send)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		w.Close()
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := w.Add(filepath.Join(root, e.Name())); err != nil {
				warn(err)
			}
			if sv := filepath.Join(root, e.Name(), "SavedVariables"); isDir(sv) {
				if err := w.Add(sv); err != nil {
					warn(err)
				}
			}
		}
	}

	go func() {
		defer close(out)
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				warn(err)
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				dir := filepath.Dir(ev.Name)
				switch {
				case filepath.Base(ev.Name) == FileName && filepath.Base(dir) == "SavedVariables":
					if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
						send(ev.Name)
					}
				case ev.Has(fsnotify.Create) && dir == root && isDir(ev.Name):
					addAccount(ev.Name)
				case ev.Has(fsnotify.Create) && filepath.Base(ev.Name) == "SavedVariables" && filepath.Dir(dir) == root && isDir(ev.Name):
					addSavedVariables(w, ev.Name, warn, send)
				}
			}
		}
	}()
	return out, nil
}

func addSavedVariables(w *fsnotify.Watcher, sv string, warn func(error), send func(string)) {
	if err := w.Add(sv); err != nil {
		warn(err)
		return
	}
	file := filepath.Join(sv, FileName)
	if _, err := os.Stat(file); err == nil {
		send(file)
	} else if !errors.Is(err, os.ErrNotExist) {
		warn(err)
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
