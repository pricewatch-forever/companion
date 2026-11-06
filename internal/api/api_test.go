package api_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pricewatch-forever/companion/internal/api"
)

type apiCase struct {
	Name  string `json:"name"`
	Input struct {
		Given   string `json:"given"`
		Request struct {
			Method  string            `json:"method"`
			Path    string            `json:"path"`
			Headers map[string]string `json:"headers"`
			Body    json.RawMessage   `json:"body"`
		} `json:"request"`
	} `json:"input"`
	Expected struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"expected"`
}

type apiFixture struct {
	Format struct {
		Varies struct {
			Code   string `json:"code"`
			Secret string `json:"secret"`
			Token  string `json:"token"`
		} `json:"varies"`
	} `json:"format"`
	Cases []json.RawMessage `json:"cases"`
}

// The example values the fixture's cases show for what the server makes up.
const (
	exampleCode   = "K7QM-R4TX"
	exampleSecret = "q3VnX0pT8sLhYb2RkWc6ZfJ1uEoAa9MdGiN5eC7xPtw"
	exampleToken  = "Zk8wR2yH5nQe1LxVb7TgCu4MjDs0FaPi6oYtW3rXEhA"
)

func loadFixture(t *testing.T) apiFixture {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/companion-api.json")
	if err != nil {
		t.Fatal(err)
	}
	var f apiFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// randomFrom makes a value the way the server would: n characters of the
// alphabet, so that the Companion can't be relying on the fixture's examples.
func randomFrom(alphabet string, n int) string {
	b := make([]byte, n)
	r := make([]byte, n)
	_, _ = rand.Read(r)
	for i := range b {
		b[i] = alphabet[int(r[i])%len(alphabet)]
	}
	return string(b)
}

const codeAlphabet = "ACDEFGHJKMNPQRTUVWXY23456789"
const secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"

// TestFixtureCases runs every link, link/poll and upload case against a fake
// server that checks the request is the one the case writes and answers what
// the case expects, with fresh server-made values in place of the examples.
func TestFixtureCases(t *testing.T) {
	f := loadFixture(t)
	codeRe := regexp.MustCompile(f.Format.Varies.Code)
	secretRe := regexp.MustCompile(f.Format.Varies.Secret)
	tokenRe := regexp.MustCompile(f.Format.Varies.Token)
	if len(f.Cases) == 0 {
		t.Fatal("no cases in the fixture")
	}

	for _, rawCase := range f.Cases {
		var named struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(rawCase, &named)
		t.Run(named.Name, func(t *testing.T) {
			code := randomFrom(codeAlphabet, 4) + "-" + randomFrom(codeAlphabet, 4)
			secret := randomFrom(secretAlphabet, 43)
			token := randomFrom(secretAlphabet, 43)
			text := strings.NewReplacer(exampleCode, code, exampleSecret, secret, exampleToken, token).Replace(string(rawCase))
			var c apiCase
			if err := json.Unmarshal([]byte(text), &c); err != nil {
				t.Fatal(err)
			}
			req := c.Input.Request

			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != req.Method || r.URL.Path != req.Path {
					t.Errorf("request %s %s, want %s %s", r.Method, r.URL.Path, req.Method, req.Path)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type %q, want application/json", got)
				}
				wantAuth := req.Headers["Authorization"]
				if got := r.Header.Get("Authorization"); got != wantAuth {
					t.Errorf("Authorization %q, want %q", got, wantAuth)
				}
				body, _ := io.ReadAll(r.Body)
				if !sameJSON(t, body, req.Body) {
					t.Errorf("body %s\nwant %s", body, req.Body)
				}
				w.Header().Set("Content-Type", "application/json")
				if c.Expected.Status == http.StatusTooManyRequests {
					var b struct {
						RetryAfter int `json:"retryAfter"`
					}
					_ = json.Unmarshal(c.Expected.Body, &b)
					w.Header().Set("Retry-After", strconv.Itoa(b.RetryAfter))
				}
				w.WriteHeader(c.Expected.Status)
				_, _ = w.Write(c.Expected.Body)
			}))
			defer srv.Close()

			var body map[string]json.RawMessage
			if err := json.Unmarshal(req.Body, &body); err != nil {
				t.Fatal(err)
			}
			version := stringField(t, body, "version")
			client := api.New(srv.URL, version)
			ctx := context.Background()

			var got any
			var err error
			switch req.Path {
			case "/companion/v1/link":
				var l api.Link
				l, err = client.Link(ctx, stringField(t, body, "name"))
				if err == nil {
					if !codeRe.MatchString(l.Code) || !secretRe.MatchString(l.Secret) {
						t.Errorf("code %q or secret %q doesn't match the server's patterns", l.Code, l.Secret)
					}
					got = map[string]any{"code": l.Code, "secret": l.Secret, "url": l.URL, "expiresIn": l.ExpiresIn, "interval": l.Interval}
				}
			case "/companion/v1/link/poll":
				var p api.Poll
				p, err = client.Poll(ctx, stringField(t, body, "secret"))
				if err == nil {
					m := map[string]any{"status": string(p.Status)}
					if p.Token != "" {
						if !tokenRe.MatchString(p.Token) {
							t.Errorf("token %q doesn't match the server's pattern", p.Token)
						}
						m["token"] = p.Token
					}
					got = m
				}
			case "/companion/v1/upload":
				var recording any
				if r, ok := body["recording"]; ok {
					if err := json.Unmarshal(r, &recording); err != nil {
						t.Fatal(err)
					}
				}
				var u api.UploadResult
				u, err = client.Upload(ctx, strings.TrimPrefix(req.Headers["Authorization"], "Bearer "), recording)
				if err == nil {
					var upTo any
					if u.UploadedUpTo != nil {
						upTo = *u.UploadedUpTo
					}
					got = map[string]any{"uploadedUpTo": upTo}
				}
			default:
				t.Fatalf("no client call for %s", req.Path)
			}
			if requests != 1 {
				t.Errorf("%d requests sent, want 1", requests)
			}

			if c.Expected.Status == http.StatusOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				gotJSON, _ := json.Marshal(got)
				if !sameJSON(t, gotJSON, c.Expected.Body) {
					t.Errorf("read the answer as %s\nwant %s", gotJSON, c.Expected.Body)
				}
				return
			}
			checkErrorAnswer(t, err, c.Expected.Status, c.Expected.Body)
		})
	}
}

func checkErrorAnswer(t *testing.T, err error, status int, body json.RawMessage) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error for a %d answer", status)
	}
	var b struct {
		Error      string `json:"error"`
		RetryAfter int    `json:"retryAfter"`
	}
	if e := json.Unmarshal(body, &b); e != nil {
		t.Fatal(e)
	}
	switch status {
	case http.StatusBadRequest:
		var se *api.StatusError
		if !errors.As(err, &se) || se.Status != status || se.Code != b.Error {
			t.Errorf("error %v, want a %d %q", err, status, b.Error)
		}
	case http.StatusUnauthorized:
		if !errors.Is(err, api.ErrUnauthorized) {
			t.Errorf("error %v, want ErrUnauthorized", err)
		}
	case http.StatusTooManyRequests:
		var rl *api.RateLimitedError
		if !errors.As(err, &rl) {
			t.Fatalf("error %v, want a RateLimitedError", err)
		}
		if want := time.Duration(b.RetryAfter) * time.Second; rl.RetryAfter != want {
			t.Errorf("retry after %v, want %v", rl.RetryAfter, want)
		}
	default:
		t.Fatalf("no check for status %d", status)
	}
}

func TestAServerErrorIsTemporary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv.Close()
	_, err := api.New(srv.URL, "1.0.0").Upload(context.Background(), "t", map[string]any{})
	if !api.IsTemporary(err) {
		t.Errorf("error %v is not temporary", err)
	}
}

func TestANetworkFailureIsTemporary(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := api.New(url, "1.0.0").Upload(context.Background(), "t", map[string]any{})
	if err == nil || !api.IsTemporary(err) {
		t.Errorf("error %v is not temporary", err)
	}
}

func TestARefusalIsNotTemporary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
	}))
	defer srv.Close()
	_, err := api.New(srv.URL, "1.0.0").Upload(context.Background(), "t", map[string]any{})
	if err == nil || api.IsTemporary(err) {
		t.Errorf("error %v is temporary", err)
	}
}

func TestA429WithOnlyTheHeaderWaitsItsTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := api.New(srv.URL, "1.0.0").Upload(context.Background(), "t", map[string]any{})
	var rl *api.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != 2*time.Minute {
		t.Errorf("error %v, want a wait of 2m", err)
	}
}

func TestAServerAddressWithATrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/companion/v1/link/poll" {
			t.Errorf("path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"pending","unknown":1}`))
	}))
	defer srv.Close()
	p, err := api.New(srv.URL+"/", "1.0.0").Poll(context.Background(), "s")
	if err != nil || p.Status != api.Pending {
		t.Errorf("poll %v, %v", p, err)
	}
}

func stringField(t *testing.T, body map[string]json.RawMessage, name string) string {
	t.Helper()
	raw, ok := body[name]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Errorf("not JSON: %s", a)
		return false
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}
