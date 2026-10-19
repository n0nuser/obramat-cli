package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
)

type prettyStub struct{ called bool }

func (p *prettyStub) Pretty(w io.Writer, color bool) { p.called = true }

func TestParseFormat(t *testing.T) {
	for _, f := range []Format{JSON, JSONL, Pretty, Toon} {
		if got, err := ParseFormat(string(f)); err != nil || got != f {
			t.Errorf("ParseFormat(%q) = %q, %v", f, got, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil || !strings.Contains(err.Error(), "json, jsonl, pretty or toon") {
		t.Fatalf("bad format: %v", err)
	}
}

func TestPrintFormats(t *testing.T) {
	type row struct {
		A int    `json:"a"`
		B string `json:"b"`
	}

	t.Run("json", func(t *testing.T) {
		var b bytes.Buffer
		if err := (Printer{W: &b, Format: JSON}).Print(row{1, "x"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), `"a": 1`) {
			t.Fatalf("%q", b.String())
		}
	})
	t.Run("jsonl list", func(t *testing.T) {
		var b bytes.Buffer
		if err := (Printer{W: &b, Format: JSONL}).Print([]row{{1, "x"}, {2, "y"}}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(strings.TrimSpace(b.String()), "\n"); got != 1 {
			t.Fatalf("want two lines, got %q", b.String())
		}
	})
	t.Run("jsonl single", func(t *testing.T) {
		var b bytes.Buffer
		if err := (Printer{W: &b, Format: JSONL}).Print(row{1, "x"}); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(b.String(), "\n") || strings.Count(b.String(), "\n") != 1 {
			t.Fatalf("%q", b.String())
		}
	})
	t.Run("pretty with a Prettier", func(t *testing.T) {
		var b bytes.Buffer
		p := &prettyStub{}
		if err := (Printer{W: &b, Format: Pretty, Color: true}).Print(p); err != nil {
			t.Fatal(err)
		}
		if !p.called {
			t.Fatal("Pretty was not called")
		}
	})
	t.Run("pretty without a Prettier falls back to json", func(t *testing.T) {
		var b bytes.Buffer
		if err := (Printer{W: &b, Format: Pretty}).Print(row{1, "x"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), `"a": 1`) {
			t.Fatalf("%q", b.String())
		}
	})
	t.Run("toon", func(t *testing.T) {
		var b bytes.Buffer
		if err := (Printer{W: &b, Format: Toon}).Print(row{1, "x"}); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(b.String()) == "" {
			t.Fatal("toon wrote nothing")
		}
	})
	t.Run("marshal error", func(t *testing.T) {
		for _, f := range []Format{JSON, JSONL, Toon} {
			if err := (Printer{W: &bytes.Buffer{}, Format: f}).Print(make(chan int)); err == nil {
				t.Errorf("%s accepted a channel", f)
			}
		}
	})
	t.Run("write error", func(t *testing.T) {
		if err := (Printer{W: errWriter{}, Format: JSON}).Print(row{1, "x"}); err == nil {
			t.Fatal("a failing writer was ignored")
		}
	})
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("full") }

func TestTableCleanTruncateDim(t *testing.T) {
	var b bytes.Buffer
	Table(&b, true, []string{"A", "B"}, [][]string{{"one", "two\tthree"}, {"\x1b[31mred\x1b[0m", "x"}})
	out := b.String()
	if !strings.Contains(out, "\x1b[1m") {
		t.Fatal("header not bold with colour on")
	}
	if strings.Contains(out, "\x1b[31m") {
		t.Fatalf("control characters survived Clean: %q", out)
	}
	if strings.Contains(out, "\tt") {
		t.Fatalf("a tab was not turned into a space: %q", out)
	}
	b.Reset()
	Table(&b, false, nil, [][]string{{"a", "b"}})
	if strings.Contains(b.String(), "\x1b[1m") {
		t.Fatal("header bold without colour")
	}

	if got := Truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("Truncate = %q", got)
	}
	if got := Truncate("abc", 4); got != "abc" {
		t.Fatalf("Truncate short = %q", got)
	}
	if got := Dim("x", true); !strings.Contains(got, "x") {
		t.Fatalf("Dim = %q", got)
	}
	if got := Dim("", true); got != "" {
		t.Fatalf("Dim empty = %q", got)
	}
	if got := Clean("a\x00b\x7fc"); got != "abc" {
		t.Fatalf("Clean = %q", got)
	}
}

func TestUsageErrorAndClassify(t *testing.T) {
	ue := Usagef("bad %s", "flag")
	var target *UsageError
	if !errors.As(ue, &target) || target.Error() != "bad flag" {
		t.Fatalf("%v", ue)
	}
	if env := Classify(ue, "1.0", "obramat search"); env.Code != 2 || env.Category != "usage" {
		t.Fatalf("%+v", env)
	}
	if env := Classify(errors.New("boom"), "1.0", "obramat"); env.Code != 1 || env.Category != "generic" {
		t.Fatalf("%+v", env)
	}

	kinds := []struct {
		err  *api.Error
		code int
		cat  string
	}{
		{&api.Error{Kind: api.KindAuth, Msg: "no session"}, 3, "auth"},
		{&api.Error{Kind: api.KindNotFound, Msg: "gone"}, 4, "not_found"},
		{&api.Error{Kind: api.KindBlocked, Msg: "blocked", Retryable: true}, 5, "blocked"},
		{&api.Error{Kind: api.KindNetwork, Msg: "down", Retryable: true}, 6, "network"},
		{&api.Error{Kind: api.KindAPIChanged, Endpoint: "GET /x", Msg: "moved", Body: "body"}, 7, "api_changed"},
	}
	for _, k := range kinds {
		env := Classify(k.err, "1.0", "obramat x")
		if env.Code != k.code || env.Category != k.cat || env.Message != k.err.Msg {
			t.Errorf("%s: %+v", k.cat, env)
		}
		if k.code == 7 && env.IssueURL == "" {
			t.Errorf("api_changed without an issue link")
		}
	}
	if env := Classify(&api.Error{Kind: api.KindGeneric}, "1", "x"); env.Code != 1 {
		t.Fatalf("generic: %+v", env)
	}
}

func TestIssueURL(t *testing.T) {
	url := IssueURL("1.2.3", "obramat search", &api.Error{Endpoint: "GET /search", Status: 500, Body: "boom"})
	for _, want := range []string{"template=api-change.yml", "version=1.2.3", "endpoint=GET+%2Fsearch", "status=500", "body_excerpt=boom"} {
		if !strings.Contains(url, want) {
			t.Errorf("issue url missing %q: %s", want, url)
		}
	}
	url = IssueURL("1", "x", &api.Error{Endpoint: "GET /x"})
	if strings.Contains(url, "status=") {
		t.Errorf("status 0 leaked into %s", url)
	}
}

func TestPrintError(t *testing.T) {
	var b bytes.Buffer
	if code := PrintError(&b, Usagef("bad flag"), false, "1", "x"); code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.HasPrefix(b.String(), "obramat: bad flag") {
		t.Fatalf("%q", b.String())
	}

	b.Reset()
	PrintError(&b, &api.Error{Kind: api.KindBlocked, Msg: "blocked", Retryable: true}, false, "1", "x")
	if !strings.Contains(b.String(), "try:") {
		t.Fatalf("blocked error without a suggestion: %q", b.String())
	}

	b.Reset()
	PrintError(&b, &api.Error{Kind: api.KindAPIChanged, Endpoint: "GET /x", Msg: "moved"}, false, "1", "x")
	if !strings.Contains(b.String(), "issues/new") {
		t.Fatalf("api_changed without an issue link: %q", b.String())
	}

	b.Reset()
	if code := PrintError(&b, &api.Error{Kind: api.KindNetwork, Msg: "down"}, true, "1", "x"); code != 6 {
		t.Fatalf("json error code %d", code)
	}
	var env Envelope
	if err := json.Unmarshal(b.Bytes(), &env); err != nil || env.Category != "network" {
		t.Fatalf("%q %v", b.String(), err)
	}
}

func TestColorEnabled(t *testing.T) {
	if ColorEnabled(true, nil) {
		t.Error("--no-color ignored")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(false, nil) {
		t.Error("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if ColorEnabled(false, nil) {
		t.Error("TERM=dumb ignored")
	}
	t.Setenv("TERM", "xterm")
	if ColorEnabled(false, nil) {
		t.Error("a nil file was treated as a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ColorEnabled(false, f) {
		t.Error("a regular file was treated as a terminal")
	}
	if IsTerminal(nil) {
		t.Error("nil is a terminal")
	}
}
