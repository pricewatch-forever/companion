package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const token = "Zk8wR2yH5nQe1LxVb7TgCu4MjDs0FaPi6oYtW3rXEhA"

// useTempConfigFolder points the user's config folder at a temp folder.
func useTempConfigFolder(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "PriceWatch Companion", "config.json")
}

func fakeLinkServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/companion/v1/link":
			_, _ = io.WriteString(w, `{"code":"K7QM-R4TX","secret":"q3VnX0pT8sLhYb2RkWc6ZfJ1uEoAa9MdGiN5eC7xPtw","url":"https://pricewatchforever.com/link?code=K7QM-R4TX","expiresIn":600,"interval":1}`)
		case "/companion/v1/link/poll":
			_, _ = io.WriteString(w, `{"status":"linked","token":"`+token+`"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLinkThenStatusNeverShowTheToken(t *testing.T) {
	path := useTempConfigFolder(t)
	srv := fakeLinkServer(t)
	var opened []string
	openBrowser = func(page string) error { opened = append(opened, page); return nil }

	var out, errOut bytes.Buffer
	if code := run([]string{"link", "--server", srv.URL}, &out, &errOut); code != 0 {
		t.Fatalf("link exited %d: %s%s", code, out.String(), errOut.String())
	}
	if len(opened) != 1 || !strings.Contains(out.String(), "K7QM-R4TX") {
		t.Errorf("opened %v, said:\n%s", opened, out.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("no config file at %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config file mode %o, want 600", info.Mode().Perm())
	}

	if code := run([]string{"status"}, &out, &errOut); code != 0 {
		t.Fatalf("status exited %d", code)
	}
	if !strings.Contains(out.String(), "Linked") || !strings.Contains(out.String(), "Nothing uploaded yet") {
		t.Errorf("status said:\n%s", out.String())
	}
	if strings.Contains(out.String()+errOut.String(), token) {
		t.Errorf("the token was printed:\n%s%s", out.String(), errOut.String())
	}
}

func TestRunUnlinkedSaysToLink(t *testing.T) {
	useTempConfigFolder(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--game", t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("exited %d", code)
	}
	if !strings.Contains(errOut.String(), "companion link") {
		t.Errorf("said:\n%s%s", out.String(), errOut.String())
	}
}

func TestRunOnAnotherServerThanLinkedIsRefused(t *testing.T) {
	useTempConfigFolder(t)
	srv := fakeLinkServer(t)
	openBrowser = func(string) error { return nil }
	var out, errOut bytes.Buffer
	if code := run([]string{"link", "--server", srv.URL}, &out, &errOut); code != 0 {
		t.Fatalf("link exited %d: %s", code, errOut.String())
	}
	errOut.Reset()
	if code := run([]string{"run", "--server", "https://other.example", "--game", t.TempDir()}, &out, &errOut); code != 1 {
		t.Errorf("exited %d", code)
	}
	if !strings.Contains(errOut.String(), "linked on "+srv.URL) {
		t.Errorf("said:\n%s", errOut.String())
	}
}

func TestStatusUnlinked(t *testing.T) {
	useTempConfigFolder(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"status"}, &out, &errOut); code != 0 {
		t.Fatalf("exited %d", code)
	}
	if !strings.HasPrefix(out.String(), "Not linked") {
		t.Errorf("said:\n%s", out.String())
	}
}

func TestHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &out, &errOut); code != 0 {
		t.Fatalf("exited %d", code)
	}
	for _, want := range []string{"companion link", "companion run", "companion status", "--game", "--server"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help has no %q", want)
		}
	}
}

func TestComputerNameFitsTheServersLimit(t *testing.T) {
	name := computerName()
	if name == "" || len(name) > 64 || strings.HasSuffix(name, ".local") {
		t.Errorf("computer name %q", name)
	}
}

func TestOnlyWebAddressesAreServersOrOpened(t *testing.T) {
	for addr, want := range map[string]bool{
		"https://example.convex.site":  true,
		"http://localhost:3000/link":   true,
		"file:///etc/passwd":           false,
		"javascript:alert(1)":          false,
		"https://":                     false,
		"example.convex.site":          false,
		"x-apple.systempreferences:ab": false,
	} {
		if got := isWebAddress(addr); got != want {
			t.Errorf("isWebAddress(%q) = %v, want %v", addr, got, want)
		}
	}
}
