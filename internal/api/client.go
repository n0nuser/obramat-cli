// Package api is the only code that knows obramat.es: hosts, paths, the store
// cookie and the shape of every page and endpoint the CLI reads. Everything
// above it works with the types in this package, so when the site moves the
// fix lands here and nowhere else. It never touches the disk.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultBase = "https://www.obramat.es"

// browserUA is required: the site's bot protection (DataDome) refuses any
// request whose User-Agent is not a browser's with a 403. The CLI's own name
// and version ride along at the end so it is still identifiable in logs.
const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

type Client struct {
	HTTP *http.Client
	Base string
	UA   string
	// Debug, when set, gets one line per request and per response.
	Debug io.Writer
	// Gap is the minimum time between two requests from this client. Sweeps
	// (all stores, several pages) stay polite without each caller sleeping.
	Gap time.Duration
	// SweepGap replaces Gap inside multi-request sweeps (every store, stock
	// everywhere). A dozen requests at Gap got this client blocked once.
	SweepGap time.Duration

	mu   sync.Mutex
	last time.Time
}

// New builds a client. OBRAMAT_BASE_URL points it at a fake server in tests.
func New(version string) *Client {
	base := DefaultBase
	if v := os.Getenv("OBRAMAT_BASE_URL"); v != "" {
		base = strings.TrimRight(v, "/")
	}
	return &Client{
		HTTP:     &http.Client{Timeout: 25 * time.Second},
		Base:     base,
		UA:       browserUA + " obramat-cli/" + version,
		Gap:      400 * time.Millisecond,
		SweepGap: 1500 * time.Millisecond,
	}
}

// StoreRef selects the store whose prices and stock a page should show. The
// zero value means none: the site then shows its national web price and no
// stock quantities.
type StoreRef struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// cookie is the customer_context value the site sets when a visitor picks a
// store. Two fields are enough: main_store and a stores_name entry with a
// non-empty name. Without the name, listings drop most prices (regression
// test TestStoreCookieCarriesName).
func (s StoreRef) cookie() string {
	name := s.Name
	if name == "" {
		name = "Obramat"
	}
	id := strconv.Itoa(s.ID)
	raw, _ := json.Marshal(map[string]any{
		"main_store":  id,
		"stores_name": []map[string]string{{"id": id, "name": name}},
	})
	return "customer_context=" + url.QueryEscape(string(raw))
}

type request struct {
	path     string // starts with /, may carry its own query
	query    url.Values
	store    StoreRef
	json     bool
	endpoint string // template for errors, never carries user input
}

type response struct {
	status int
	body   []byte
	url    *url.URL // after redirects
}

// get performs one GET. 2xx and 404 come back as a response; everything else
// is classified into *Error.
func (c *Client) get(ctx context.Context, r request) (*response, error) {
	u := c.Base + r.path
	if len(r.query) > 0 {
		sep := "?"
		if strings.Contains(r.path, "?") {
			sep = "&"
		}
		u += sep + r.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	if r.json {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	}
	if r.store.ID > 0 {
		req.Header.Set("Cookie", r.store.cookie())
	}

	c.wait(ctx)
	c.debugf("> GET %s", u)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, netError(r.endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, netError(r.endpoint, err)
	}
	c.debugf("< %d %s (%d bytes)", resp.StatusCode, resp.Request.URL, len(body))

	switch {
	case isBlock(resp, body):
		return nil, &Error{Kind: KindBlocked, Status: resp.StatusCode, Endpoint: r.endpoint, Retryable: true,
			Msg: fmt.Sprintf("obramat.es bot protection refused the request (HTTP %d). Wait a few minutes before retrying, and keep the request rate low", resp.StatusCode)}
	case resp.StatusCode >= 200 && resp.StatusCode < 300, resp.StatusCode == http.StatusNotFound:
		return &response{status: resp.StatusCode, body: body, url: resp.Request.URL}, nil
	case resp.StatusCode >= 500:
		return nil, &Error{Kind: KindNetwork, Status: resp.StatusCode, Endpoint: r.endpoint, Retryable: true,
			Msg: fmt.Sprintf("obramat.es is unavailable right now (HTTP %d on %s)", resp.StatusCode, r.endpoint)}
	}
	e := changed(r.endpoint, fmt.Sprintf("HTTP %d", resp.StatusCode), body)
	e.Status = resp.StatusCode
	return nil, e
}

// isBlock recognises a DataDome refusal. The "captcha-delivery" string also
// appears in the Content-Security-Policy of every normal page, so the status
// code has to say refusal first (regression test TestNormalPageIsNotABlock).
func isBlock(resp *http.Response, body []byte) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	return resp.Header.Get("X-Datadome") != "" || bytes.Contains(body, []byte("captcha-delivery.com"))
}

func netError(endpoint string, err error) *Error {
	msg := "could not reach obramat.es: " + err.Error()
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		msg = "obramat.es did not answer in time"
	}
	return &Error{Kind: KindNetwork, Endpoint: endpoint, Retryable: true, Msg: msg}
}

func (c *Client) wait(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.Gap - time.Since(c.last); !c.last.IsZero() && d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
	c.last = time.Now()
}

// sweep slows the client to SweepGap until the returned func is called.
func (c *Client) sweep() (restore func()) {
	c.mu.Lock()
	old := c.Gap
	if c.SweepGap > c.Gap {
		c.Gap = c.SweepGap
	}
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		c.Gap = old
		c.mu.Unlock()
	}
}

func (c *Client) debugf(format string, args ...any) {
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "obramat: "+format+"\n", args...)
	}
}

var reAppliedStore = regexp.MustCompile(`data-store-id="0*(\d+)"`)

// checkStoreApplied makes sure a full page was rendered for the requested
// store. Pages rendered for a store carry its code ("004") in the store
// header; for a code the site does not know they render with no store, and
// their default web prices must not be reported as that store's
// (regression test TestUnknownStoreIsRefused).
func checkStoreApplied(page []byte, store StoreRef) error {
	if store.ID == 0 {
		return nil
	}
	for _, m := range reAppliedStore.FindAllSubmatch(page, -1) {
		if string(m[1]) == strconv.Itoa(store.ID) {
			return nil
		}
	}
	return usagef("obramat.es did not apply store %d: it is not a store code the site knows. See `obramat store list`", store.ID)
}

// abs turns a site path into a full URL on the public host, whatever base the
// client talks to.
func abs(path string) string {
	if path == "" || strings.HasPrefix(path, "http") {
		return path
	}
	return DefaultBase + path
}
