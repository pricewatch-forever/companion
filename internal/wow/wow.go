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
	if !isDir(accountsDir(game)) {
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
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &watcher{fs: fw, root: accountsDir(game), out: make(chan string, 16), done: ctx.Done(), warn: warn}
	if err := fw.Add(w.root); err != nil {
		fw.Close()
		return nil, err
	}
	entries, err := os.ReadDir(w.root)
	if err != nil {
		fw.Close()
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			// The caller reads the files there at the start itself.
			w.addAccount(filepath.Join(w.root, e.Name()), false)
		}
	}
	go w.run()
	return w.out, nil
}

type watcher struct {
	fs   *fsnotify.Watcher
	root string // WTF/Account
	out  chan string
	done <-chan struct{}
	warn func(error)
}

func (w *watcher) run() {
	defer close(w.out)
	defer w.fs.Close()
	for {
		select {
		case <-w.done:
			return
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			w.warn(err)
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			dir := filepath.Dir(ev.Name)
			switch {
			case filepath.Base(ev.Name) == FileName && filepath.Base(dir) == "SavedVariables":
				if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
					w.send(ev.Name)
				}
			case ev.Has(fsnotify.Create) && dir == w.root && isDir(ev.Name):
				w.addAccount(ev.Name, true)
			case ev.Has(fsnotify.Create) && filepath.Base(ev.Name) == "SavedVariables" && filepath.Dir(dir) == w.root && isDir(ev.Name):
				w.addSavedVariables(ev.Name, true)
			}
		}
	}
}

func (w *watcher) send(path string) {
	select {
	case w.out <- path:
	case <-w.done:
	}
}

// addAccount watches an account folder and its SavedVariables. With report,
// it reports a PriceWatch.lua already there, made before it was watched.
func (w *watcher) addAccount(account string, report bool) {
	if err := w.fs.Add(account); err != nil {
		w.warn(err)
		return
	}
	if savedVars := filepath.Join(account, "SavedVariables"); isDir(savedVars) {
		w.addSavedVariables(savedVars, report)
	}
}

func (w *watcher) addSavedVariables(savedVars string, report bool) {
	if err := w.fs.Add(savedVars); err != nil {
		w.warn(err)
		return
	}
	file := filepath.Join(savedVars, FileName)
	if _, err := os.Stat(file); err == nil && report {
		w.send(file)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		w.warn(err)
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
