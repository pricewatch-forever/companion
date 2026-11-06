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
	Upload(ctx context.Context, token string, recording any) (api.UploadResult, error)
}

// ErrNotLinked means Run was started before the Companion was linked.
var ErrNotLinked = errors.New("the Companion isn't linked yet: run `companion link` first")

// ErrUnlinked means the server no longer knows this computer: it was
// unlinked on the website.
var ErrUnlinked = errors.New("this computer was unlinked on the website: run `companion link` to link it again")

const (
	// Settle is how long a file must go unchanged before it is uploaded.
	Settle = 2 * time.Second
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

// file is where one PriceWatch.lua stands.
type file struct {
	dirty     bool      // changed since it was last uploaded
	due       time.Time // when it has stopped changing
	notBefore time.Time // when to try again after a failure
	failures  int
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
	token := cfg.Token

	paths, err := wow.Files(r.Game)
	if err != nil {
		return err
	}
	now := r.Clock.Now()
	files := map[string]*file{}
	for _, p := range paths {
		files[p] = &file{dirty: true, due: now}
	}
	if len(files) == 0 {
		r.Say("No PriceWatch data yet: it is uploaded when the game first saves it.")
	}
	if err := r.writeAddon(cfg, files); err != nil {
		return err
	}

	var blockedUntil time.Time // after a 429
	events := r.Events
	for {
		now := r.Clock.Now()
		for _, p := range sortedKeys(files) {
			f := files[p]
			if !f.dirty || now.Before(latest(f.due, f.notBefore, blockedUntil)) {
				continue
			}
			cfg, err = r.upload(ctx, token, p, f, &blockedUntil)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			if err := r.writeAddon(cfg, files); err != nil {
				r.Say("Can't write the data addon: %v", err)
			}
		}

		wait := idleWait
		for _, f := range files {
			if f.dirty {
				if d := latest(f.due, f.notBefore, blockedUntil).Sub(now); d < wait {
					wait = max(d, 0)
				}
			}
		}
		timer := r.Clock.After(wait)
		select {
		case <-ctx.Done():
			return nil
		case p, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			f := files[p]
			if f == nil {
				f = &file{}
				files[p] = f
			}
			f.dirty = true
			f.due = r.Clock.Now().Add(Settle)
		case <-timer:
		}
	}
}

// upload uploads one file and records the answer. It returns the config as
// saved, or an error that stops the runner.
func (r *Runner) upload(ctx context.Context, token config.Token, path string, f *file, blockedUntil *time.Time) (config.Config, error) {
	account := wow.Account(path)
	f.dirty = false
	recording, err := readRecording(path)
	if err != nil {
		r.Say("Can't read %s's PriceWatch data (%v). It is read again the next time the game saves it.", account, err)
		return r.Store.Load()
	}
	if recording == nil {
		r.Say("Nothing recorded yet for %s.", account)
		return r.record(token, path, nil, false)
	}

	res, err := r.Client.Upload(ctx, string(token), recording)
	var rl *api.RateLimitedError
	switch {
	case err == nil:
		f.failures = 0
		f.notBefore = time.Time{}
		r.Say("Uploaded %s's PriceWatch data.", account)
		return r.record(token, path, res.UploadedUpTo, true)
	case ctx.Err() != nil:
		return config.Config{}, nil
	case errors.Is(err, api.ErrUnauthorized):
		return r.unlink(token)
	case errors.As(err, &rl):
		f.dirty = true
		*blockedUntil = r.Clock.Now().Add(rl.RetryAfter)
		r.Say("The server has had enough uploads for now: the next one is in %s.", roundWait(rl.RetryAfter))
	case api.IsTemporary(err):
		f.dirty = true
		wait := firstBackoff << min(f.failures, 16)
		wait = min(wait, maxBackoff)
		f.failures++
		f.notBefore = r.Clock.Now().Add(wait)
		r.Say("Couldn't upload %s's PriceWatch data (%v). Trying again in %s.", account, err, roundWait(wait))
	case errors.Is(err, api.ErrTooLarge):
		r.Say("%s's PriceWatch data is too large to upload. It is tried again the next time the game saves it.", account)
	default:
		r.Say("The server refused %s's PriceWatch data (%v). It is tried again the next time the game saves it.", account, err)
	}
	return r.Store.Load()
}

// record saves the server's answer for one file. A nil time keeps the time
// it had.
func (r *Runner) record(token config.Token, path string, upTo *int64, uploaded bool) (config.Config, error) {
	err := r.Store.Update(token, func(c *config.Config) {
		if c.Accounts == nil {
			c.Accounts = map[string]config.Account{}
		}
		a := c.Accounts[path]
		a.Answered = true
		if upTo != nil {
			a.UploadedUpTo = upTo
		}
		c.Accounts[path] = a
		if uploaded {
			c.LastUpload = r.Clock.Now()
		}
	})
	if errors.Is(err, config.ErrRelinked) {
		return config.Config{}, errors.New("the Companion was linked again while running: start `companion run` again")
	}
	if err != nil {
		return config.Config{}, err
	}
	return r.Store.Load()
}

// unlink marks the Companion unlinked after a 401 and tells the addon.
func (r *Runner) unlink(token config.Token) (config.Config, error) {
	err := r.Store.Update(token, func(c *config.Config) {
		c.Token = ""
		c.Accounts = nil
	})
	if err != nil && !errors.Is(err, config.ErrRelinked) {
		return config.Config{}, err
	}
	if _, err := addon.Write(r.Game, addon.State{Linked: false}); err != nil {
		r.Say("Can't write the data addon: %v", err)
	}
	r.Say("This computer was unlinked on the website, so uploads have stopped. Run `companion link` to link it again.")
	return config.Config{}, ErrUnlinked
}

// writeAddon tells the addon the Companion is linked, and the smallest of
// the accounts' times: no account's addon may drop what isn't uploaded.
func (r *Runner) writeAddon(cfg config.Config, files map[string]*file) error {
	created, err := addon.Write(r.Game, addon.State{Linked: true, UploadedUpTo: smallestTime(cfg, files)})
	if err != nil {
		return err
	}
	if created {
		r.Say("Wrote the PriceWatch_Companion addon into the game. The game only finds a new addon when it starts: if it's running, restart it (a /reload isn't enough).")
	}
	return nil
}

// smallestTime is the smallest uploaded-up-to time of the accounts, or nil
// while one of them hasn't been answered. An account answered with no time
// at all (it had no records) holds nothing back.
func smallestTime(cfg config.Config, files map[string]*file) *int64 {
	var smallest *int64
	for p := range files {
		a, ok := cfg.Accounts[p]
		if !ok || !a.Answered {
			return nil
		}
		if a.UploadedUpTo != nil && (smallest == nil || *a.UploadedUpTo < *smallest) {
			v := *a.UploadedUpTo
			smallest = &v
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

func sortedKeys(m map[string]*file) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
