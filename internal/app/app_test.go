package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/app"
	"github.com/pricewatch-forever/companion/internal/clocktest"
	"github.com/pricewatch-forever/companion/internal/config"
	"github.com/pricewatch-forever/companion/internal/menu"
)

const (
	token      = "Zk8wR2yH5nQe1LxVb7TgCu4MjDs0FaPi6oYtW3rXEhA"
	linkAnswer = `{"code":"K7QM-R4TX","secret":"q3VnX0pT8sLhYb2RkWc6ZfJ1uEoAa9MdGiN5eC7xPtw","url":"https://pricewatchforever.com/link?code=K7QM-R4TX","expiresIn":600,"interval":5}`
	linkPage   = "https://pricewatchforever.com/link?code=K7QM-R4TX"
)

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

// loginItem is a login item in memory.
type loginItem struct {
	mu      sync.Mutex
	enabled bool
}

func (l *loginItem) Enabled() bool { l.mu.Lock(); defer l.mu.Unlock(); return l.enabled }
func (l *loginItem) Enable() error { l.mu.Lock(); defer l.mu.Unlock(); l.enabled = true; return nil }
func (l *loginItem) Disable() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = false
	return nil
}

type harness struct {
	t     *testing.T
	game  string
	store *config.Store
	// configFile is the store's file.
	configFile string
	clock      *clocktest.Clock
	tick       chan time.Time
	log        *safeBuffer
	login      *loginItem
	app        *app.App

	mu        sync.Mutex
	poll      string                    // the answer to a poll
	uploadSay func(n int) (int, string) // the answer to the nth upload
	uploads   int
	opened    []string
	running   []int // the game's processes
	srv       *httptest.Server
	cancel    func()
}

// newHarness makes a game install with one account's PriceWatch.lua, a
// fake server that answers polls with pending and uploads with 200, and an
// App on them, not started.
func newHarness(t *testing.T) *harness {
	h := &harness{
		t:          t,
		game:       t.TempDir(),
		configFile: filepath.Join(t.TempDir(), "config.json"),
		clock:      clocktest.New(),
		tick:       make(chan time.Time),
		log:        &safeBuffer{},
		login:      &loginItem{},
		poll:       `{"status":"pending"}`,
		uploadSay: func(int) (int, string) {
			return 200, `{"uploadedUpTo":1793805060}`
		},
	}
	h.store = config.NewStore(h.configFile)
	h.makeGameFolder()
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/companion/v1/link":
			_, _ = io.WriteString(w, linkAnswer)
		case "/companion/v1/link/poll":
			_, _ = io.WriteString(w, h.poll)
		case "/companion/v1/upload":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Errorf("uploaded with %q", r.Header.Get("Authorization"))
			}
			h.uploads++
			status, body := h.uploadSay(h.uploads)
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		default:
			t.Errorf("request to %s", r.URL.Path)
		}
	}))
	t.Cleanup(h.srv.Close)
	h.app = h.newApp()
	t.Cleanup(func() {
		if strings.Contains(h.log.String(), token) {
			t.Errorf("the log holds the token:\n%s", h.log)
		}
	})
	return h
}

// newApp is an App on the harness's game, server, config and clock.
func (h *harness) newApp() *app.App {
	return &app.App{
		Game:      h.game,
		Server:    h.srv.URL,
		Version:   "1.0.0",
		Name:      "Mara's Mac",
		Store:     h.store,
		Clock:     h.clock,
		Tick:      h.tick,
		LoginItem: h.login,
		Log:       h.log,
		LockFile:  filepath.Join(h.t.TempDir(), "companion.lock"),
		Open: func(url string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.opened = append(h.opened, url)
			return nil
		},
		GameProcesses: func(string) ([]int, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return slices.Clone(h.running), nil
		},
	}
}

func (h *harness) makeGameFolder() {
	h.t.Helper()
	save := filepath.Join(h.game, "WTF", "Account", "AAA", "SavedVariables", "PriceWatch.lua")
	text := "\nPriceWatchDB = {\n\t[\"recording\"] = {\n\t\t[\"v1\"] = {\n\t\t\t[\"characters\"] = {\n\t\t\t\t[\"AAA-Forever\"] = \"seu\\tspvp\",\n\t\t\t},\n\t\t},\n\t},\n}\n"
	for _, dir := range []string{filepath.Dir(save), filepath.Join(h.game, "Interface", "AddOns", "PriceWatch")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := os.WriteFile(save, []byte(text), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) linked() {
	h.t.Helper()
	if err := h.store.Save(config.Config{Server: h.srv.URL, Token: token}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) set(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f()
}

func (h *harness) start() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	if err := h.app.Start(ctx); err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		cancel()
		h.app.Wait()
	})
}

// until lets time pass, as the App waits, until the menu is what ok wants,
// and returns it.
func (h *harness) until(what string, ok func(menu.Menu) bool) menu.Menu {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		m := h.app.Menu()
		if ok(m) {
			return m
		}
		select {
		case d := <-h.clock.Waits:
			h.clock.Advance(d)
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			h.t.Fatalf("waited for %s; the menu says %q\n%s", what, m.Lines, h.log)
		}
	}
}

func shows(lines ...string) func(menu.Menu) bool {
	return func(m menu.Menu) bool { return slices.Equal(m.Lines, lines) }
}

func linkedAndUploadedToday(m menu.Menu) bool {
	return len(m.Lines) == 2 && m.Lines[0] == "Linked" && strings.HasPrefix(m.Lines[1], "Last upload: today ")
}

func (h *harness) dataAddon() string {
	b, _ := os.ReadFile(filepath.Join(h.game, "Interface", "AddOns", "PriceWatch_Companion", "PriceWatch_Companion.lua"))
	return string(b)
}

func TestLinkingFromTheMenu(t *testing.T) {
	h := newHarness(t)
	h.start()
	if m := h.app.Menu(); !slices.Equal(m.Lines, []string{"Not linked"}) || !m.Link {
		t.Fatalf("before linking, the menu is %+v", m)
	}

	h.app.Link()
	m := h.until("the code", shows("Not linked", "Code: K7QM-R4TX"))
	if m.Link {
		t.Error("Link this computer… is offered while a code waits")
	}
	h.set(func() {
		if !slices.Equal(h.opened, []string{linkPage}) {
			t.Errorf("opened %v, want the link page", h.opened)
		}
	})

	h.set(func() { h.poll = `{"status":"linked","token":"` + token + `"}` })
	m = h.until("linked and uploaded", shows("Linked", "Last upload: today 20:00"))
	if !m.StartAtLogin || !h.login.Enabled() {
		t.Error("Start when I log in isn't on once linked")
	}
	c, err := h.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Token != token || c.Server != h.srv.URL {
		t.Errorf("config %+v, want the token and the server", c)
	}
	if got := h.dataAddon(); got != "PriceWatchCompanion = { linked = true, uploadedUpTo = 1793805060 }\n" {
		t.Errorf("data addon %q", got)
	}
	if strings.Contains(h.log.String(), token) {
		t.Errorf("the log holds the token:\n%s", h.log)
	}
	if !strings.Contains(h.log.String(), "Linked") {
		t.Errorf("the log doesn't say it linked:\n%s", h.log)
	}
}

func TestTheCodeRunsOut(t *testing.T) {
	h := newHarness(t)
	h.set(func() { h.poll = `{"status":"expired"}` })
	h.start()
	h.app.Link()
	m := h.until("the code to run out", shows("The code ran out"))
	if !m.Link {
		t.Error("Link this computer… isn't offered again")
	}
	if c, _ := h.store.Load(); c.Linked() {
		t.Error("linked with a code that ran out")
	}
}

func TestUnlinkedOnTheWebsite(t *testing.T) {
	h := newHarness(t)
	h.linked()
	h.set(func() { h.uploadSay = func(int) (int, string) { return 401, `{"error":"unauthorized"}` } })
	h.start()
	m := h.until("not linked", shows("Not linked"))
	if !m.Link {
		t.Error("Link this computer… isn't offered")
	}
	if c, _ := h.store.Load(); c.Linked() {
		t.Error("still linked after a 401")
	}
	if got := h.dataAddon(); got != "PriceWatchCompanion = { linked = false }\n" {
		t.Errorf("data addon %q", got)
	}
}

func TestTheServerUnreachableThenBack(t *testing.T) {
	h := newHarness(t)
	h.linked()
	h.set(func() {
		h.uploadSay = func(n int) (int, string) {
			if n == 1 {
				return 503, ""
			}
			return 200, `{"uploadedUpTo":1793805060}`
		}
	})
	h.start()
	h.until("unreachable", shows("Can't reach PriceWatch Forever, trying again", "No upload yet"))
	h.until("uploaded once the server is back", linkedAndUploadedToday)
}

func TestNoGameFolderUntilItIsThere(t *testing.T) {
	h := newHarness(t)
	h.linked()
	if err := os.RemoveAll(h.game); err != nil {
		t.Fatal(err)
	}
	h.start()
	h.until("no game folder", shows("Can't find World of Warcraft: Forever", "No upload yet"))
	if h.dataAddon() != "" {
		t.Error("wrote the data addon with no game folder")
	}
	h.makeGameFolder()
	h.until("uploaded once the folder is there", linkedAndUploadedToday)
}

func TestRestartNeededWhileTheGameThatWasRunningRuns(t *testing.T) {
	h := newHarness(t)
	h.linked()
	h.set(func() { h.running = []int{4242} })
	h.start()
	h.until("restart needed", shows("Linked", "Restart the game to finish setting up", "Last upload: today 20:00"))

	// Still the same game: still needed.
	h.tick <- h.clock.Now()
	h.tick <- h.clock.Now() // the first tick is handled once the second is taken
	if m := h.app.Menu(); len(m.Lines) != 3 {
		t.Errorf("the menu says %q with the same game running", m.Lines)
	}

	// The game was quit and started again.
	h.set(func() { h.running = []int{5151} })
	h.tick <- h.clock.Now()
	h.until("restart done", shows("Linked", "Last upload: today 20:00"))
}

func TestNoRestartNeededWhenTheGameIsntRunning(t *testing.T) {
	h := newHarness(t)
	h.linked()
	h.start()
	h.until("linked", shows("Linked", "Last upload: today 20:00"))
	if _, err := os.Stat(filepath.Join(h.game, "Interface", "AddOns", "PriceWatch_Companion")); err != nil {
		t.Fatalf("no data addon: %v", err)
	}
}

func TestStartWhenILogInIsOnOnceLinkedUntilTurnedOff(t *testing.T) {
	h := newHarness(t)
	h.linked() // in the terminal, before the app ever ran
	h.start()
	h.until("linked", shows("Linked", "Last upload: today 20:00"))
	if !h.login.Enabled() || !h.app.Menu().StartAtLogin {
		t.Error("Start when I log in isn't on once linked")
	}

	h.app.SetStartAtLogin(false)
	if h.login.Enabled() || h.app.Menu().StartAtLogin {
		t.Error("turned off, the login item is still there")
	}
	h.cancel()
	h.app.Wait()
	again := h.newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); again.Wait() }()
	if err := again.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.login.Enabled() {
		t.Error("started again, it turned Start when I log in back on")
	}

	again.SetStartAtLogin(true)
	if !h.login.Enabled() || !again.Menu().StartAtLogin {
		t.Error("turned on, the login item isn't there")
	}
}

func TestLinkingAgainKeepsStartWhenILogInOff(t *testing.T) {
	h := newHarness(t)
	h.set(func() { h.poll = `{"status":"linked","token":"` + token + `"}` })
	h.start()
	h.app.SetStartAtLogin(false) // the player turned it off before linking
	h.app.Link()
	h.until("linked", shows("Linked", "Last upload: today 20:00"))
	if h.login.Enabled() {
		t.Error("linking turned Start when I log in on after the player turned it off")
	}
}

func TestLinkedInTheTerminalWhileTheAppRuns(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.linked()
	h.tick <- h.clock.Now()
	h.until("linked", shows("Linked", "Last upload: today 20:00"))
	if !h.login.Enabled() {
		t.Error("Start when I log in isn't on once linked")
	}
}

func TestTheLogIsDated(t *testing.T) {
	h := newHarness(t)
	h.linked()
	h.start()
	h.until("linked", shows("Linked", "Last upload: today 20:00"))
	first, _, _ := strings.Cut(h.log.String(), "\n")
	want := h.clock.Now().Local().Format("2006-01-02 ")
	if !strings.HasPrefix(first, want) {
		t.Errorf("the log's first line is %q, want it to start with the date", first)
	}
}

func TestASecondCopyQuitsWithoutTouchingAnything(t *testing.T) {
	h := newHarness(t)
	h.linked()
	lock := filepath.Join(t.TempDir(), "companion.lock")
	h.app.LockFile = lock
	h.start()
	h.until("the first copy uploaded", shows("Linked", "Last upload: today 20:00"))
	h.idle()

	addonFile := filepath.Join(h.game, "Interface", "AddOns", "PriceWatch_Companion", "PriceWatch_Companion.lua")
	before := snapshot(t, addonFile, h.configFile)
	logBefore := h.log.String()

	second := h.newApp()
	second.LockFile = lock
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := second.Start(ctx); !errors.Is(err, app.ErrAlreadyRunning) {
		t.Fatalf("the second copy started: %v", err)
	}
	if after := snapshot(t, addonFile, h.configFile); after != before {
		t.Errorf("the second copy touched the files:\nbefore %s\nafter  %s", before, after)
	}
	if h.log.String() != logBefore {
		t.Errorf("the second copy wrote to the log:\n%s", strings.TrimPrefix(h.log.String(), logBefore))
	}

	// Once the first has quit, a copy starts.
	h.cancel()
	h.app.Wait()
	third := h.newApp()
	third.LockFile = lock
	if err := third.Start(ctx); err != nil {
		t.Fatalf("a copy didn't start once the first had quit: %v", err)
	}
	cancel()
	third.Wait()
}

// idle waits until the upload loop has nothing to do and waits an hour.
func (h *harness) idle() {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case d := <-h.clock.Waits:
			if d == time.Hour {
				return
			}
			h.clock.Advance(d)
		case <-deadline:
			h.t.Fatalf("never idle\n%s", h.log)
		}
	}
}

// snapshot is what the files hold and when they were last written.
func snapshot(t *testing.T, paths ...string) string {
	t.Helper()
	var b strings.Builder
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(p)
		b.WriteString(info.ModTime().String() + " " + string(data) + "\n")
	}
	return b.String()
}
