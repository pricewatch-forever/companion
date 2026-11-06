package atomicfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pricewatch-forever/companion/internal/atomicfile"
)

func TestWriteReplacesTheFileWholeWithItsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old, longer text"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("read %q, %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %o, want 600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("left %d files in the folder, want 1", len(entries))
	}
}

func TestWriteIntoAMissingFolderFails(t *testing.T) {
	if err := atomicfile.Write(filepath.Join(t.TempDir(), "none", "f"), []byte("x"), 0o644); err == nil {
		t.Error("no error")
	}
}
