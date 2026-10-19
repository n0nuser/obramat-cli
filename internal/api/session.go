package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Domain is the registrable domain the CLI talks to. Cookies for any other
// domain are dropped at import, so a whole-browser export is safe to pass.
const Domain = "obramat.es"

// Cookie is one browser cookie for obramat.es.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path,omitempty"`
	Expires  time.Time `json:"expires,omitzero"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"http_only,omitempty"`
}

// Session is the browser state the CLI replays: the cookies, and the user
// agent of the browser that earned them (DataDome scores the pair together).
// It is pure data; persistence belongs to the config package.
type Session struct {
	Cookies   []Cookie `json:"cookies"`
	UserAgent string   `json:"user_agent,omitempty"`
	// ClientHints are the browser's Sec-CH-UA* request headers, from a HAR.
	ClientHints map[string]string `json:"client_hints,omitempty"`

	dirty bool
}

// Dirty reports whether a response rotated a cookie since load.
func (s *Session) Dirty() bool { return s != nil && s.dirty }

// Cookie returns the named cookie, if present and not expired.
func (s *Session) Cookie(name string) (Cookie, bool) {
	if s == nil {
		return Cookie{}, false
	}
	now := time.Now()
	for _, c := range s.Cookies {
		if c.Name == name && (c.Expires.IsZero() || c.Expires.After(now)) {
			return c, true
		}
	}
	return Cookie{}, false
}

// header builds the Cookie header value for u.
func (s *Session) header(u *url.URL) string {
	var parts []string
	for _, c := range s.Jar().Cookies(u) {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// Jar exposes the session as an http.CookieJar, so redirects within the site
// send and keep rotated cookies hop by hop.
func (s *Session) Jar() http.CookieJar { return sessionJar{s} }

type sessionJar struct{ s *Session }

// Cookies returns the session cookies that apply to u: host and path match,
// not expired, secure ones only over https.
func (j sessionJar) Cookies(u *url.URL) []*http.Cookie {
	if j.s == nil {
		return nil
	}
	now := time.Now()
	host := u.Hostname()
	var out []*http.Cookie
	for _, c := range j.s.Cookies {
		if !c.Expires.IsZero() && !c.Expires.After(now) {
			continue
		}
		d := strings.TrimPrefix(c.Domain, ".")
		if host != d && !strings.HasSuffix(host, "."+d) {
			continue
		}
		if c.Path != "" && !strings.HasPrefix(u.Path, c.Path) {
			continue
		}
		if c.Secure && u.Scheme != "https" {
			continue
		}
		out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
	}
	return out
}

// SetCookies applies a response's Set-Cookie headers. DataDome rotates its
// cookie on most responses, and replaying the old one goes stale quickly.
func (j sessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if j.s == nil {
		return
	}
	for _, hc := range cookies {
		c := Cookie{Name: hc.Name, Value: hc.Value, Domain: hc.Domain, Path: hc.Path, Secure: hc.Secure, HTTPOnly: hc.HttpOnly, Expires: hc.Expires}
		if c.Domain == "" {
			c.Domain = u.Hostname() // host-only cookie
		} else {
			// net/http strips the leading dot. Regression: rotated domain
			// cookies were stored without it and never matched again.
			c.Domain = "." + strings.TrimPrefix(c.Domain, ".")
		}
		if !ownDomain(c.Domain) {
			continue
		}
		if hc.MaxAge > 0 {
			c.Expires = time.Now().Add(time.Duration(hc.MaxAge) * time.Second)
		}
		remove := hc.MaxAge < 0 || (!c.Expires.IsZero() && c.Expires.Before(time.Now()))
		j.s.set(c, remove)
	}
}

func (s *Session) set(c Cookie, remove bool) {
	for i, old := range s.Cookies {
		if old.Name == c.Name && strings.TrimPrefix(old.Domain, ".") == strings.TrimPrefix(c.Domain, ".") && old.Path == c.Path {
			if remove {
				s.Cookies = append(s.Cookies[:i], s.Cookies[i+1:]...)
				s.dirty = true
			} else if old != c {
				s.Cookies[i] = c
				s.dirty = true
			}
			return
		}
	}
	if !remove {
		s.Cookies = append(s.Cookies, c)
		s.dirty = true
	}
}

func ownDomain(d string) bool {
	d = strings.ToLower(strings.TrimPrefix(d, "."))
	return d == Domain || strings.HasSuffix(d, "."+Domain)
}

// Import formats.
const (
	FormatAuto     = "auto"
	FormatJSON     = "json"
	FormatNetscape = "netscape"
	FormatHeader   = "header"
	FormatHAR      = "har"
)

// ImportResult is a parsed export: the session plus what was left out.
type ImportResult struct {
	Session Session
	Format  string
	Skipped int // cookies for other domains
}

// ParseExport reads a cookie export or a HAR. Accepted: Cookie-Editor /
// EditThisCookie JSON (array or {"url","cookies"} wrapper), DevTools-style
// cookie lists, Netscape cookies.txt, a raw Cookie: header, and HAR 1.2. Only
// obramat.es cookies are kept.
func ParseExport(raw []byte, format string) (ImportResult, error) {
	raw = bytes.TrimPrefix(bytes.TrimSpace(raw), []byte("\xef\xbb\xbf"))
	if len(raw) == 0 {
		return ImportResult{}, usagef("the cookie export is empty")
	}
	if format == "" || format == FormatAuto {
		format = detect(raw)
	}
	var res ImportResult
	var err error
	switch format {
	case FormatJSON:
		res, err = parseJSON(raw)
	case FormatNetscape:
		res, err = parseNetscape(raw)
	case FormatHeader:
		res, err = parseHeader(raw)
	case FormatHAR:
		res, err = parseHAR(raw)
	default:
		return res, usagef("unknown cookie format %q. Use auto, json, netscape, header or har", format)
	}
	if err != nil {
		return res, err
	}
	res.Format = format
	if len(res.Session.Cookies) == 0 {
		return res, usagef("the %s export has no obramat.es cookies (%d for other sites were skipped). Export while an obramat.es tab is open", format, res.Skipped)
	}
	sort.SliceStable(res.Session.Cookies, func(i, j int) bool { return res.Session.Cookies[i].Name < res.Session.Cookies[j].Name })
	return res, nil
}

func detect(raw []byte) string {
	switch raw[0] {
	case '{':
		if bytes.Contains(raw[:min(len(raw), 4096)], []byte(`"log"`)) {
			return FormatHAR
		}
		return FormatJSON
	case '[':
		return FormatJSON
	}
	first, _, _ := bytes.Cut(raw, []byte("\n"))
	if bytes.HasPrefix(first, []byte("#")) || bytes.Count(first, []byte("\t")) >= 6 {
		return FormatNetscape
	}
	return FormatHeader
}

// jsonCookie covers the field spellings of the common exporters.
type jsonCookie struct {
	Name           string          `json:"name"`
	Value          string          `json:"value"`
	Domain         string          `json:"domain"`
	Path           string          `json:"path"`
	Secure         bool            `json:"secure"`
	HTTPOnly       bool            `json:"httpOnly"`
	ExpirationDate float64         `json:"expirationDate"` // Cookie-Editor, EditThisCookie: unix seconds
	Expires        json.RawMessage `json:"expires"`        // DevTools/Playwright: unix seconds; HAR: RFC 3339
}

func (j jsonCookie) cookie() Cookie {
	c := Cookie{Name: j.Name, Value: j.Value, Domain: j.Domain, Path: j.Path, Secure: j.Secure, HTTPOnly: j.HTTPOnly}
	switch {
	case j.ExpirationDate > 0:
		c.Expires = unixFloat(j.ExpirationDate)
	case len(j.Expires) > 0:
		var f float64
		var s string
		if json.Unmarshal(j.Expires, &f) == nil && f > 0 {
			c.Expires = unixFloat(f)
		} else if json.Unmarshal(j.Expires, &s) == nil && s != "" {
			c.Expires, _ = time.Parse(time.RFC3339, s)
		}
	}
	return c
}

func unixFloat(f float64) time.Time {
	sec := int64(f)
	return time.Unix(sec, int64((f-float64(sec))*1e9)).UTC()
}

func parseJSON(raw []byte) (ImportResult, error) {
	var list []jsonCookie
	var userAgent string
	if raw[0] == '{' {
		// Cookie-Editor wraps the list as {"url","cookies"}; a script that
		// reads document.cookie often wraps it as {"userAgent","cookies"}.
		// Both are the same list, and the latter also carries the browser
		// that earned the cookies, which the site scores together with them.
		var wrap struct {
			Cookies     []jsonCookie `json:"cookies"`
			UserAgent   string       `json:"userAgent"`
			UserAgentV2 string       `json:"user_agent"`
		}
		if err := json.Unmarshal(raw, &wrap); err != nil {
			return ImportResult{}, usagef("cookie JSON does not parse: %v", err)
		}
		list = wrap.Cookies
		userAgent = wrap.UserAgent
		if userAgent == "" {
			userAgent = wrap.UserAgentV2
		}
	} else if err := json.Unmarshal(raw, &list); err != nil {
		return ImportResult{}, usagef("cookie JSON does not parse: %v", err)
	}
	var res ImportResult
	for _, j := range list {
		res.add(j.cookie())
	}
	res.Session.UserAgent = strings.TrimSpace(userAgent)
	return res, nil
}

func parseNetscape(raw []byte) (ImportResult, error) {
	var res ImportResult
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		httpOnly := false
		if rest, ok := strings.CutPrefix(line, "#HttpOnly_"); ok {
			line, httpOnly = rest, true
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			return res, usagef("cookies.txt line %d has %d fields, want 7", n, len(f))
		}
		c := Cookie{Domain: f[0], Path: f[2], Secure: strings.EqualFold(f[3], "TRUE"), Name: f[5], Value: f[6], HTTPOnly: httpOnly}
		if sec, err := strconv.ParseInt(f[4], 10, 64); err == nil && sec > 0 {
			c.Expires = time.Unix(sec, 0).UTC()
		}
		res.add(c)
	}
	return res, sc.Err()
}

func parseHeader(raw []byte) (ImportResult, error) {
	line := strings.TrimSpace(string(raw))
	if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "cookie") {
		line = v
	}
	var res ImportResult
	for _, part := range strings.Split(line, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || name == "" {
			continue
		}
		res.add(Cookie{Name: name, Value: value, Domain: "." + Domain, Path: "/", Secure: true})
	}
	if len(res.Session.Cookies) == 0 {
		return res, usagef("not a cookie export: expected JSON, cookies.txt, a HAR or a Cookie: header")
	}
	return res, nil
}

// harFile is the part of HAR 1.2 the import reads.
type harFile struct {
	Log struct {
		Entries []struct {
			StartedDateTime time.Time `json:"startedDateTime"`
			Request         struct {
				URL     string       `json:"url"`
				Headers []harHeader  `json:"headers"`
				Cookies []jsonCookie `json:"cookies"`
			} `json:"request"`
			Response struct {
				Headers []harHeader  `json:"headers"`
				Cookies []jsonCookie `json:"cookies"`
			} `json:"response"`
		} `json:"entries"`
	} `json:"log"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// parseHAR replays the recording in order: each obramat.es request's cookies,
// then its response's Set-Cookies, so the result is the jar as it stood after
// the last request. The user agent comes from the last request.
func parseHAR(raw []byte) (ImportResult, error) {
	var h harFile
	if err := json.Unmarshal(raw, &h); err != nil {
		return ImportResult{}, usagef("HAR does not parse: %v", err)
	}
	entries := h.Log.Entries
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].StartedDateTime.Before(entries[j].StartedDateTime) })
	var res ImportResult
	sawHeader := false
	for _, e := range entries {
		u, err := url.Parse(e.Request.URL)
		if err != nil || !ownDomain(u.Hostname()) {
			continue
		}
		for _, hd := range e.Request.Headers {
			switch strings.ToLower(hd.Name) {
			case "user-agent":
				res.Session.UserAgent = hd.Value
			case "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform":
				if res.Session.ClientHints == nil {
					res.Session.ClientHints = map[string]string{}
				}
				res.Session.ClientHints[http.CanonicalHeaderKey(hd.Name)] = hd.Value
			case "cookie":
				// Chrome fills request.cookies too, but the header is the
				// source of truth and the only one some exporters keep.
				sawHeader = true
				if parsed, err := parseHeader([]byte(hd.Value)); err == nil {
					for _, c := range parsed.Session.Cookies {
						res.Session.set(c, false)
					}
				}
			}
		}
		for _, j := range e.Request.Cookies {
			c := j.cookie()
			c.Domain, c.Path, c.Secure = "."+Domain, "/", true
			res.Session.set(c, false)
		}
		for _, j := range e.Response.Cookies {
			c := j.cookie()
			if c.Domain == "" {
				c.Domain = u.Hostname()
			}
			if !ownDomain(c.Domain) {
				res.Skipped++
				continue
			}
			res.Session.set(c, !c.Expires.IsZero() && c.Expires.Before(time.Now()))
		}
	}
	res.Session.dirty = false
	if len(res.Session.Cookies) == 0 && !sawHeader {
		return res, usagef("the HAR has no cookies. Chrome strips them unless you enable \"Allow to generate HAR with sensitive data\" and use \"Export HAR (with sensitive data)\"")
	}
	return res, nil
}

func (r *ImportResult) add(c Cookie) {
	if c.Name == "" || !ownDomain(c.Domain) {
		r.Skipped++
		return
	}
	r.Session.set(c, false)
	r.Session.dirty = false
}

// Summary describes a session without revealing any cookie value.
type Summary struct {
	Cookies     int        `json:"cookies"`
	Names       []string   `json:"names"`
	HasDataDome bool       `json:"has_datadome"`
	UserAgent   string     `json:"user_agent,omitempty"`
	Expires     *time.Time `json:"datadome_expires,omitempty"`
}

// Summarize lists cookie names only. Values are secrets.
func (s *Session) Summarize() Summary {
	sum := Summary{UserAgent: s.UserAgent}
	for _, c := range s.Cookies {
		sum.Names = append(sum.Names, c.Name)
	}
	sum.Cookies = len(sum.Names)
	if dd, ok := s.Cookie("datadome"); ok {
		sum.HasDataDome = true
		if !dd.Expires.IsZero() {
			sum.Expires = &dd.Expires
		}
	}
	return sum
}

// String never prints values.
func (c Cookie) String() string { return fmt.Sprintf("%s=<redacted>; domain=%s", c.Name, c.Domain) }

// StoreFromCookie reads the store a browser visitor had selected out of a
// customer_context cookie value (ADR 0001). It returns false when the value is
// not that cookie or carries no store.
func StoreFromCookie(v string) (StoreRef, bool) {
	raw := v
	if unescaped, err := url.QueryUnescape(v); err == nil {
		raw = unescaped
	}
	var c struct {
		MainStore  string `json:"main_store"`
		StoresName []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"stores_name"`
	}
	if json.Unmarshal([]byte(raw), &c) != nil {
		return StoreRef{}, false
	}
	id, err := strconv.Atoi(strings.TrimSpace(c.MainStore))
	if err != nil || id <= 0 {
		return StoreRef{}, false
	}
	ref := StoreRef{ID: id}
	for _, s := range c.StoresName {
		if s.ID == c.MainStore {
			ref.Name = strings.TrimSpace(s.Name)
		}
	}
	return ref, true
}
