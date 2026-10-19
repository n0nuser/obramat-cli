package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArgvFlagsThenPositionals(t *testing.T) {
	tool, _ := lookup("search")
	argv, err := tool.Argv(map[string]any{"keywords": "-tablero", "brand": []any{"WEBER"}, "max_price": 20.0, "in_stock": true, "store": "4"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"search", "--brand", "WEBER", "--in-stock", "--max-price", "20", "--store", "4", "--", "-tablero"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("%q", argv)
	}
}

func TestArgvRejectsUnknownAndBadTypes(t *testing.T) {
	tool, _ := lookup("product_show")
	for _, in := range []map[string]any{{}, {"product": "1", "colour": "red"}, {"product": 25064668.0}} {
		if _, err := tool.Argv(in); err == nil {
			t.Errorf("accepted %v", in)
		}
	}
	search, _ := lookup("search")
	if _, err := search.Argv(map[string]any{"keywords": "x", "sort": "cheapest"}); err == nil {
		t.Error("accepted a sort outside the enum")
	}
}

func TestServeCallsTheRunner(t *testing.T) {
	var got []string
	srv := &Server{Version: "1.0.0", Run: func(ctx context.Context, argv []string) Result {
		got = argv
		return Result{Stdout: `{"ok":true}` + "\n"}
	}}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"store_near","arguments":{"lat":40.4,"lng":-3.7}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cart_add","arguments":{}}}`,
	}, "\n")
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 answers (the notification gets none), got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-03-26"`) || !strings.Contains(lines[1], `{\"ok\":true}`) || !strings.Contains(lines[2], "-32602") {
		t.Fatalf("%s", out.String())
	}
	if !reflect.DeepEqual(got, []string{"store", "near", "--lat", "40.4", "--lng", "-3.7"}) {
		t.Fatalf("%q", got)
	}
}

func TestInstallJSONKeepsOtherEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	os.WriteFile(path, []byte(`{"theme":"dark","n":12345678901234567890,"mcpServers":{"other":{"command":"x"},"obramat":{"command":"old","env":{"A":"1"}}}}`), 0o600)
	ch, err := Install("claude-code", "/bin/obramat", []string{"mcp"})
	if err != nil || ch.Action != "updated" {
		t.Fatalf("%+v %v", ch, err)
	}
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	s := doc["mcpServers"].(map[string]any)
	if s["other"] == nil || s["obramat"].(map[string]any)["env"] == nil || !strings.Contains(string(raw), "12345678901234567890") || doc["theme"] != "dark" {
		t.Fatalf("%s", raw)
	}
	if ch, _ := Install("claude-code", "/bin/obramat", []string{"mcp"}); ch.Action != "unchanged" {
		t.Fatalf("second run: %s", ch.Action)
	}
}

func TestInstallTOMLKeepsComments(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("# mine\nmodel = \"x\"\n"), 0o600)
	for _, want := range []string{"added", "unchanged"} {
		ch, err := Install("codex", "/bin/obramat", []string{"mcp"})
		if err != nil || ch.Action != want {
			t.Fatalf("want %s: %+v %v", want, ch, err)
		}
	}
	ch, err := Install("codex", "/usr/bin/obramat", []string{"mcp"})
	raw, _ := os.ReadFile(path)
	if err != nil || ch.Action != "updated" || !strings.HasPrefix(string(raw), "# mine\n") || !strings.Contains(string(raw), `command = "/usr/bin/obramat"`) {
		t.Fatalf("%v\n%s", err, raw)
	}
}
