package companion_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/companion"
	"github.com/pricewatch-forever/companion/internal/config"
)

const token = "Zk8wR2yH5nQe1LxVb7TgCu4MjDs0FaPi6oYtW3rXEhA"

// fakeClock is a clock the test moves by hand. Each time the runner waits,
// it reports how long on waits, so the test knows the runner is idle.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
	waits  chan time.Duration
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC), waits: make(chan time.Duration, 100)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	c.mu.Unlock()
	c.waits <- d
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			kept = append(kept, t)
		} else {
			t.ch <- c.now
		}
	}
	c.timers = kept
}

// upload is one request the fake server received.
type upload struct {
	auth      string
	version   string
	recording map[string]any
}

func (u upload) account() string {
	v1, _ := u.recording["v1"].(map[string]any)
	chars, _ := v1["characters"].(map[string]any)
	for k := range chars {
		return strings.TrimSuffix(k, "-Forever")
	}
	return ""
}

type answer struct {
	status int
	body   string
}

type harness struct {
	t      *testing.T
	game   string
	store  *config.Store
	clock  *fakeClock
	events chan string
	out    *safeBuffer

	mu      sync.Mutex
	uploads []upload
	answer  func(n int, u upload) answer

	done   chan error
	cancel func()
	srv    *httptest.Server
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// newHarness makes a game install with a PriceWatch.lua for each account
// and a fake server that answers each upload with uploadedUpTo 1793805060.
func newHarness(t *testing.T, accounts ...string) *harness {
	t.Helper()
	h := &harness{
		t:      t,
		game:   t.TempDir(),
		store:  config.NewStore(filepath.Join(t.TempDir(), "config.json")),
		clock:  newFakeClock(),
		events: make(chan string, 10),
		out:    &safeBuffer{},
		answer: func(int, upload) answer { return answer{200, `{"uploadedUpTo":1793805060}`} },
	}
	if err := os.MkdirAll(filepath.Join(h.game, "Interface", "AddOns", "PriceWatch"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, a := range accounts {
		h.save(a, 1)
	}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/companion/v1/upload" {
			t.Errorf("request to %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Version   string         `json:"version"`
			Recording map[string]any `json:"recording"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body isn't JSON: %s", raw)
		}
		u := upload{auth: r.Header.Get("Authorization"), version: body.Version, recording: body.Recording}
		h.mu.Lock()
		h.uploads = append(h.uploads, u)
		n := len(h.uploads)
		a := h.answer(n, u)
		h.mu.Unlock()
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body)
	}))
	t.Cleanup(h.srv.Close)
	t.Cleanup(func() {
		if strings.Contains(h.out.String(), token) {
			t.Errorf("the token was printed:\n%s", h.out)
		}
	})
	return h
}

func (h *harness) file(account string) string {
	return filepath.Join(h.game, "WTF", "Account", account, "SavedVariables", "PriceWatch.lua")
}

// save writes the account's PriceWatch.lua as the game would, its recording
// holding one Ledger row whose count is n.
func (h *harness) save(account string, n int) {
	h.t.Helper()
	text := fmt.Sprintf("\nPriceWatchDB = {\n\t[\"recording\"] = {\n\t\t[\"v1\"] = {\n\t\t\t[\"characters\"] = {\n\t\t\t\t[\"%s-Forever\"] = \"seu\\tspvp\",\n\t\t\t},\n\t\t\t[\"ledger\"] = {\n\t\t\t\t[\"%s-Forever\"] = \"spost\\tn1793790661\\tsitem:2589\\tn%d\",\n\t\t\t},\n\t\t},\n\t},\n}\n", account, account, n)
	if err := os.MkdirAll(filepath.Dir(h.file(account)), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.file(account), []byte(text), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// saveEmpty writes the account's PriceWatch.lua with nothing recorded yet.
func (h *harness) saveEmpty(account string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.file(account)), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.file(account), []byte("\nPriceWatchDB = {\n}\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) link() {
	h.t.Helper()
	if err := h.store.Save(config.Config{Server: h.srv.URL, Token: token}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.done = make(chan error, 1)
	h.cancel = cancel
	r := &companion.Runner{
		Game:   h.game,
		Client: api.New(h.srv.URL, "1.0.0"),
		Store:  h.store,
		Clock:  h.clock,
		Events: h.events,
		Out:    h.out,
	}
	go func() { h.done <- r.Run(ctx) }()
	h.t.Cleanup(h.stop)
}

// stop stops the runner and waits until it has.
func (h *harness) stop() {
	h.cancel()
	select {
	case err := <-h.done:
		h.done <- err
	case <-time.After(5 * time.Second):
		h.t.Error("the runner didn't stop")
	}
}

// idle waits until the runner has done what it can and waits, and returns
// how long it waits for.
func (h *harness) idle() time.Duration {
	h.t.Helper()
	select {
	case d := <-h.clock.waits:
		return d
	case err := <-h.done:
		h.done <- err
		h.t.Fatalf("the runner stopped: %v\n%s", err, h.out)
	case <-time.After(5 * time.Second):
		h.t.Fatalf("the runner never waited\n%s", h.out)
	}
	return 0
}

// rewrite saves the account's file again and tells the runner.
func (h *harness) rewrite(account string, n int) {
	h.save(account, n)
	h.events <- h.file(account)
}

func (h *harness) uploadCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.uploads)
}

func (h *harness) lastUpload() upload {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.uploads[len(h.uploads)-1]
}

func (h *harness) dataAddon() string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.game, "Interface", "AddOns", "PriceWatch_Companion", "PriceWatch_Companion.lua"))
	if err != nil {
		h.t.Fatalf("no data addon: %v", err)
	}
	return string(b)
}

func (h *harness) expectUploads(n int) {
	h.t.Helper()
	if got := h.uploadCount(); got != n {
		h.t.Fatalf("%d uploads, want %d\n%s", got, n, h.out)
	}
}

func (h *harness) expectDataAddon(want string) {
	h.t.Helper()
	if got := h.dataAddon(); got != want {
		h.t.Errorf("data addon %q, want %q", got, want)
	}
}

func TestUploadsEveryAccountAtStartAndTellsTheAddon(t *testing.T) {
	h := newHarness(t, "AAA")
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)
	u := h.lastUpload()
	if u.auth != "Bearer "+token || u.version != "1.0.0" || u.account() != "AAA" {
		t.Errorf("upload %+v", u)
	}
	h.expectDataAddon("PriceWatchCompanion = { linked = true, uploadedUpTo = 1793805060 }\n")
	if !strings.Contains(h.out.String(), "restart") {
		t.Errorf("didn't say the game needs a restart:\n%s", h.out)
	}
	c, _ := h.store.Load()
	if !c.LastUpload.Equal(h.clock.Now()) {
		t.Errorf("last upload %v, want %v", c.LastUpload, h.clock.Now())
	}
}

func TestUploadsTheRecordingAsTheAddonSavedIt(t *testing.T) {
	h := newHarness(t)
	var svf struct {
		Cases []struct {
			Name  string `json:"name"`
			Input struct {
				Text string `json:"text"`
			} `json:"input"`
		} `json:"cases"`
	}
	readJSON(t, "../../fixtures/saved-variables.json", &svf)
	var rec struct {
		Cases []struct {
			Expected struct {
				Recording any `json:"recording"`
			} `json:"expected"`
		} `json:"cases"`
	}
	readJSON(t, "../../fixtures/recording-v1.json", &rec)
	for _, c := range svf.Cases {
		if strings.HasPrefix(c.Name, "PriceWatch.lua") {
			if err := os.MkdirAll(filepath.Dir(h.file("AAA")), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(h.file("AAA"), []byte(c.Input.Text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)
	got := any(h.lastUpload().recording)
	if !reflect.DeepEqual(got, rec.Cases[0].Expected.Recording) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(rec.Cases[0].Expected.Recording)
		t.Errorf("uploaded %s\nwant %s", g, w)
	}
}

func TestAFileRewrittenSeveralTimesInARowIsUploadedOnce(t *testing.T) {
	h := newHarness(t, "AAA")
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)

	h.rewrite("AAA", 2)
	h.idle()
	h.clock.Advance(time.Second)
	h.rewrite("AAA", 3)
	h.idle()
	h.clock.Advance(time.Second)
	h.rewrite("AAA", 4)
	settle := h.idle()
	h.expectUploads(1)

	h.clock.Advance(settle - time.Millisecond)
	h.expectUploads(1)
	h.clock.Advance(time.Millisecond)
	h.idle()
	h.expectUploads(2)
	if !strings.Contains(fmt.Sprint(h.lastUpload().recording), "n4") {
		t.Errorf("uploaded %v, want the last save", h.lastUpload().recording)
	}
}

func TestAFailedUploadIsTriedAgain(t *testing.T) {
	h := newHarness(t, "AAA")
	h.answer = func(n int, _ upload) answer {
		if n == 1 {
			return answer{503, `oops`}
		}
		return answer{200, `{"uploadedUpTo":1793805060}`}
	}
	h.link()
	h.start()
	wait := h.idle()
	h.expectUploads(1)
	h.expectDataAddon("PriceWatchCompanion = { linked = true }\n")
	if wait <= 0 || wait > time.Hour {
		t.Fatalf("tries again in %v", wait)
	}
	h.clock.Advance(wait)
	h.idle()
	h.expectUploads(2)
	h.expectDataAddon("PriceWatchCompanion = { linked = true, uploadedUpTo = 1793805060 }\n")
}

func TestAnUnreachableServerIsTriedAgainLaterEachTime(t *testing.T) {
	h := newHarness(t, "AAA")
	h.answer = func(n int, _ upload) answer { return answer{500, `{}`} }
	h.link()
	h.start()
	first := h.idle()
	h.clock.Advance(first)
	second := h.idle()
	h.expectUploads(2)
	if second <= first {
		t.Errorf("waited %v, then %v: not backing off", first, second)
	}
}

func TestA429WithoutATimeStillWaits(t *testing.T) {
	h := newHarness(t, "AAA")
	h.answer = func(n int, _ upload) answer {
		if n == 1 {
			return answer{429, `{"error":"too_many_uploads"}`}
		}
		return answer{200, `{"uploadedUpTo":1793805060}`}
	}
	h.link()
	h.start()
	wait := h.idle()
	h.expectUploads(1)
	if wait <= 0 {
		t.Fatalf("tries again in %v", wait)
	}
	h.clock.Advance(wait)
	h.idle()
	h.expectUploads(2)
}

func TestA429WaitsItsRetryAfter(t *testing.T) {
	h := newHarness(t, "AAA")
	h.answer = func(n int, _ upload) answer {
		if n == 1 {
			return answer{429, `{"error":"too_many_uploads","retryAfter":3600}`}
		}
		return answer{200, `{"uploadedUpTo":1793805060}`}
	}
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)

	// The game saves meanwhile: still nothing before the hour is up.
	h.clock.Advance(10 * time.Minute)
	h.rewrite("AAA", 2)
	h.idle()
	h.clock.Advance(50*time.Minute - time.Second)
	h.expectUploads(1)
	h.clock.Advance(time.Second)
	h.idle()
	h.expectUploads(2)
}

func TestA401StopsUploadingAndTellsTheAddon(t *testing.T) {
	h := newHarness(t, "AAA")
	h.answer = func(int, upload) answer { return answer{401, `{"error":"invalid_token"}`} }
	h.link()
	h.start()
	select {
	case err := <-h.done:
		h.done <- err
		if !errors.Is(err, companion.ErrUnlinked) {
			t.Errorf("stopped with %v, want ErrUnlinked", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still running after a 401")
	}
	h.expectUploads(1)
	h.expectDataAddon("PriceWatchCompanion = { linked = false }\n")
	c, _ := h.store.Load()
	if c.Linked() {
		t.Error("still linked in the config file")
	}
	if !strings.Contains(h.out.String(), "companion link") {
		t.Errorf("didn't say how to link again:\n%s", h.out)
	}
}

// A 401 for the old token after `companion link` has linked the computer
// again leaves the new link and the addon's linked = true alone.
func TestA401AfterLinkingAgainKeepsTheNewLink(t *testing.T) {
	const newToken = "Qm3vT8bX1kRw6NpLc9HsYe2UjGa5FzDo0tVi4rEyKhB"
	h := newHarness(t, "AAA")
	h.answer = func(int, upload) answer {
		if err := h.store.Save(config.Config{Server: h.srv.URL, Token: newToken}); err != nil {
			t.Error(err)
		}
		return answer{401, `{"error":"invalid_token"}`}
	}
	h.link()
	h.start()
	select {
	case err := <-h.done:
		h.done <- err
		if !errors.Is(err, companion.ErrRelinked) {
			t.Errorf("stopped with %v, want ErrRelinked", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still running after a 401")
	}
	h.expectDataAddon("PriceWatchCompanion = { linked = true }\n")
	c, _ := h.store.Load()
	if c.Token != newToken {
		t.Error("the new link was lost")
	}
	if strings.Contains(h.out.String(), "unlinked on the website") {
		t.Errorf("said it was unlinked:\n%s", h.out)
	}
}

func TestAnUnlinkedCompanionUploadsNothingAndWritesNoDataAddon(t *testing.T) {
	h := newHarness(t, "AAA")
	h.start()
	select {
	case err := <-h.done:
		h.done <- err
		if !errors.Is(err, companion.ErrNotLinked) {
			t.Errorf("stopped with %v, want ErrNotLinked", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still running unlinked")
	}
	h.expectUploads(0)
	if _, err := os.Stat(filepath.Join(h.game, "Interface", "AddOns", "PriceWatch_Companion")); !os.IsNotExist(err) {
		t.Errorf("data addon written unlinked: %v", err)
	}
}

func TestTwoAccountsTellTheAddonTheSmallerTime(t *testing.T) {
	h := newHarness(t, "AAA", "BBB")
	h.answer = func(n int, u upload) answer {
		if u.account() == "AAA" {
			return answer{200, `{"uploadedUpTo":1793805060}`}
		}
		if n <= 2 {
			return answer{503, `{}`}
		}
		return answer{200, `{"uploadedUpTo":1793790661}`}
	}
	h.link()
	h.start()
	wait := h.idle()
	h.expectUploads(2)
	// BBB hasn't been answered yet: no time, or BBB's addon would drop
	// what it hasn't uploaded.
	h.expectDataAddon("PriceWatchCompanion = { linked = true }\n")

	h.clock.Advance(wait)
	h.idle()
	h.expectUploads(3)
	h.expectDataAddon("PriceWatchCompanion = { linked = true, uploadedUpTo = 1793790661 }\n")
}

// An account that had nothing recorded holds no other account back, but
// once it records something, it does until that is uploaded.
func TestAnAccountThatHadNothingHoldsTheTimeBackOnceItRecords(t *testing.T) {
	h := newHarness(t, "AAA")
	h.saveEmpty("BBB")
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)
	h.expectDataAddon("PriceWatchCompanion = { linked = true, uploadedUpTo = 1793805060 }\n")
	h.stop()

	// With the Companion stopped, BBB plays; then BBB's upload fails.
	h.save("BBB", 1)
	h.mu.Lock()
	h.answer = func(n int, u upload) answer {
		if u.account() == "BBB" {
			return answer{503, `{}`}
		}
		return answer{200, `{"uploadedUpTo":1793809999}`}
	}
	h.mu.Unlock()
	h.start()
	h.idle()
	h.expectUploads(3)
	h.expectDataAddon("PriceWatchCompanion = { linked = true }\n")
}

// A file that can't be read may hold records that were never uploaded.
func TestAnUnreadableFileHoldsTheTimeBack(t *testing.T) {
	h := newHarness(t, "AAA")
	if err := os.MkdirAll(filepath.Dir(h.file("BBB")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.file("BBB"), []byte("PriceWatchDB = {\n\t[\"recording\"] = \"cut off"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)
	h.expectDataAddon("PriceWatchCompanion = { linked = true }\n")
}

func TestAnAnswerOfNullKeepsTheTimeItHad(t *testing.T) {
	h := newHarness(t, "AAA")
	h.answer = func(n int, _ upload) answer {
		if n == 1 {
			return answer{200, `{"uploadedUpTo":1793805060}`}
		}
		return answer{200, `{"uploadedUpTo":null}`}
	}
	h.link()
	h.start()
	h.idle()
	h.rewrite("AAA", 2)
	settle := h.idle()
	h.clock.Advance(settle)
	h.idle()
	h.expectUploads(2)
	h.expectDataAddon("PriceWatchCompanion = { linked = true, uploadedUpTo = 1793805060 }\n")
}

func TestATimeIsKeptAcrossRestarts(t *testing.T) {
	h := newHarness(t, "AAA")
	h.link()
	h.start()
	h.idle()
	h.expectUploads(1)

	// Started again, with the server answering null this time.
	h.stop()
	h.mu.Lock()
	h.answer = func(int, upload) answer { return answer{200, `{"uploadedUpTo":null}`} }
	h.mu.Unlock()
	h.start()
	h.idle()
	h.expectUploads(2)
	h.expectDataAddon("PriceWatchCompanion = { linked = true, uploadedUpTo = 1793805060 }\n")
}

func TestANewAccountIsUploadedWhenTheGameFirstSavesIt(t *testing.T) {
	h := newHarness(t, "AAA")
	h.link()
	h.start()
	h.idle()
	h.rewrite("BBB", 1)
	settle := h.idle()
	h.clock.Advance(settle)
	h.idle()
	h.expectUploads(2)
	if got := h.lastUpload().account(); got != "BBB" {
		t.Errorf("uploaded %s, want BBB", got)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
