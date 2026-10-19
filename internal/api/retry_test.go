package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testClient points a client at a local server and shrinks the backoff, so
// the retry tests do not sleep for real.
func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("test")
	c.Base, c.Gap = srv.URL, 0
	c.retryBase = time.Millisecond
	return c
}

func get(c *Client) (*response, error) {
	return c.get(context.Background(), request{path: "/x", endpoint: "GET /x"})
}

// A 5xx is worth another try: two failures, then the page.
func TestRetriesServerErrorThenSucceeds(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ok"))
	})
	resp, err := get(c)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 || string(resp.body) != "ok" {
		t.Fatalf("%d attempts, body %q", hits, resp.body)
	}
}

// Two retries is the whole budget, then the error surfaces.
func TestRetriesExhausted(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := get(c)
	e, ok := err.(*Error)
	if !ok || e.Kind != KindNetwork || !e.Retryable || e.Status != http.StatusBadGateway {
		t.Fatalf("%v", err)
	}
	if hits != retryAttempts {
		t.Fatalf("%d attempts, want %d", hits, retryAttempts)
	}
}

// A transport error is retried too.
func TestRetriesTransportError(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 2 {
			hj := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := New("test")
	c.Base, c.Gap, c.retryBase = srv.URL, 0, time.Millisecond
	resp, err := get(c)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 || string(resp.body) != "ok" {
		t.Fatalf("%d attempts, body %q", hits, resp.body)
	}
}

// A short Retry-After is honoured: wait, then one retry.
func TestRetryAfterSeconds(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	})
	if _, err := get(c); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("%d attempts, want 2", hits)
	}
}

// Retry-After also comes as an HTTP-date.
func TestRetryAfterHTTPDate(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", time.Now().Add(time.Second).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	})
	if _, err := get(c); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("%d attempts, want 2", hits)
	}
}

// A long Retry-After is reported with the wait, not sat through.
func TestRetryAfterTooLongIsReported(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := get(c)
	e, ok := err.(*Error)
	if !ok || e.Kind != KindBlocked || !e.RetryAfterSet || e.RetryAfter != 10*time.Minute {
		t.Fatalf("%v", err)
	}
	if hits != 1 || !strings.Contains(e.Msg, "10m") {
		t.Fatalf("%d attempts, message %q", hits, e.Msg)
	}
}

// Without a Retry-After there is nothing to honour, so no retry.
func Test429WithoutRetryAfterIsNotRetried(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := get(c)
	e, ok := err.(*Error)
	if !ok || e.Kind != KindBlocked || e.RetryAfterSet || hits != 1 {
		t.Fatalf("%v, %d attempts", err, hits)
	}
}

// A DataDome 403 must never be retried: retrying only deepens the block.
func Test403IsNotRetried(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("X-Datadome", "protected")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<html>datadome</html>"))
	})
	_, err := get(c)
	e, ok := err.(*Error)
	if !ok || e.Kind != KindBlocked || e.Status != http.StatusForbidden {
		t.Fatalf("%v", err)
	}
	if hits != 1 {
		t.Fatalf("%d attempts, want 1", hits)
	}
}

// Retries keep the client's request spacing.
func TestRetryRespectsTheGap(t *testing.T) {
	hits := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ok"))
	})
	c.Gap = 40 * time.Millisecond
	start := time.Now()
	if _, err := get(c); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < c.Gap {
		t.Fatalf("two attempts took %s, less than the %s gap", d, c.Gap)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in    string
		want  time.Duration
		found bool
	}{
		{"120", 2 * time.Minute, true},
		{"0", 0, true},
		{"-1", 0, false},
		{"soon", 0, false},
		{"", 0, false},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
	}
	for _, tc := range cases {
		got, found := retryAfter(tc.in, now)
		if found != tc.found || (found && got != tc.want) {
			t.Errorf("retryAfter(%q) = %s, %v; want %s, %v", tc.in, got, found, tc.want, tc.found)
		}
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	c := New("test")
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, retryCap, retryCap}
	for attempt, w := range want {
		if got := c.backoff(attempt); got != w {
			t.Errorf("backoff(%d) = %s, want %s", attempt, got, w)
		}
	}
}
