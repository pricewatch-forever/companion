// Package app is the menu-bar app without its UI: it links, keeps the
// upload loop running while linked, and works out what the menu shows. The
// UI draws Menu and calls Link, SetStartAtLogin and OpenWebsite when the
// player chooses them.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/companion"
	"github.com/pricewatch-forever/companion/internal/config"
	"github.com/pricewatch-forever/companion/internal/menu"
	"github.com/pricewatch-forever/companion/internal/single"
	"github.com/pricewatch-forever/companion/internal/wow"
)

// LoginItem starts the Companion when the user logs in.
type LoginItem interface {
	Enabled() bool
	Enable() error
	Disable() error
}

// retryWait is how long the App waits before it looks again for a missing
// game folder, or starts uploading again after the upload loop stopped.
const retryWait = 30 * time.Second

// App is the Companion as the menu-bar app runs it.
type App struct {
	// Game is the game's Forever folder.
	Game string
	// Server is the server to link to. Once linked, the App uploads to the
	// server it was linked to.
	Server string
	// Website is the website's address, for "Open the website".
	Website string
	// Version is the Companion's own, sent to the server.
	Version string
	// Name is this computer's name, shown on the website once linked.
	Name  string
	Store *config.Store
	Clock companion.Clock
	// Tick makes the App look again at what may change without telling it:
	// the time in the menu, the config (linked in the terminal) and whether
	// the game has been restarted.
	Tick <-chan time.Time
	// Open opens a web page in the browser.
	Open      func(url string) error
	LoginItem LoginItem
	// GameProcesses returns the IDs of the game's running processes.
	GameProcesses func(game string) ([]int, error)
	// Log is where the App says what it does. It never holds the token.
	Log io.Writer
	// Changed, if set, is called with the menu each time it may have
	// changed, from any goroutine.
	Changed func(menu.Menu)
	// LockFile is the file the running copy holds a lock on, so that only
	// one runs.
	LockFile string

	ctx     context.Context
	log     io.Writer
	wg      sync.WaitGroup
	drawing sync.Mutex // one call to Changed at a time, the latest menu last
	release func()     // lets go of the lock
	stopped sync.Once

	mu        sync.Mutex
	state     menu.State
	linkPage  string // the page to confirm the waiting code on
	linking   bool   // asking for a code, or waiting for it to be confirmed
	uploading bool   // the upload loop is running
	// restartPIDs are the game's processes that ran when the data addon was
	// first written: once none runs, the game has been restarted.
	restartPIDs []int
}

// ErrAlreadyRunning means another copy of the App is running.
var ErrAlreadyRunning = errors.New("the Companion is already running")

// Start starts the App: it uploads at once if linked. It returns once
// started; the App runs until ctx is done. If another copy is running, it
// returns ErrAlreadyRunning before touching anything.
func (a *App) Start(ctx context.Context) error {
	release, err := single.Lock(a.LockFile)
	if errors.Is(err, single.ErrHeld) {
		return ErrAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("can't tell whether the Companion is already running: %w", err)
	}
	a.ctx = ctx
	a.log = dated{a.Log, a.Clock}
	cfg, err := a.Store.Load()
	if err != nil {
		release()
		return err
	}
	a.release = release
	a.say("PriceWatch Companion %s started.", a.Version)
	if a.LoginItem.Enabled() {
		// Written again, it opens the app where it is now, if it was moved.
		if err := a.LoginItem.Enable(); err != nil {
			a.say("Couldn't update the login item: %v", err)
		}
	}
	a.update(func(s *menu.State) {
		s.Linked = cfg.Linked()
		s.LastUpload = cfg.LastUpload
	})
	if cfg.Linked() {
		a.becameLinked()
	} else {
		a.update(func(s *menu.State) { s.StartAtLogin = a.LoginItem.Enabled() })
	}
	a.wg.Add(1)
	go a.ticks()
	return nil
}

// Wait waits until the App has stopped, once Start's ctx is done, and lets
// another copy start.
func (a *App) Wait() {
	a.wg.Wait()
	a.stopped.Do(func() {
		if a.release != nil {
			a.release()
		}
	})
}

// Menu is what the menu shows now.
func (a *App) Menu() menu.Menu {
	a.mu.Lock()
	s := a.state
	a.mu.Unlock()
	s.Now = a.Clock.Now()
	return menu.For(s)
}

// Link links this computer, as "Link this computer…" does: it asks for a
// code, opens the website's link page and waits for the player to confirm
// the code there.
func (a *App) Link() {
	a.mu.Lock()
	if a.state.Linked || a.linking {
		a.mu.Unlock()
		return
	}
	a.linking = true
	a.mu.Unlock()
	a.update(func(s *menu.State) {
		s.GettingCode, s.Code, s.CodeRanOut, s.LinkFailed = true, "", false, false
	})
	a.wg.Add(1)
	go a.link()
}

func (a *App) link() {
	defer a.wg.Done()
	linked := false
	defer func() {
		a.mu.Lock()
		a.linking = false
		a.linkPage = ""
		a.mu.Unlock()
		if linked {
			a.becameLinked()
		}
	}()
	l := &companion.Linker{
		Client: api.New(a.Server, a.Version),
		Server: a.Server,
		Store:  a.Store,
		Clock:  a.Clock,
	}
	a.say("Asking for a link code.")
	link, err := l.Request(a.ctx, a.Name)
	if a.ctx.Err() != nil {
		return
	}
	if err != nil {
		a.say("%v", err)
		a.update(func(s *menu.State) { s.GettingCode, s.LinkFailed = false, true })
		return
	}
	a.mu.Lock()
	a.linkPage = link.URL
	a.mu.Unlock()
	a.update(func(s *menu.State) { s.GettingCode, s.Code = false, link.Code })
	if err := a.Open(link.URL); err != nil {
		a.say("Couldn't open the link page in the browser: %v", err)
	}

	err = l.Wait(a.ctx, link)
	switch {
	case a.ctx.Err() != nil:
		return
	case errors.Is(err, companion.ErrLinkExpired):
		a.say("The link code ran out before it was confirmed.")
		a.update(func(s *menu.State) { s.Code, s.CodeRanOut = "", true })
		return
	case err != nil:
		a.say("%v", err)
		a.update(func(s *menu.State) { s.Code, s.LinkFailed = "", true })
		return
	}
	a.say("Linked.")
	linked = true
	a.update(func(s *menu.State) { *s = menu.State{Linked: true} })
}

// becameLinked starts uploading, and turns "Start when I log in" on unless
// the player has turned it off.
func (a *App) becameLinked() {
	if cfg, err := a.Store.Load(); err == nil && cfg.StartAtLogin == nil {
		a.SetStartAtLogin(true)
	} else {
		a.update(func(s *menu.State) { s.StartAtLogin = a.LoginItem.Enabled() })
	}
	a.startUploading()
}

// SetStartAtLogin adds the Companion to the login items, or removes it, as
// "Start when I log in" does, and keeps the player's choice.
func (a *App) SetStartAtLogin(on bool) {
	change := a.LoginItem.Disable
	if on {
		change = a.LoginItem.Enable
	}
	if err := change(); err != nil {
		a.say("Couldn't change the login items: %v", err)
	}
	if err := a.Store.Change(func(c *config.Config) { c.StartAtLogin = &on }); err != nil {
		a.say("Couldn't save Start when I log in: %v", err)
	}
	a.update(func(s *menu.State) { s.StartAtLogin = a.LoginItem.Enabled() })
}

// OpenWebsite opens the website, as "Open the website" does: the link page
// while a code waits there.
func (a *App) OpenWebsite() {
	a.mu.Lock()
	page := a.linkPage
	a.mu.Unlock()
	if page == "" {
		page = a.Website
	}
	if err := a.Open(page); err != nil {
		a.say("Couldn't open the website: %v", err)
	}
}

func (a *App) ticks() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.Tick:
			a.refresh()
		}
	}
}

// refresh looks again at what may have changed without the App being told.
func (a *App) refresh() {
	cfg, err := a.Store.Load()
	if err != nil {
		a.say("%v", err)
	}
	a.mu.Lock()
	linkedElsewhere := err == nil && cfg.Linked() && !a.state.Linked && !a.linking
	restart := slices.Clone(a.restartPIDs)
	a.mu.Unlock()

	restarted := false
	if len(restart) > 0 {
		running, ok := a.gameRunning()
		restarted = ok && !slices.ContainsFunc(running, func(pid int) bool { return slices.Contains(restart, pid) })
	}

	a.update(func(s *menu.State) {
		// Unless the addon was written anew meanwhile, by another game.
		if restarted && slices.Equal(a.restartPIDs, restart) {
			a.restartPIDs = nil
			s.RestartNeeded = false
		}
		s.StartAtLogin = a.LoginItem.Enabled()
		if linkedElsewhere {
			*s = menu.State{Linked: true, StartAtLogin: s.StartAtLogin}
		}
		if s.Linked && err == nil {
			s.LastUpload = cfg.LastUpload
		}
	})
	if linkedElsewhere {
		a.say("Linked in the terminal.")
		a.becameLinked()
	}
}

func (a *App) startUploading() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.uploading {
		return
	}
	a.uploading = true
	a.wg.Add(1)
	go a.upload()
}

// upload keeps the upload loop running while linked.
func (a *App) upload() {
	defer a.wg.Done()
	defer func() {
		a.mu.Lock()
		a.uploading = false
		a.mu.Unlock()
	}()
	for a.ctx.Err() == nil {
		err := a.runOnce()
		switch {
		case a.ctx.Err() != nil:
			return
		case errors.Is(err, companion.ErrUnlinked), errors.Is(err, companion.ErrNotLinked):
			a.update(func(s *menu.State) {
				a.restartPIDs = nil
				*s = menu.State{StartAtLogin: s.StartAtLogin}
			})
			return
		case errors.Is(err, companion.ErrRelinked):
			continue
		case errors.Is(err, errNoGame):
		case err != nil:
			a.say("Stopped uploading: %v. Starting again in %v.", err, retryWait)
		}
		select {
		case <-a.ctx.Done():
		case <-a.Clock.After(retryWait):
		}
	}
}

var errNoGame = errors.New("no game folder")

// runOnce runs the upload loop until it stops.
func (a *App) runOnce() error {
	if !gameThere(a.Game) {
		a.mu.Lock()
		said := a.state.GameMissing
		a.mu.Unlock()
		if !said {
			a.say("Can't find the game at %s.", a.Game)
		}
		a.update(func(s *menu.State) { s.GameMissing = true })
		return errNoGame
	}
	a.update(func(s *menu.State) { s.GameMissing = false })

	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	if !cfg.Linked() {
		return companion.ErrNotLinked
	}
	server := cfg.Server
	if server == "" {
		server = a.Server
	}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	r := &companion.Runner{
		Game:   a.Game,
		Client: api.New(server, a.Version),
		Store:  a.Store,
		Clock:  a.Clock,
		Out:    a.log,
		Notify: a.runnerEvent,
	}
	events, err := wow.Watch(ctx, a.Game, func(err error) { a.say("Watching the game's files: %v", err) })
	if err != nil {
		return fmt.Errorf("can't watch the game's files: %w", err)
	}
	r.Events = events
	a.say("Uploading PriceWatch data from %s.", a.Game)
	return r.Run(ctx)
}

// runnerEvent hears what the upload loop did.
func (a *App) runnerEvent(e companion.Event) {
	switch e {
	case companion.Uploaded:
		now := a.Clock.Now()
		a.update(func(s *menu.State) { s.Unreachable, s.LastUpload = false, now })
	case companion.UploadFailed:
		a.update(func(s *menu.State) { s.Unreachable = true })
	case companion.AddonCreated:
		// A game that is running finds the addon only once restarted; one
		// that isn't finds it when it starts.
		running, ok := a.gameRunning()
		if !ok || len(running) == 0 {
			return
		}
		a.update(func(s *menu.State) {
			a.restartPIDs = running
			s.RestartNeeded = true
		})
	}
}

// gameRunning returns the game's running processes, and whether it could
// tell.
func (a *App) gameRunning() ([]int, bool) {
	running, err := a.GameProcesses(a.Game)
	if err != nil {
		a.say("Can't tell whether the game is running: %v", err)
		return nil, false
	}
	return running, true
}

// gameThere reports whether game looks like the game's Forever folder,
// played in at least once.
func gameThere(game string) bool {
	for _, dir := range []string{filepath.Join(game, "WTF", "Account"), filepath.Join(game, "Interface", "AddOns")} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// update changes the state, with the App's lock held, and tells the UI.
func (a *App) update(change func(*menu.State)) {
	a.mu.Lock()
	change(&a.state)
	a.mu.Unlock()
	if a.Changed != nil {
		// The menu is read once the draw before has finished, so the last
		// draw is never of an older state.
		a.drawing.Lock()
		defer a.drawing.Unlock()
		a.Changed(a.Menu())
	}
}

// say writes a line to the log, timed as the upload loop's are.
func (a *App) say(format string, args ...any) {
	companion.Say(a.log, a.Clock, format, args...)
}

// dated starts each line written to the log with the date.
type dated struct {
	w     io.Writer
	clock companion.Clock
}

func (d dated) Write(p []byte) (int, error) {
	line := append([]byte(d.clock.Now().Local().Format("2006-01-02 ")), p...)
	if _, err := d.w.Write(line); err != nil {
		return 0, err
	}
	return len(p), nil
}
