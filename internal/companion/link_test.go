package companion_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/companion"
	"github.com/pricewatch-forever/companion/internal/config"
)

const (
	linkAnswer = `{"code":"K7QM-R4TX","secret":"q3VnX0pT8sLhYb2RkWc6ZfJ1uEoAa9MdGiN5eC7xPtw","url":"https://pricewatchforever.com/link?code=K7QM-R4TX","expiresIn":600,"interval":5}`
	secret     = "q3VnX0pT8sLhYb2RkWc6ZfJ1uEoAa9MdGiN5eC7xPtw"
)

type linkHarness struct {
	t      *testing.T
	srv    *httptest.Server
	store  *config.Store
	clock  *fakeClock
	out    *safeBuffer
	opened []string

	mu    sync.Mutex
	polls []string // answers to give, in order; the last one repeats
	asked []map[string]any
	waits []time.Duration
}

func newLinkHarness(t *testing.T, polls ...string) *linkHarness {
	h := &linkHarness{
		t:     t,
		store: config.NewStore(filepath.Join(t.TempDir(), "config.json")),
		clock: newFakeClock(),
		out:   &safeBuffer{},
		polls: polls,
	}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		h.mu.Lock()
		defer h.mu.Unlock()
		h.asked = append(h.asked, body)
		switch r.URL.Path {
		case "/companion/v1/link":
			_, _ = io.WriteString(w, linkAnswer)
		case "/companion/v1/link/poll":
			if body["secret"] != secret {
				t.Errorf("polled with %v", body["secret"])
			}
			a := h.polls[0]
			if len(h.polls) > 1 {
				h.polls = h.polls[1:]
			}
			if strings.HasPrefix(a, "5") {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = io.WriteString(w, a)
		default:
			t.Errorf("request to %s", r.URL.Path)
		}
	}))
	t.Cleanup(h.srv.Close)
	// Time passes as soon as the Companion waits.
	go func() {
		for d := range h.clock.waits {
			h.mu.Lock()
			h.waits = append(h.waits, d)
			h.mu.Unlock()
			h.clock.Advance(d)
		}
	}()
	t.Cleanup(func() { close(h.clock.waits) })
	return h
}

func (h *linkHarness) link() error {
	l := &companion.Linker{
		Client: api.New(h.srv.URL, "1.0.0"),
		Server: h.srv.URL,
		Store:  h.store,
		Clock:  h.clock,
		Open:   func(url string) error { h.opened = append(h.opened, url); return nil },
		Out:    h.out,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return l.Link(ctx, "Mara's Mac")
}

func TestLinkingShowsTheCodeOpensThePageAndKeepsTheToken(t *testing.T) {
	h := newLinkHarness(t, `{"status":"pending"}`, `{"status":"pending"}`, `{"status":"linked","token":"`+token+`"}`)
	if err := h.link(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.out.String(), "K7QM-R4TX") {
		t.Errorf("the code wasn't shown:\n%s", h.out)
	}
	if strings.Contains(h.out.String(), token) {
		t.Errorf("the token was printed:\n%s", h.out)
	}
	if len(h.opened) != 1 || h.opened[0] != "https://pricewatchforever.com/link?code=K7QM-R4TX" {
		t.Errorf("opened %v", h.opened)
	}
	if first := h.asked[0]; first["name"] != "Mara's Mac" || first["version"] != "1.0.0" {
		t.Errorf("asked for a link with %v", first)
	}
	if len(h.asked) != 4 {
		t.Errorf("%d requests, want a link and 3 polls", len(h.asked))
	}
	for _, d := range h.waits {
		if d != 5*time.Second {
			t.Errorf("waited %v between polls, want the interval, 5s", d)
		}
	}
	c, err := h.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != token || c.Server != h.srv.URL {
		t.Errorf("config %+v, want the token and the server", c)
	}
}

func TestLinkingAgainForgetsTheEarlierLinksTimes(t *testing.T) {
	h := newLinkHarness(t, `{"status":"linked","token":"`+token+`"}`)
	if err := h.store.Save(config.Config{Token: "old", UploadedUpTo: map[string]int64{"x": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := h.link(); err != nil {
		t.Fatal(err)
	}
	c, _ := h.store.Load()
	if c.Token != token || len(c.UploadedUpTo) != 0 {
		t.Errorf("config %+v", c)
	}
}

func TestAnExpiredLinkStopsPolling(t *testing.T) {
	h := newLinkHarness(t, `{"status":"pending"}`, `{"status":"expired"}`)
	err := h.link()
	if !errors.Is(err, companion.ErrLinkExpired) {
		t.Errorf("error %v, want ErrLinkExpired", err)
	}
	if len(h.asked) != 3 {
		t.Errorf("%d requests, want a link and 2 polls", len(h.asked))
	}
	if c, _ := h.store.Load(); c.Linked() {
		t.Error("linked after an expired link")
	}
}

func TestAFailedPollIsTriedAgain(t *testing.T) {
	h := newLinkHarness(t, "503", `{"status":"linked","token":"`+token+`"}`)
	if err := h.link(); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.store.Load(); c.Token != token {
		t.Error("not linked after a failed poll and a good one")
	}
}
