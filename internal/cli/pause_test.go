package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolate(t *testing.T, base string) string {
	t.Helper()
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(k, t.TempDir())
	}
	t.Setenv("OBRAMAT_BASE_URL", base)
	t.Setenv("OBRAMAT_STORE", "")
	t.Setenv("OBRAMAT_CONFIG", "")
	return filepath.Join(os.Getenv("XDG_CACHE_HOME"), "obramat-cli", "blocked-until")
}

func run(args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	code := Execute("test", args, strings.NewReader(""), &out, &errb)
	return out.String(), errb.String(), code
}

// Regression: a second sweep right after a first one got a DataDome 403,
// and the address stayed refused for minutes afterwards. After a block the
// CLI must stop sending requests for a while instead of retrying.
func TestBlockStartsAPause(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("X-Datadome", "protected")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<html><script>var dd={'host':'geo.captcha-delivery.com'}</script></html>"))
	}))
	defer srv.Close()
	pause := isolate(t, srv.URL)

	if _, _, code := run("search", "yeso"); code != 5 || hits != 1 {
		t.Fatalf("first: exit %d, %d requests", code, hits)
	}
	if _, err := os.Stat(pause); err != nil {
		t.Fatalf("no pause recorded: %v", err)
	}
	_, errOut, code := run("product", "show", "25064668")
	if code != 5 || hits != 1 || !strings.Contains(errOut, "paused until") {
		t.Fatalf("during the pause: exit %d, %d requests, %q", code, hits, errOut)
	}
	if out, _, code := run("config", "path"); code != 0 || !strings.Contains(out, "config.toml") {
		t.Fatalf("local commands must keep working: %d", code)
	}
	if out, _, code := run("watch", "list"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("watch list: %d %q", code, out)
	}
}

func TestNeedsSite(t *testing.T) {
	root := CommandTree("test")
	for path, want := range map[string]bool{
		"search": true, "store stock": true, "watch check": true, "watch add product": true,
		"watch list": false, "config set": false, "sink test": false, "skills get": false,
	} {
		cmd, _, err := root.Find(strings.Fields(path))
		if err != nil {
			t.Fatal(err)
		}
		if got := needsSite(cmd); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
}

func TestStoreFlagNoneClearsTheDefault(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	a := &App{}
	a.Cfg.Store = 4
	if s, err := a.storeContext(t.Context(), "none"); err != nil || s.ID != 0 {
		t.Fatalf("%+v %v", s, err)
	}
	if s, _ := a.storeContext(t.Context(), ""); s.ID != 4 {
		t.Fatalf("config default: %+v", s)
	}
	t.Setenv("OBRAMAT_STORE", "12")
	if s, _ := a.storeContext(t.Context(), ""); s.ID != 12 {
		t.Fatalf("env beats config: %+v", s)
	}
}

// Regression: `config set watch.interval abc` was saved and only failed at
// the next watch command.
func TestConfigSetChecksValues(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	for _, args := range [][]string{{"watch.interval", "abc"}, {"watch.interval", "1m"}, {"store", "leganes"}, {"sinks.x.command", "notanarray"}, {"colour", "red"}} {
		if _, _, code := run(append([]string{"config", "set"}, args...)...); code != 2 {
			t.Errorf("config set %v: exit %d", args, code)
		}
	}
	if _, _, code := run("config", "set", "watch.interval", "2h"); code != 0 {
		t.Fatalf("valid interval refused: %d", code)
	}
	if out, _, _ := run("config", "get", "watch.interval"); strings.TrimSpace(out) != `"2h"` {
		t.Fatalf("get: %q", out)
	}
}
