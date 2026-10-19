package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/config"
)

func TestConfigCommands(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	out, errOut, code := run("config", "path")
	if code != 0 || !strings.Contains(out, "config.toml") || !strings.Contains(out, "state.db") {
		t.Fatalf("path: %d %s %s", code, out, errOut)
	}
	if out, _, code := run("config", "list"); code != 0 {
		t.Fatalf("list: %d %q", code, out)
	} else if cfg := decode[config.Config](t, out); cfg.Store != 0 {
		t.Fatalf("initial config: %+v", cfg)
	}
	if out, _, code := run("config", "set", "store", "4"); code != 0 {
		t.Fatalf("set store: %d %q", code, out)
	}
	if out, _, code := run("config", "get", "store"); code != 0 || strings.TrimSpace(out) != "4" {
		t.Fatalf("get store: %d %q", code, out)
	}
	if _, _, code := run("config", "set", "watch.interval", "1h"); code != 0 {
		t.Fatalf("set interval: %d", code)
	}
	if out, _, _ := run("config", "get", "watch.interval"); strings.TrimSpace(out) != `"1h"` {
		t.Fatalf("get interval: %q", out)
	}
	if _, _, code := run("config", "set", "sinks.phone.type", "ntfy"); code != 0 {
		t.Fatalf("set sink: %d", code)
	}
	if out, _, _ := run("config", "get", "sinks.phone"); !strings.Contains(out, "ntfy") {
		t.Fatalf("get sink: %q", out)
	}
	for _, args := range [][]string{
		{"config", "get", "nope"},
		{"config", "get", "sinks.phone.type.deeper"},
		{"config", "set", "colour", "red"},
		{"config", "set", "watch.interval", "abc"},
	} {
		if _, _, code := run(args...); code != 2 {
			t.Errorf("%v: %d", args, code)
		}
	}
}

func TestDoctor(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	t.Setenv("HOME", t.TempDir())
	out, errOut, code := run("doctor")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	rep := decode[doctorReport](t, out)
	if !rep.OK || len(rep.Checks) < 4 {
		t.Fatalf("%+v", rep)
	}
	pretty, _, code := run("doctor", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "ok") || !strings.Contains(pretty, "search") {
		t.Fatalf("%s", pretty)
	}

	isolate(t, "http://127.0.0.1:1")
	t.Setenv("HOME", t.TempDir())
	if _, errOut, code := run("doctor"); code != 1 || !strings.Contains(errOut, "doctor found problems") {
		t.Fatalf("broken site: exit %d %q", code, errOut)
	}
}

func TestSkills(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	if out, _, code := run("skills", "list"); code != 0 || !strings.Contains(out, "obramat-usage") {
		t.Fatalf("list: %d %q", code, out)
	}
	if out, _, code := run("skills", "get", "obramat-usage"); code != 0 || !strings.Contains(out, "# obramat usage notes") {
		t.Fatalf("get: %d %q", code, out)
	}
	if _, _, code := run("skills", "get", "nope"); code != 2 {
		t.Fatalf("missing note: %d", code)
	}
}

func TestSinkListAndTest(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	run("config", "set", "sinks.bad.type", "email")
	run("config", "set", "sinks.script.type", "exec")
	run("config", "set", "sinks.script.command", `["sh", "-c", "cat >/dev/null"]`)

	out, errOut, code := run("sink", "list")
	if code != 0 {
		t.Fatalf("list: %d %s", code, errOut)
	}
	rows := decode[[]sinkRow](t, out)
	if len(rows) != 2 {
		t.Fatalf("%+v", rows)
	}
	for _, r := range rows {
		if r.Name == "bad" && r.Problem == "" {
			t.Fatalf("a broken sink has no problem: %+v", r)
		}
	}
	if pretty, _, code := run("sink", "list", "--format", "pretty"); code != 0 || !strings.Contains(pretty, "SINK") {
		t.Fatalf("%s", pretty)
	}
	if out, errOut, code := run("sink", "test", "script"); code != 0 || !strings.Contains(out, `"delivered": true`) {
		t.Fatalf("test: %d %q %q", code, out, errOut)
	}
	if _, _, code := run("sink", "test", "nope"); code != 2 {
		t.Fatalf("unknown sink: %d", code)
	}
}

const cookieExport = `[
  {"name":"datadome","value":"SYNTH_DD","domain":".obramat.es","path":"/","secure":true},
  {"name":"customer_context","value":"%7B%22main_store%22%3A%224%22%7D","domain":".obramat.es","path":"/"}
]`

func TestAuthFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "datadome", Value: "ROTATED", Domain: ".obramat.es", Path: "/"})
			w.Write([]byte("<html>ok</html>"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	isolate(t, srv.URL)

	if out, _, code := run("auth", "status"); code != 0 || !strings.Contains(out, `"source": "none"`) {
		t.Fatalf("status before login: %d %q", code, out)
	}
	if _, _, code := run("auth", "status", "--check"); code != 3 {
		t.Fatalf("check without a session: %d", code)
	}

	file := t.TempDir() + "/cookies.json"
	if err := os.WriteFile(file, []byte(cookieExport), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, errOut, code := run("auth", "login", "--cookies", file, "--no-check"); code != 0 {
		t.Fatalf("login: %d %q %q", code, out, errOut)
	}
	if pretty, _, code := run("auth", "login", "--cookies", file, "--no-check", "--format", "pretty"); code != 0 ||
		!strings.Contains(pretty, "imported 2 obramat.es cookies") {
		t.Fatalf("login pretty: %s", pretty)
	}
	out, errOut, code := run("auth", "status", "--check")
	if code != 0 {
		t.Fatalf("check: %d %s", code, errOut)
	}
	if st := decode[statusResult](t, out); !st.Checked || st.Valid == nil || !*st.Valid || st.Session == nil || !st.Session.HasDataDome {
		t.Fatalf("%+v", st)
	}
	// The storefront rotated the datadome cookie; the file session is written back.
	raw, err := os.ReadFile(sessionPath())
	if err != nil || !strings.Contains(string(raw), "ROTATED") {
		t.Fatalf("rotated session not persisted: %q %v", raw, err)
	}
	if pretty, _, code := run("auth", "status", "--format", "pretty"); code != 0 || !strings.Contains(pretty, "datadome") {
		t.Fatalf("status pretty: %s", pretty)
	}

	if out, _, code := run("auth", "logout"); code != 0 || !strings.Contains(out, `"removed": true`) {
		t.Fatalf("logout: %d %q", code, out)
	}
	if _, _, code := run("auth", "status", "--check"); code != 3 {
		t.Fatalf("check after logout: %d", code)
	}
	if _, _, code := run("auth", "login", "--cookies", "/does/not/exist", "--no-check"); code != 1 {
		t.Fatalf("missing file: %d", code)
	}
	t.Setenv(config.CookieJarEnv, file)
	if _, _, code := run("auth", "login", "--cookies", file, "--no-check"); code != 2 {
		t.Fatalf("login with a cookie jar in the env: %d", code)
	}
}

func TestAuthStatusCheckReportsARefusal(t *testing.T) {
	isolate(t, site(t, map[string]string{"/": "403:datadome_403.html"}))
	file := t.TempDir() + "/cookies.json"
	if err := os.WriteFile(file, []byte(cookieExport), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, code := run("auth", "login", "--cookies", file, "--no-check"); code != 0 {
		t.Fatalf("login: %d", code)
	}
	out, _, code := run("auth", "status", "--check")
	if code != 0 {
		t.Fatalf("a refused check must still succeed: %d", code)
	}
	if st := decode[statusResult](t, out); st.Valid == nil || *st.Valid || st.CheckError == "" {
		t.Fatalf("%+v", st)
	}
}

func TestMCPInstallAndServe(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	out, errOut, code := run("mcp", "install", "claude-code")
	if code != 0 {
		t.Fatalf("install: %d %s", code, errOut)
	}
	if !strings.Contains(out, `"action": "added"`) {
		t.Fatalf("%q", out)
	}
	if pretty, _, code := run("mcp", "install", "claude-code", "--format", "pretty"); code != 0 || !strings.Contains(pretty, "unchanged") {
		t.Fatalf("%s", pretty)
	}
	if _, _, code := run("mcp", "install", "vim"); code != 2 {
		t.Fatalf("unknown harness: %d", code)
	}

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search","arguments":{"keywords":"tablero osb"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"product_show","arguments":{"product":"999999"}}}`,
	}, "\n")
	out, errOut, code = runWith(in, "mcp")
	if code != 0 {
		t.Fatalf("serve: %d %s", code, errOut)
	}
	if !strings.Contains(out, "protocolVersion") || !strings.Contains(out, "25064668") || !strings.Contains(out, `"isError":true`) {
		t.Fatalf("%s", out)
	}
}
