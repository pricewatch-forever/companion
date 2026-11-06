package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/config"
)

const token = "Zk8wR2yH5nQe1LxVb7TgCu4MjDs0FaPi6oYtW3rXEhA"

func newStore(t *testing.T) (*config.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "PriceWatch Companion", "config.json")
	return config.NewStore(path), path
}

func TestNothingSavedIsNotLinked(t *testing.T) {
	s, _ := newStore(t)
	c, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Linked() {
		t.Error("linked with nothing saved")
	}
}

func TestSavedIsReadBackAndReadableByTheUserOnly(t *testing.T) {
	s, path := newStore(t)
	upTo := int64(1793805060)
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	want := config.Config{
		Server:     "https://example.convex.site",
		Token:      token,
		LastUpload: at,
		Accounts:   map[string]config.Account{"/game/WTF/Account/A/SavedVariables/PriceWatch.lua": {Answered: true, UploadedUpTo: &upTo}},
	}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config file mode %o, want 600", mode)
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if mode := dir.Mode().Perm(); mode != 0o700 {
		t.Errorf("config folder mode %o, want 700", mode)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Linked() || got.Token != token || got.Server != want.Server || !got.LastUpload.Equal(at) {
		t.Errorf("read back %+v", got)
	}
	a := got.Accounts["/game/WTF/Account/A/SavedVariables/PriceWatch.lua"]
	if !a.Answered || a.UploadedUpTo == nil || *a.UploadedUpTo != upTo {
		t.Errorf("account read back as %+v", a)
	}
}

func TestAFileOthersCouldReadIsMadeTheUsersOnly(t *testing.T) {
	s, path := newStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(config.Config{Token: token}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config file mode %o, want 600", mode)
	}
}

func TestTheTokenIsNeverPrinted(t *testing.T) {
	c := config.Config{Server: "https://example.convex.site", Token: token}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if s := fmt.Sprintf(format, c); strings.Contains(s, token) {
			t.Errorf("%s prints the token: %s", format, s)
		}
		if s := fmt.Sprintf(format, c.Token); strings.Contains(s, token) {
			t.Errorf("%s prints the token: %s", format, s)
		}
	}
}

func TestUpdateChangesOnlyWhileTheSameTokenIsSaved(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(config.Config{Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(token, func(c *config.Config) { c.LastUpload = time.Unix(1, 0) }); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(config.Config{Token: "another"}); err != nil {
		t.Fatal(err)
	}
	err := s.Update(token, func(c *config.Config) { c.LastUpload = time.Unix(2, 0) })
	if !errors.Is(err, config.ErrRelinked) {
		t.Errorf("update after a new link: %v, want ErrRelinked", err)
	}
	c, _ := s.Load()
	if c.Token != "another" || !c.LastUpload.IsZero() {
		t.Errorf("the new link's config was changed: %+v", c)
	}
}
