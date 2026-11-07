package loginitem_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pricewatch-forever/companion/internal/loginitem"
)

func TestTheLaunchAgentIsAddedAndRemoved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "LaunchAgents") // made when first needed
	a := loginitem.LaunchAgent{
		Dir:     dir,
		Label:   "com.pricewatchforever.companion",
		Program: []string{"/usr/bin/open", "-a", "/Applications/PriceWatch Companion & Co.app"},
	}
	if a.Enabled() {
		t.Fatal("enabled before it was added")
	}
	if err := a.Disable(); err != nil {
		t.Fatalf("removing what isn't there: %v", err)
	}
	if err := a.Enable(); err != nil {
		t.Fatal(err)
	}
	if !a.Enabled() {
		t.Fatal("not enabled once added")
	}
	path := filepath.Join(dir, "com.pricewatchforever.companion.plist")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<key>Label</key>\n\t<string>com.pricewatchforever.companion</string>",
		"<string>/Applications/PriceWatch Companion &amp; Co.app</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the plist has no %q:\n%s", want, data)
		}
	}
	if plutil, err := exec.LookPath("plutil"); err == nil {
		if out, err := exec.Command(plutil, "-lint", path).CombinedOutput(); err != nil {
			t.Errorf("plutil: %v\n%s", err, out)
		}
	}
	if err := a.Disable(); err != nil {
		t.Fatal(err)
	}
	if a.Enabled() {
		t.Error("still enabled once removed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the plist is still there: %v", err)
	}
}
