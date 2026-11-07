// Package clocktest is a clock for tests, moved by hand.
package clocktest

import (
	"sync"
	"time"
)

// Clock is a clock the test moves by hand. Each time the code under test
// waits, it reports how long on Waits, so the test knows it is idle.
type Clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []timer
	Waits  chan time.Duration
}

type timer struct {
	at time.Time
	ch chan time.Time
}

// New returns a clock at 2026-11-06 20:00 UTC.
func New() *Clock {
	return &Clock{now: time.Date(2026, 11, 6, 20, 0, 0, 0, time.UTC), Waits: make(chan time.Duration, 100)}
}

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, timer{at: c.now.Add(d), ch: ch})
	c.mu.Unlock()
	c.Waits <- d
	return ch
}

// Advance moves the clock on by d and fires the waits that are over.
func (c *Clock) Advance(d time.Duration) {
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
