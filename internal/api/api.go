// Package api talks to the PriceWatch Forever server's Companion endpoints,
// under /companion/v1: asking for a link, polling it, and uploading.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client sends the Companion's requests to one server.
type Client struct {
	base    string
	version string
	http    *http.Client
}

// New returns a client for the server at base (its address, such as
// https://example.convex.site), sending version as the Companion's own.
func New(base, version string) *Client {
	return &Client{
		base:    strings.TrimRight(base, "/"),
		version: version,
		http:    &http.Client{Timeout: 2 * time.Minute},
	}
}

// Link is the server's answer to asking for a link.
type Link struct {
	Code      string `json:"code"`      // shown to the player
	Secret    string `json:"secret"`    // polled with
	URL       string `json:"url"`       // the website's link page, with the code in it
	ExpiresIn int    `json:"expiresIn"` // seconds the code lasts
	Interval  int    `json:"interval"`  // seconds between polls
}

// PollStatus is where a link stands.
type PollStatus string

const (
	Pending PollStatus = "pending" // the player hasn't confirmed the code yet
	Linked  PollStatus = "linked"  // confirmed: the answer has the device token
	Expired PollStatus = "expired" // this link is over; ask for a new one
)

// Poll is the server's answer to a poll.
type Poll struct {
	Status PollStatus `json:"status"`
	Token  string     `json:"token"` // the device token, only with Linked
}

// UploadResult is the server's answer to an upload.
type UploadResult struct {
	// UploadedUpTo is the latest record time the upload held, in the game's
	// server time, or nil when the server read none.
	UploadedUpTo *int64 `json:"uploadedUpTo"`
}

// ErrUnauthorized is a 401: the device token is of no linked computer.
var ErrUnauthorized = errors.New("this computer isn't linked")

// ErrTooLarge is a 413: the upload is over the server's limit.
var ErrTooLarge = errors.New("the upload is too large for the server")

// RateLimitedError is a 429: wait RetryAfter before the next upload.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many uploads; next one in %v", e.RetryAfter)
}

// StatusError is any other answer that isn't a success.
type StatusError struct {
	Status int
	Code   string // the answer's `error`, if it had one
}

func (e *StatusError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("server answered %d %s", e.Status, e.Code)
	}
	return fmt.Sprintf("server answered %d", e.Status)
}

// IsTemporary reports whether a request that failed with err is worth
// sending again later: the network failed or the server answered 5xx.
func IsTemporary(err error) bool {
	if err == nil {
		return false
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status >= 500
	}
	var rl *RateLimitedError
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrTooLarge) || errors.As(err, &rl) {
		return false
	}
	var ne *networkError
	return errors.As(err, &ne)
}

type networkError struct{ err error }

func (e *networkError) Error() string { return "can't reach the server: " + e.err.Error() }
func (e *networkError) Unwrap() error { return e.err }

// Link asks for a link, naming the computer name on the website.
func (c *Client) Link(ctx context.Context, name string) (Link, error) {
	var l Link
	err := c.post(ctx, "/companion/v1/link", "", struct {
		Name    string `json:"name,omitempty"`
		Version string `json:"version,omitempty"`
	}{name, c.version}, &l)
	return l, err
}

// Poll asks where the link with this secret stands.
func (c *Client) Poll(ctx context.Context, secret string) (Poll, error) {
	var p Poll
	err := c.post(ctx, "/companion/v1/link/poll", "", struct {
		Secret string `json:"secret,omitempty"`
	}{secret}, &p)
	return p, err
}

// Upload sends recording, PriceWatchDB.recording as the addon saved it, with
// the device token.
func (c *Client) Upload(ctx context.Context, token string, recording any) (UploadResult, error) {
	var u UploadResult
	err := c.post(ctx, "/companion/v1/upload", token, struct {
		Version   string `json:"version,omitempty"`
		Recording any    `json:"recording,omitempty"`
	}{c.version, recording}, &u)
	return u, err
}

func (c *Client) post(ctx context.Context, path, token string, body, answer any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &networkError{err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return &networkError{err}
	}

	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, answer); err != nil {
			return &StatusError{Status: res.StatusCode, Code: "unreadable answer"}
		}
		return nil
	}
	var e struct {
		Error      string `json:"error"`
		RetryAfter *int   `json:"retryAfter"`
	}
	_ = json.Unmarshal(raw, &e)
	switch res.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusRequestEntityTooLarge:
		return ErrTooLarge
	case http.StatusTooManyRequests:
		secs := 0
		if e.RetryAfter != nil {
			secs = *e.RetryAfter
		} else if h, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil {
			secs = h
		}
		return &RateLimitedError{RetryAfter: time.Duration(secs) * time.Second}
	}
	return &StatusError{Status: res.StatusCode, Code: e.Error}
}
