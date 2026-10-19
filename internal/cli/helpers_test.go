package cli

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
)

// fixture reads a captured response from the api package's testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "api", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(name, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		r = gz
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// site serves fixtures by path. An exact key wins; a key ending in "/" is a
// prefix; "redirect:" answers a 307 to a path; "403:" is a DataDome refusal;
// "404:" answers a not-found page.
func site(t *testing.T, routes map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := routes[r.URL.Path]
		if !ok {
			for prefix, n := range routes {
				if strings.HasSuffix(prefix, "/") && strings.HasPrefix(r.URL.Path, prefix) {
					name, ok = n, true
					break
				}
			}
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rest, ok := strings.CutPrefix(name, "redirect:"); ok {
			http.Redirect(w, r, rest, http.StatusTemporaryRedirect)
			return
		}
		status := http.StatusOK
		if rest, ok := strings.CutPrefix(name, "403:"); ok {
			status, name = http.StatusForbidden, rest
			w.Header().Set("X-Datadome", "protected")
		}
		if rest, ok := strings.CutPrefix(name, "404:"); ok {
			status, name = http.StatusNotFound, rest
		}
		w.WriteHeader(status)
		w.Write([]byte(fixture(t, name)))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// runWith feeds stdin to one invocation.
func runWith(stdin string, args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	code := Execute("test", args, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

// decode reads a JSON document the CLI printed.
func decode[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return v
}

// cacheStores writes the store list cache, so commands that need stores do not
// sweep the site.
func cacheStores(t *testing.T, stores ...api.Store) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "obramat-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(stores)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stores.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

var leganes = api.Store{ID: 4, Name: "Obramat Leganés", Slug: "obramat-leganes", Lat: 40.3354, Lng: -3.7656}
