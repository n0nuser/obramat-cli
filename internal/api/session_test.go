package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func cookieNames(s Session) string {
	var n []string
	for _, c := range s.Cookies {
		n = append(n, c.Name)
	}
	return strings.Join(n, ",")
}

// All *.synthetic.* fixtures are hand-written: real exports carry live tokens.
func TestParseExportFormats(t *testing.T) {
	cases := []struct {
		file, format string
	}{
		{"cookies_editor.synthetic.json", FormatJSON},
		{"cookies_browser.synthetic.json", FormatJSON},
		{"cookies.synthetic.txt", FormatNetscape},
		{"session.synthetic.har", FormatHAR},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			res, err := ParseExport([]byte(fixture(t, tc.file)), FormatAuto)
			if err != nil {
				t.Fatal(err)
			}
			if res.Format != tc.format {
				t.Fatalf("detected %s, want %s", res.Format, tc.format)
			}
			if got := cookieNames(res.Session); got != "customer_context,datadome" {
				t.Fatalf("cookies %s, want customer_context,datadome (other domains dropped)", got)
			}
			if res.Session.Dirty() {
				t.Fatal("a fresh import is not dirty")
			}
			roundTrip(t, res.Session.Cookies)
		})
	}
}

// Regression: a {"userAgent","cookies"} export imported the cookies but
// dropped the user agent, so the client replayed Chrome 155 cookies as the
// default browser's user agent and the pair did not match.
func TestParseExportBrowserWrapperKeepsUserAgent(t *testing.T) {
	res, err := ParseExport([]byte(fixture(t, "cookies_browser.synthetic.json")), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Session.UserAgent, "Chrome/155") {
		t.Fatalf("user agent %q not taken from the export", res.Session.UserAgent)
	}
	if got := cookieNames(res.Session); got != "customer_context,datadome" {
		t.Fatalf("cookies %s, want customer_context,datadome (other domains dropped)", got)
	}
	if res.Skipped != 1 {
		t.Fatalf("skipped %d, want the one cookie for another site", res.Skipped)
	}
}

func TestParseExportHARKeepsRotatedCookieAndUA(t *testing.T) {
	res, err := ParseExport([]byte(fixture(t, "session.synthetic.har")), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	dd, _ := res.Session.Cookie("datadome")
	if dd.Value != "ROTATED_DD" {
		t.Fatalf("datadome = %q, want the value the last response set", dd.Value)
	}
	if !strings.Contains(res.Session.UserAgent, "Chrome/141") {
		t.Fatalf("user agent %q not taken from the HAR", res.Session.UserAgent)
	}
	if res.Session.ClientHints["Sec-Ch-Ua"] == "" {
		t.Fatalf("client hints not taken from the HAR: %v", res.Session.ClientHints)
	}
}

func TestParseExportNetscapeExpiryAndHTTPOnly(t *testing.T) {
	res, err := ParseExport([]byte(fixture(t, "cookies.synthetic.txt")), FormatAuto)
	if err != nil {
		t.Fatal(err)
	}
	dd, _ := res.Session.Cookie("datadome")
	if dd.Expires.Unix() != 1823896471 || !dd.Secure {
		t.Fatalf("datadome = %+v", dd)
	}
	st, _ := res.Session.Cookie("customer_context")
	if !st.HTTPOnly || !st.Expires.IsZero() {
		t.Fatalf("customer_context = %+v", st)
	}
}

func TestParseExportHeader(t *testing.T) {
	for _, in := range []string{"Cookie: datadome=a; customer_context=b", "datadome=a; customer_context=b"} {
		res, err := ParseExport([]byte(in), FormatAuto)
		if err != nil {
			t.Fatal(err)
		}
		if res.Format != FormatHeader || cookieNames(res.Session) != "customer_context,datadome" {
			t.Fatalf("%q -> %s %s", in, res.Format, cookieNames(res.Session))
		}
	}
}

func TestParseExportRejects(t *testing.T) {
	for name, in := range map[string]string{
		"empty":          "",
		"other domains":  `[{"name":"x","value":"y","domain":".example.com"}]`,
		"har no cookies": `{"log":{"entries":[{"request":{"url":"https://www.obramat.es/","headers":[]},"response":{}}]}}`,
		"broken json":    `[{"name":`,
	} {
		if _, err := ParseExport([]byte(in), FormatAuto); err == nil {
			t.Errorf("%s: accepted", name)
		} else if e, ok := err.(*Error); !ok || e.Kind != KindUsage {
			t.Errorf("%s: %v is not a usage error", name, err)
		}
	}
}

// A lookalike domain must not receive or supply cookies.
func TestOwnDomain(t *testing.T) {
	for d, want := range map[string]bool{
		"obramat.es": true, ".obramat.es": true, "www.obramat.es": true,
		"obramat.es.evil.example": false, "evilobramat.es": false, "obramat.com": false,
	} {
		if ownDomain(d) != want {
			t.Errorf("ownDomain(%q) = %v", d, !want)
		}
	}
}

func TestSessionHeaderMatching(t *testing.T) {
	s := &Session{Cookies: []Cookie{
		{Name: "a", Value: "1", Domain: ".obramat.es", Path: "/"},
		{Name: "b", Value: "2", Domain: "www.obramat.es", Path: "/productos"},
		{Name: "c", Value: "3", Domain: ".obramat.es", Expires: time.Now().Add(-time.Hour)},
		{Name: "d", Value: "4", Domain: ".obramat.es", Secure: true},
	}}
	u, _ := url.Parse("https://www.obramat.es/productos/x.html")
	if got := s.header(u); got != "a=1; b=2; d=4" {
		t.Fatalf("header = %q", got)
	}
	u, _ = url.Parse("http://www.obramat.es/")
	if got := s.header(u); got != "a=1" {
		t.Fatalf("plain-http header = %q, want secure cookies withheld", got)
	}
}

// The datadome cookie and the browser's user agent must both go out, or the
// pair DataDome scores is inconsistent.
func TestClientSendsSessionAndAbsorbsRotation(t *testing.T) {
	var gotCookie, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie, gotUA = r.Header.Get("Cookie"), r.Header.Get("User-Agent")
		http.SetCookie(w, &http.Cookie{Name: "datadome", Value: "NEW", Domain: ".obramat.es", Path: "/", MaxAge: 3600})
		w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()
	c := New("test")
	c.Base, c.Gap = srv.URL, 0
	// The test server is 127.0.0.1, so give the session a cookie for it too.
	c.Session = &Session{Cookies: []Cookie{{Name: "datadome", Value: "OLD", Domain: "127.0.0.1", Path: "/"}}, UserAgent: "UA/1"}
	if err := c.Reachable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotCookie != "datadome=OLD" || gotUA != "UA/1" {
		t.Fatalf("sent Cookie %q UA %q", gotCookie, gotUA)
	}
	if !c.Session.Dirty() {
		t.Fatal("a Set-Cookie for obramat.es must mark the session dirty")
	}
	var rotated bool
	for _, ck := range c.Session.Cookies {
		rotated = rotated || (ck.Domain == ".obramat.es" && ck.Value == "NEW")
	}
	if !rotated {
		t.Fatalf("rotated cookie not stored: %v", c.Session.Cookies)
	}
}

// The store the command asked for wins: a session's own customer_context must
// not ride along beside the one the CLI built for --store.
func TestClientStoreCookieWinsOverSession(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()
	c := New("test")
	c.Base, c.Gap = srv.URL, 0
	c.Session = &Session{Cookies: []Cookie{
		{Name: "datadome", Value: "DD", Domain: "127.0.0.1", Path: "/"},
		{Name: "customer_context", Value: "FROM_BROWSER", Domain: "127.0.0.1", Path: "/"},
	}}
	// Without a store the session's own customer_context rides along.
	if err := c.Reachable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "datadome=DD") || !strings.Contains(got, "customer_context=FROM_BROWSER") {
		t.Fatalf("cookie header without a store: %q", got)
	}
	// With a store, the CLI's cookie replaces the session's.
	if _, err := c.get(context.Background(), request{path: "/", store: StoreRef{ID: 4}, endpoint: "GET /"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "FROM_BROWSER") {
		t.Fatalf("session customer_context leaked with a store: %q", got)
	}
	if !strings.HasPrefix(got, "customer_context=%7B%22main_store%22%3A%224%22") || !strings.Contains(got, "datadome=DD") {
		t.Fatalf("cookie header with a store: %q", got)
	}
}

func TestClientIgnoresForeignSetCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "x", Value: "y", Domain: "example.com"})
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := New("test")
	c.Base, c.Gap = srv.URL, 0
	c.Session = &Session{}
	if err := c.Reachable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(c.Session.Cookies) != 0 || c.Session.Dirty() {
		t.Fatalf("foreign cookie absorbed: %+v", c.Session.Cookies)
	}
}
