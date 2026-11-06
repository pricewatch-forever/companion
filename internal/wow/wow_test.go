package wow_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/wow"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilesFindsEveryAccountsPriceWatchLua(t *testing.T) {
	game := t.TempDir()
	a := filepath.Join(game, "WTF", "Account", "AAA", "SavedVariables", "PriceWatch.lua")
	b := filepath.Join(game, "WTF", "Account", "BBB", "SavedVariables", "PriceWatch.lua")
	writeFile(t, a, "")
	writeFile(t, b, "")
	writeFile(t, filepath.Join(game, "WTF", "Account", "AAA", "SavedVariables", "PriceWatch.lua.bak"), "")
	writeFile(t, filepath.Join(game, "WTF", "Account", "AAA", "SavedVariables", "Other.lua"), "")
	writeFile(t, filepath.Join(game, "WTF", "Account", "CCC", "SavedVariables", "Other.lua"), "")

	got, err := wow.Files(game)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{a, b}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFilesWithoutAnAccountFolderIsAnError(t *testing.T) {
	if _, err := wow.Files(t.TempDir()); err == nil {
		t.Error("no error for a folder without WTF/Account")
	}
}

func TestAccountIsTheAccountFolder(t *testing.T) {
	if got := wow.Account(filepath.Join("game", "WTF", "Account", "12345#1", "SavedVariables", "PriceWatch.lua")); got != "12345#1" {
		t.Errorf("got %q", got)
	}
}

// The watcher reports each save of a PriceWatch.lua, in an account that was
// there at the start or one made later, and nothing else.
func TestWatchReportsSavesOfPriceWatchLua(t *testing.T) {
	game := t.TempDir()
	a := filepath.Join(game, "WTF", "Account", "AAA", "SavedVariables", "PriceWatch.lua")
	writeFile(t, a, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := wow.Watch(ctx, game, func(err error) { t.Log(err) })
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(game, "WTF", "Account", "AAA", "SavedVariables", "Other.lua"), "x")
	writeFile(t, a+".bak", "x")
	writeFile(t, a, "x = 1")
	expectEvent(t, events, a)

	// A file written beside it and renamed over it, as some programs save.
	tmp := filepath.Join(game, "WTF", "Account", "AAA", "SavedVariables", "PriceWatch.tmp")
	writeFile(t, tmp, "x = 2")
	if err := os.Rename(tmp, a); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, events, a)

	b := filepath.Join(game, "WTF", "Account", "BBB", "SavedVariables", "PriceWatch.lua")
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // let the watcher see the new folders
	writeFile(t, b, "y = 1")
	expectEvent(t, events, b)
}

func expectEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case got := <-events:
			if got == want {
				return
			}
			if filepath.Base(got) != "PriceWatch.lua" {
				t.Fatalf("event for %s, want %s", got, want)
			}
		case <-timeout:
			t.Fatalf("no event for %s", want)
		}
	}
}
