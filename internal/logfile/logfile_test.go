package logfile_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pricewatch-forever/companion/internal/logfile"
)

func TestTheLogIsRotatedSoItStaysSmall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Logs", "PriceWatch Companion") // made when opened
	path := filepath.Join(dir, "companion.log")
	l, err := logfile.Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		fmt.Fprintf(l, "line %02d\n", i) // 8 bytes each
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("%d files in the log folder, want the log and one older", len(entries))
	}
	now, _ := os.ReadFile(path)
	old, _ := os.ReadFile(filepath.Join(dir, "companion.old.log"))
	if len(now) > 100 || len(old) > 100 {
		t.Errorf("the log holds %d bytes and the older one %d, want 100 at most", len(now), len(old))
	}
	if !strings.HasSuffix(string(now), "line 29\n") || !strings.HasSuffix(string(old), lineBefore(string(now))) {
		t.Errorf("log:\n%s\nolder:\n%s", now, old)
	}

	// Opened again, it goes on where it was.
	l, err = logfile.Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(l, "line 30\n")
	l.Close()
	again, _ := os.ReadFile(path)
	if string(again) != string(now)+"line 30\n" {
		t.Errorf("reopened, the log is:\n%s", again)
	}
}

// lineBefore is the line logged just before the first in log.
func lineBefore(log string) string {
	var n int
	fmt.Sscanf(log, "line %d", &n)
	return fmt.Sprintf("line %02d\n", n-1)
}
