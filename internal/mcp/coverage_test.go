package mcp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeProtocolErrors(t *testing.T) {
	srv := &Server{Version: "1.0.0", Run: func(context.Context, []string) Result {
		return Result{Stdout: "{\"ok\":true}\n", ErrorJSON: "{\"code\":5}\n"}
	}}
	in := strings.Join([]string{
		"",
		"not json",
		`{"id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":"not an object"}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"search","arguments":{"keywords":"x"}}}`,
	}, "\n")
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"-32700", "-32600", "-32601", "-32602", `"tools":[`, `"isError":true`, `"content":[`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(strings.TrimSpace(got), "\n") + 1; n != 7 {
		t.Fatalf("%d answers, want 7 (blank line ignored):\n%s", n, got)
	}
}

func TestInitializeDefaultsToTheNewestVersion(t *testing.T) {
	srv := &Server{Version: "1", Run: func(context.Context, []string) Result { return Result{} }}
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), protocolVersions[len(protocolVersions)-1]) {
		t.Fatalf("%s", out.String())
	}
}

func TestInstallCursorAndErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ch, err := Install("cursor", "/bin/obramat", []string{"mcp"})
	if err != nil || ch.Action != "added" {
		t.Fatalf("%+v %v", ch, err)
	}
	fi, err := os.Stat(filepath.Join(home, ".cursor", "mcp.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cursor config: %v %v", fi.Mode().Perm(), err)
	}
	if _, err := Install("vim", "/bin/obramat", nil); err == nil {
		t.Fatal("an unknown harness was accepted")
	}
}

func TestInstallRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	for name, body := range map[string]string{
		"not an object": `[]`,
		"trailing data": `{}{}`,
		"servers":       `{"mcpServers": 5}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Install("claude-code", "/bin/obramat", []string{"mcp"}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestInstallTOMLFreshAndUnrewritable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	path := filepath.Join(dir, "config.toml")

	ch, err := Install("codex", "/bin/obramat", []string{"mcp"})
	if err != nil || ch.Action != "added" {
		t.Fatalf("fresh file: %+v %v", ch, err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "[mcp_servers.obramat]") {
		t.Fatalf("%s", raw)
	}

	other := filepath.Join(dir, "inline.toml")
	if err := os.WriteFile(other, []byte("mcp_servers = { obramat = { command = 'x' } }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", "")
	// Point codex at the inline file by making it the home config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("mcp_servers = { obramat = { command = 'x' } }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("codex", "/bin/obramat", []string{"mcp"}); err == nil {
		t.Fatal("an inline table was rewritten instead of refused")
	}
}

func TestInstallRejectsBrokenTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("store = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("codex", "/bin/obramat", []string{"mcp"}); err == nil {
		t.Fatal("broken TOML was accepted")
	}
}

func TestLookupAndNames(t *testing.T) {
	if _, ok := lookup("cart_add"); ok {
		t.Fatal("a write tool was found")
	}
	names := Names()
	if len(names) != len(Tools) || names[0] != "search" {
		t.Fatalf("%v", names)
	}
}
