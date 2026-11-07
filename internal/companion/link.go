package companion

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/pricewatch-forever/companion/internal/api"
	"github.com/pricewatch-forever/companion/internal/config"
)

// LinkClient asks the server for a link and polls it.
type LinkClient interface {
	Link(ctx context.Context, name string) (api.Link, error)
	Poll(ctx context.Context, secret string) (api.Poll, error)
}

// ErrLinkExpired means the link is over before it was confirmed.
var ErrLinkExpired = errors.New("the link code ran out before it was confirmed: run `companion link` again for a new one")

// Linker links the Companion to an account on Server.
type Linker struct {
	Client LinkClient
	Server string // kept in the config with the token
	Store  *config.Store
	Clock  Clock
	// Open opens the website's link page in the browser, for Link.
	Open func(url string) error
	// Out is where Link says what it does.
	Out io.Writer
}

// Link asks for a link code naming this computer name, shows it, opens the
// link page and polls until the player has confirmed it there.
func (l *Linker) Link(ctx context.Context, name string) error {
	link, err := l.Request(ctx, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(l.Out, "Your link code is %s\n\n", link.Code)
	if err := l.Open(link.URL); err != nil {
		fmt.Fprintf(l.Out, "Open this page, sign in and confirm the code:\n  %s\n\n", link.URL)
	} else {
		fmt.Fprintf(l.Out, "The link page is open in your browser: sign in and confirm the code there.\nIf it didn't open: %s\n\n", link.URL)
	}
	fmt.Fprintln(l.Out, "Waiting for you to confirm it…")
	if err := l.Wait(ctx, link); err != nil {
		return err
	}
	fmt.Fprintln(l.Out, "Linked. Run `companion run` to start uploading.")
	return nil
}

// Request asks the server for a link code naming this computer name.
func (l *Linker) Request(ctx context.Context, name string) (api.Link, error) {
	link, err := l.Client.Link(ctx, name)
	if err != nil {
		return link, fmt.Errorf("couldn't ask for a link code: %w", err)
	}
	return link, nil
}

// Wait polls the link until the player has confirmed its code, and saves
// the link. It returns ErrLinkExpired if the code runs out first.
func (l *Linker) Wait(ctx context.Context, link api.Link) error {
	interval := time.Duration(link.Interval) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.Clock.After(interval):
		}
		p, err := l.Client.Poll(ctx, link.Secret)
		var limited *api.RateLimitedError
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.As(err, &limited):
			interval = max(interval, limited.RetryAfter)
			continue
		case api.IsTemporary(err):
			continue
		default:
			return fmt.Errorf("couldn't check the link: %w", err)
		}
		switch p.Status {
		case api.Linked:
			if err := l.Store.Link(l.Server, p.Token); err != nil {
				return fmt.Errorf("linked, but couldn't save the link: %w", err)
			}
			return nil
		case api.Expired:
			return ErrLinkExpired
		}
	}
}
