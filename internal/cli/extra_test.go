package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
)

func TestWatchPrettyAndDelivery(t *testing.T) {
	var posts atomic.Int32
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	run("config", "set", "sinks.hook.type", "webhook")
	run("config", "set", "sinks.hook.url", hook.URL)

	out, errOut, code := run("watch", "add", "search", "tablero osb", "--name", "osb",
		"--notify", "hook", "--emit-initial", "--format", "pretty")
	if code != 0 {
		t.Fatalf("add: exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "WATCH") || !strings.Contains(out, "osb") {
		t.Fatalf("%s", out)
	}
	if posts.Load() != 8 {
		t.Fatalf("%d events delivered, want 8", posts.Load())
	}
	if pretty, _, code := run("watch", "events", "--format", "pretty"); code != 0 || !strings.Contains(pretty, "EVENT") {
		t.Fatalf("%s", pretty)
	}
}

func TestWatchProductListDescribesTheStore(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/search":                    "redirect:/productos/x-25064668.html",
		"/productos/x-25064668.html": "product_25064668_store4.html.gz",
	}))
	if _, errOut, code := run("watch", "add", "product", "25064668", "--name", "board", "--store", "4"); code != 0 {
		t.Fatalf("add: exit %d: %s", code, errOut)
	}
	pretty, _, code := run("watch", "list", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "@4") || !strings.Contains(pretty, "product") {
		t.Fatalf("%s", pretty)
	}
}

func TestAuthImportVariants(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	if out, _, code := runWith(cookieExport, "auth", "login", "--cookies-stdin", "--no-check"); code != 0 ||
		!strings.Contains(out, `"cookies": 2`) {
		t.Fatalf("stdin: %d %q", code, out)
	}
	if out, _, code := runWith("datadome=abc; customer_context=x", "auth", "login", "--cookies-stdin",
		"--cookies-format", "header", "--no-check"); code != 0 || !strings.Contains(out, `"cookies": 2`) {
		t.Fatalf("header: %d %q", code, out)
	}
	har := filepath.Join("..", "api", "testdata", "session.synthetic.har")
	if out, errOut, code := run("auth", "login", "--har", har, "--no-check", "--user-agent", "UA/1"); code != 0 {
		t.Fatalf("har: %d %q %q", code, out, errOut)
	}
	if out, _, code := run("auth", "status"); code != 0 || !strings.Contains(out, "UA/1") {
		t.Fatalf("har user agent: %d %q", code, out)
	}
	// A session without datadome still imports, with a warning.
	file := t.TempDir() + "/cookies.json"
	if err := os.WriteFile(file, []byte(`[{"name":"customer_context","value":"x","domain":".obramat.es"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	pretty, _, code := run("auth", "login", "--cookies", file, "--no-check", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "no datadome cookie") {
		t.Fatalf("warning: %s", pretty)
	}
}

func TestAuthLoginCheckRefused(t *testing.T) {
	isolate(t, site(t, map[string]string{"/": "403:datadome_403.html"}))
	file := t.TempDir() + "/cookies.json"
	if err := os.WriteFile(file, []byte(cookieExport), 0o600); err != nil {
		t.Fatal(err)
	}
	pretty, _, code := run("auth", "login", "--cookies", file, "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "storefront refused it") {
		t.Fatalf("refused login: %d\n%s", code, pretty)
	}
	if pretty, _, code := run("auth", "status", "--check", "--format", "pretty"); code != 0 ||
		!strings.Contains(pretty, "refused:") {
		t.Fatalf("refused status: %d\n%s", code, pretty)
	}
}

func TestStoreContextResolution(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	cacheStores(t, leganes, api.Store{ID: 12, Name: "Obramat Usera", Slug: "obramat-usera"})
	a := &App{Paths: config.DefaultPaths(), Cfg: config.Config{Store: 4}}
	ctx := context.Background()

	if ref, err := a.storeContext(ctx, ""); err != nil || ref.ID != 4 || ref.Name != "Obramat Leganés" {
		t.Fatalf("config: %+v %v", ref, err)
	}
	t.Setenv("OBRAMAT_STORE", "12")
	if ref, _ := a.storeContext(ctx, ""); ref.ID != 12 || ref.Name != "Obramat Usera" {
		t.Fatalf("env: %+v", ref)
	}
	if ref, _ := a.storeContext(ctx, "none"); ref.ID != 0 {
		t.Fatalf("none: %+v", ref)
	}
	if ref, err := a.storeContext(ctx, "usera"); err != nil || ref.ID != 12 {
		t.Fatalf("slug: %+v %v", ref, err)
	}
	if ref, err := a.storeContext(ctx, "0"); err != nil || ref.ID != 0 {
		t.Fatalf("zero: %+v %v", ref, err)
	}
	if _, err := a.findStore(ctx, "999"); err == nil {
		t.Fatal("an unknown numeric store resolved")
	}
	if _, err := a.findStore(ctx, "obramat"); err == nil {
		t.Fatal("an ambiguous store argument resolved")
	}
	if got := refFromPath("/productos/no-ref.html", "fallback"); got != "fallback" {
		t.Fatalf("refFromPath = %q", got)
	}
}

func TestRootSetupPaths(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	if _, _, code := run("help"); code != 0 {
		t.Fatalf("help: %d", code)
	}
	if _, _, code := run("nope"); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
	if _, _, code := run("config", "list", "--debug"); code != 0 {
		t.Fatalf("debug: %d", code)
	}
	// A corrupt config is a usage error.
	cfgDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "obramat-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("store = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := run("search", "x"); code != 2 {
		t.Fatalf("corrupt config: %d", code)
	}
}

func TestRootSetupRejectsACorruptSession(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	if err := os.MkdirAll(filepath.Dir(sessionPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath(), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := run("search", "x"); code != 1 || !strings.Contains(errOut, "corrupt") {
		t.Fatalf("corrupt session: %d %q", code, errOut)
	}
}

func TestConfigDocReadError(t *testing.T) {
	a := &App{Paths: config.Paths{ConfigFile: t.TempDir()}}
	if _, err := a.configDoc(); err == nil {
		t.Fatal("a directory read as a config")
	}
}

func TestConfirm(t *testing.T) {
	var errb bytes.Buffer
	a := &App{Interactive: false, Stderr: &errb}
	if err := a.confirm(false, "sure?"); err == nil {
		t.Fatal("no terminal and no --yes was accepted")
	}
	if err := a.confirm(true, "sure?"); err != nil {
		t.Fatalf("--yes: %v", err)
	}
	a = &App{Interactive: true, Stdin: strings.NewReader("n\n"), Stderr: &errb}
	if err := a.confirm(false, "sure?"); err == nil {
		t.Fatal("a no was accepted")
	}
	a = &App{Interactive: true, Stdin: strings.NewReader("y\n"), Stderr: &errb}
	if err := a.confirm(false, "sure?"); err != nil {
		t.Fatalf("yes: %v", err)
	}
}

func TestHarvestWithoutABrowser(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	if _, errOut, code := run("auth", "harvest", "--chrome", "/does/not/exist"); code != 1 ||
		!strings.Contains(errOut, "not a browser") {
		t.Fatalf("harvest: %d %q", code, errOut)
	}
	if _, _, code := run("watch", "service", "install", "--interval", "1m"); code != 2 {
		t.Fatalf("short service interval: %d", code)
	}
}

func TestMoreCommandPaths(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	// The pretty watch view, and the interval from config.
	if _, _, code := run("config", "set", "watch.interval", "1h"); code != 0 {
		t.Fatalf("set interval: %d", code)
	}
	pretty, errOut, code := run("watch", "add", "search", "osb", "--name", "cfg", "--format", "pretty")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errOut)
	}
	if !strings.Contains(pretty, "cfg") || !strings.Contains(pretty, "TARGET") {
		t.Fatalf("%s", pretty)
	}
	if _, _, code := run("watch", "remove", "cfg"); code != 2 {
		t.Fatalf("remove without --yes: %d", code)
	}
	if _, _, code := run("watch", "run", "--interval", "1m"); code != 2 {
		t.Fatalf("short run interval: %d", code)
	}
	if _, _, code := run("search", "filters"); code != 2 {
		t.Fatalf("filters without a query: %d", code)
	}
	if _, _, code := run("auth", "login"); code != 2 {
		t.Fatalf("login with no source: %d", code)
	}
	jar := filepath.Join(t.TempDir(), "jar.json")
	if err := os.WriteFile(jar, []byte(cookieExport), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.CookieJarEnv, jar)
	if _, _, code := run("auth", "harvest"); code != 2 {
		t.Fatalf("harvest with a cookie jar: %d", code)
	}
}

func TestDeliveryFailureIsANotice(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	run("config", "set", "sinks.hook.type", "webhook")
	run("config", "set", "sinks.hook.url", "http://127.0.0.1:1")
	_, errOut, code := run("watch", "add", "search", "osb", "--name", "w", "--notify", "hook", "--emit-initial")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errOut)
	}
	if !strings.Contains(errOut, "sink hook") {
		t.Fatalf("no delivery notice: %q", errOut)
	}
}

// A watch with an unparseable configured interval is a usage error.
func TestWatchBadConfiguredInterval(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "obramat-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[watch]\ninterval = \"abc\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := run("watch", "add", "search", "x", "--name", "w"); code != 2 ||
		!strings.Contains(errOut, "watch.interval") {
		t.Fatalf("bad configured interval: %d %q", code, errOut)
	}
}
