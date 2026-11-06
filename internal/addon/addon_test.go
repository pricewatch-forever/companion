package addon_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pricewatch-forever/companion/internal/addon"
)

func newInstall(t *testing.T) string {
	t.Helper()
	game := t.TempDir()
	if err := os.MkdirAll(filepath.Join(game, "Interface", "AddOns", "PriceWatch"), 0o755); err != nil {
		t.Fatal(err)
	}
	return game
}

func readData(t *testing.T, game string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(game, "Interface", "AddOns", "PriceWatch_Companion", "PriceWatch_Companion.lua"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ptr(v int64) *int64 { return &v }

func TestLinkedWithATime(t *testing.T) {
	game := newInstall(t)
	if _, err := addon.Write(game, addon.State{Linked: true, UploadedUpTo: ptr(1793804400)}); err != nil {
		t.Fatal(err)
	}
	want := "PriceWatchCompanion = { linked = true, uploadedUpTo = 1793804400 }\n"
	if got := readData(t, game); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLinkedBeforeTheFirstUpload(t *testing.T) {
	game := newInstall(t)
	if _, err := addon.Write(game, addon.State{Linked: true}); err != nil {
		t.Fatal(err)
	}
	want := "PriceWatchCompanion = { linked = true }\n"
	if got := readData(t, game); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnlinkedHasNoTime(t *testing.T) {
	game := newInstall(t)
	if _, err := addon.Write(game, addon.State{Linked: false, UploadedUpTo: ptr(1793804400)}); err != nil {
		t.Fatal(err)
	}
	want := "PriceWatchCompanion = { linked = false }\n"
	if got := readData(t, game); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheTocIsAPlainAddonForForever(t *testing.T) {
	game := newInstall(t)
	if _, err := addon.Write(game, addon.State{Linked: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(game, "Interface", "AddOns", "PriceWatch_Companion", "PriceWatch_Companion.toc"))
	if err != nil {
		t.Fatal(err)
	}
	toc := string(b)
	for _, want := range []string{"## Interface: 16001\n", "\nPriceWatch_Companion.lua\n"} {
		if !strings.Contains(toc, want) {
			t.Errorf("toc %q has no %q", toc, want)
		}
	}
	if strings.Contains(toc, "LoadOnDemand") {
		t.Errorf("toc %q is load-on-demand", toc)
	}
}

func TestSaysWhenTheFolderIsNew(t *testing.T) {
	game := newInstall(t)
	created, err := addon.Write(game, addon.State{Linked: true})
	if err != nil || !created {
		t.Fatalf("first write: created %v, %v", created, err)
	}
	created, err = addon.Write(game, addon.State{Linked: true, UploadedUpTo: ptr(1)})
	if err != nil || created {
		t.Fatalf("second write: created %v, %v", created, err)
	}
}

func TestTouchesNoOtherFile(t *testing.T) {
	game := newInstall(t)
	if _, err := addon.Write(game, addon.State{Linked: true}); err != nil {
		t.Fatal(err)
	}
	var files []string
	_ = filepath.Walk(game, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(game, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	want := "Interface/AddOns/PriceWatch_Companion/PriceWatch_Companion.lua Interface/AddOns/PriceWatch_Companion/PriceWatch_Companion.toc"
	if got := strings.Join(files, " "); got != want {
		t.Errorf("files %s, want %s", got, want)
	}
}

func TestNoAddOnsFolderIsAnError(t *testing.T) {
	if _, err := addon.Write(t.TempDir(), addon.State{Linked: true}); err == nil {
		t.Error("wrote into a folder that isn't a game install")
	}
}
