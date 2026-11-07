// Package companion is what the Companion does once linked: it uploads every
// account's PriceWatch.lua at start and each time the game saves it, and
// tells the PriceWatch addon, through the data addon, what it has uploaded.
package companion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/pricewatch-forever/companion/internal/addon"
	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/config"
	"github.com/pricewatch-forever/companion/internal/savedvars"
	"github.com/pricewatch-forever/companion/internal/wow"
)

// Clock is the time, and waiting for it.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the computer's clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time                         { return time.Now() }
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Uploader sends a recording to the server.
type Uploader interface {
	Upload(ctx context.Context, token api.Token, recording any) (api.UploadResult, error)
}

// ErrNotLinked means Run was started before the Companion was linked.
var ErrNotLinked = errors.New("the Companion isn't linked yet: run `companion link` first")

// ErrUnlinked means the server no longer knows this computer: it was
// unlinked on the website.
var ErrUnlinked = errors.New("this computer was unlinked on the website: run `companion link` to link it again")

// ErrRelinked means `companion link` linked the Companion again while it
// was running, so the link it uploads with is stale.
var ErrRelinked = errors.New("the Companion was linked again while running: start `companion run` again")

const (
	// settle is how long a file must go unchanged before it is uploaded.
	settle = 2 * time.Second
	// The first wait after a failed upload; each failure after it doubles
	// the wait, up to maxBackoff.
	firstBackoff = 30 * time.Second
	maxBackoff   = 30 * time.Minute
	// idleWait is how long the runner waits with nothing to do before it
	// looks again.
	idleWait = time.Hour
)

// Runner uploads the PriceWatch.lua files of the install at Game.
type Runner struct {
	Game   string
	Client Uploader
	Store  *config.Store
	Clock  Clock
	// Events gives the path of a PriceWatch.lua each time the game writes it.
	Events <-chan string
	// Out is where the Runner says what it does.
	Out io.Writer
}

// fileState is where one PriceWatch.lua stands.
type fileState struct {
	toUpload  bool      // changed since it was last read for an upload
	answered  bool      // what it holds now has been answered by the server
	due       time.Time // when it has stopped changing
	notBefore time.Time // when to try again after a failure
	failures  int
}

// saved notes that the game has written the file.
func (f *fileState) saved(now time.Time) {
	f.toUpload = true
	f.answered = false
	f.due = now.Add(settle)
}

// readyAt is when the file can be uploaded.
func (f *fileState) readyAt(blockedUntil time.Time) time.Time {
	return latest(f.due, f.notBefore, blockedUntil)
}

// session is one Run: the link it uploads with and where each file stands.
type session struct {
	*Runner
	token        api.Token
	cfg          config.Config // as last saved
	files        map[string]*fileState
	blockedUntil time.Time // no upload before it, after a 429
}

// Run uploads until ctx is done. It returns ErrNotLinked at once if the
// Companion isn't linked, and ErrUnlinked when the server answers that it
// no longer is.
func (r *Runner) Run(ctx context.Context) error {
	cfg, err := r.Store.Load()
	if err != nil {
		return err
	}
	if !cfg.Linked() {
		return ErrNotLinked
	}
	paths, err := wow.Files(r.Game)
	if err != nil {
		return err
	}
	s := &session{Runner: r, token: cfg.Token, cfg: cfg, files: map[string]*fileState{}}
	now := r.Clock.Now()
	for _, p := range paths {
		s.files[p] = &fileState{toUpload: true, due: now}
	}
	if len(paths) == 0 {
		r.Say("No PriceWatch data yet: it is uploaded when the game first saves it.")
	}
	if err := s.writeAddon(); err != nil {
		return err
	}

	events := r.Events
	for {
		err := s.uploadReady(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		timer := r.Clock.After(s.nextWait(r.Clock.Now()))
		select {
		case <-ctx.Done():
			return nil
		case p, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			f := s.files[p]
			if f == nil {
				f = &fileState{}
				s.files[p] = f
			}
			f.saved(r.Clock.Now())
		case <-timer:
		}
	}
}

// uploadReady uploads every file that is ready, and tells the addon after
// each. It returns an error that stops the runner.
func (s *session) uploadReady(ctx context.Context) error {
	now := s.Clock.Now()
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		f := s.files[p]
		if !f.toUpload || now.Before(f.readyAt(s.blockedUntil)) {
			continue
		}
		if err := s.upload(ctx, p, f); err != nil || ctx.Err() != nil {
			return err
		}
		if err := s.writeAddon(); err != nil {
			s.Say("Can't write the data addon: %v", err)
		}
	}
	return nil
}

// nextWait is how long until a file is ready to upload.
func (s *session) nextWait(now time.Time) time.Duration {
	wait := idleWait
	for _, f := range s.files {
		if f.toUpload {
			wait = min(wait, max(f.readyAt(s.blockedUntil).Sub(now), 0))
		}
	}
	return wait
}

// upload uploads one file and saves the answer. It returns an error that
// stops the runner.
func (s *session) upload(ctx context.Context, path string, f *fileState) error {
	account := wow.Account(path)
	f.toUpload = false
	recording, err := readRecording(path)
	if err != nil {
		s.Say("Can't read %s's PriceWatch data (%v). It is read again the next time the game saves it.", account, err)
		return nil
	}
	if recording == nil {
		s.Say("Nothing recorded yet for %s.", account)
		f.answered = true
		return nil
	}

	res, err := s.Client.Upload(ctx, s.token, recording)
	var limited *api.RateLimitedError
	switch {
	case err == nil:
		f.answered = true
		f.failures = 0
		f.notBefore = time.Time{}
		s.Say("Uploaded %s's PriceWatch data.", account)
		return s.saveAnswer(path, res.UploadedUpTo)
	case ctx.Err() != nil:
		return nil
	case errors.Is(err, api.ErrUnauthorized):
		return s.unlink()
	case errors.As(err, &limited) && limited.RetryAfter > 0:
		f.toUpload = true
		s.blockedUntil = s.Clock.Now().Add(limited.RetryAfter)
		s.Say("The server has had enough uploads for now: the next one is in %s.", roundWait(limited.RetryAfter))
	case limited != nil || api.IsTemporary(err):
		f.toUpload = true
		wait := min(firstBackoff<<min(f.failures, 16), maxBackoff)
		f.failures++
		f.notBefore = s.Clock.Now().Add(wait)
		s.Say("Couldn't upload %s's PriceWatch data (%v). Trying again in %s.", account, err, roundWait(wait))
	case errors.Is(err, api.ErrTooLarge):
		s.Say("%s's PriceWatch data is too large to upload. It is tried again the next time the game saves it.", account)
	default:
		s.Say("The server refused %s's PriceWatch data (%v). It is tried again the next time the game saves it.", account, err)
	}
	return nil
}

// saveAnswer saves the server's answer to an upload of one file. A nil
// time keeps the time the file had.
func (s *session) saveAnswer(path string, upTo *int64) error {
	cfg, err := s.Store.Update(s.token, func(c *config.Config) {
		if upTo != nil {
			if c.UploadedUpTo == nil {
				c.UploadedUpTo = map[string]int64{}
			}
			c.UploadedUpTo[path] = *upTo
		}
		c.LastUpload = s.Clock.Now()
	})
	if errors.Is(err, config.ErrRelinked) {
		return ErrRelinked
	}
	if err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

// unlink marks the Companion unlinked after a 401, tells the addon, and
// returns ErrUnlinked. If it was linked again meanwhile, the 401 was for
// the old link: it leaves the new one and the addon alone.
func (s *session) unlink() error {
	_, err := s.Store.Update(s.token, func(c *config.Config) {
		c.Token = ""
		c.UploadedUpTo = nil
	})
	if errors.Is(err, config.ErrRelinked) {
		return ErrRelinked
	}
	if err != nil {
		return err
	}
	if _, err := addon.Write(s.Game, addon.State{Linked: false}); err != nil {
		s.Say("Can't write the data addon: %v", err)
	}
	s.Say("This computer was unlinked on the website, so uploads have stopped. Run `companion link` to link it again.")
	return ErrUnlinked
}

// writeAddon tells the addon the Companion is linked, and up to when.
func (s *session) writeAddon() error {
	created, err := addon.Write(s.Game, addon.State{Linked: true, UploadedUpTo: s.smallestTime()})
	if err != nil {
		return err
	}
	if created {
		s.Say("Wrote the PriceWatch_Companion addon into the game. The game only finds a new addon when it starts: if it's running, restart it (a /reload isn't enough).")
	}
	return nil
}

// smallestTime is the time the addon may drop records up to: the smallest
// of the accounts' times, so that no account's addon drops what isn't
// uploaded. A file the server has a time for holds back nothing after it,
// since what the game writes later is later. A file without a time holds
// back everything (nil) until what it holds now has been answered.
func (s *session) smallestTime() *int64 {
	var smallest *int64
	for p, f := range s.files {
		t, ok := s.cfg.UploadedUpTo[p]
		if !ok {
			if !f.answered {
				return nil
			}
			continue
		}
		if smallest == nil || t < *smallest {
			smallest = &t
		}
	}
	return smallest
}

// readRecording reads PriceWatchDB.recording from a PriceWatch.lua, or nil
// if it has none.
func readRecording(path string) (any, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vars, err := savedvars.Parse(text)
	if err != nil {
		return nil, err
	}
	db, _ := vars["PriceWatchDB"].(map[string]any)
	return db["recording"], nil
}

// Say tells the player something, with the time.
func (r *Runner) Say(format string, args ...any) {
	fmt.Fprintf(r.Out, "%s  %s\n", r.Clock.Now().Local().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// roundWait says a wait in words: "30 seconds", "2 minutes", "1 hour".
func roundWait(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d < time.Minute:
		return plural(int64(d.Round(time.Second)/time.Second), "second")
	case d < time.Hour:
		return plural(int64(d.Round(time.Minute)/time.Minute), "minute")
	default:
		return plural(int64(d.Round(time.Minute)/time.Hour), "hour")
	}
}

func latest(ts ...time.Time) time.Time {
	var l time.Time
	for _, t := range ts {
		if t.After(l) {
			l = t
		}
	}
	return l
}
