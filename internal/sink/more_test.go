package sink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/state"
)

func event(typ string) state.Event {
	item, _ := json.Marshal(map[string]any{
		"ref": "25064668", "title": "TABLERO OSB", "url": "https://www.obramat.es/p/25064668.html",
		"price": map[string]any{"amount": 28.0},
		"store": map[string]any{"name": "Obramat Leganés"},
		"stock": map[string]any{"quantity": 40},
	})
	return state.Event{Type: typ, Watch: "osb", At: time.Now(), Product: item,
		Change: json.RawMessage(`{"from":30,"to":28}`)}
}

func TestSummaryEveryEvent(t *testing.T) {
	cases := []struct {
		typ   string
		title string
		part  string
	}{
		{state.ProductNew, "New: ", "28.00 €"},
		{state.PriceChanged, "Price: ", "30 € -> 28 €"},
		{state.StockChanged, "Stock: ", "30 -> 28 units"},
		{state.OutOfStock, "Out of stock: ", "Obramat Leganés"},
		{state.BackInStock, "Back in stock: ", "40 units"},
		{state.ProductRemoved, "Removed: ", ""},
		{"product.something_else", "product.something_else: ", ""},
	}
	for _, c := range cases {
		title, detail, link := Summary(event(c.typ))
		if !strings.HasPrefix(title, c.title) {
			t.Errorf("%s: title %q", c.typ, title)
		}
		if c.part != "" && !strings.Contains(detail, c.part) {
			t.Errorf("%s: detail %q missing %q", c.typ, detail, c.part)
		}
		if c.typ != state.ProductRemoved && !strings.Contains(detail, "[osb]") {
			t.Errorf("%s: detail lacks the watch: %q", c.typ, detail)
		}
		if link == "" {
			t.Errorf("%s: no link", c.typ)
		}
	}
	// A back-in-stock event without a product body still yields a title.
	if title, _, _ := Summary(state.Event{Type: state.BackInStock, Watch: "w"}); !strings.HasPrefix(title, "Back in stock:") {
		t.Errorf("empty back-in-stock: %q", title)
	}
}

func TestNtfy(t *testing.T) {
	var gotPath, gotTitle, gotTags, gotClick, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotTitle = r.URL.Path, r.Header.Get("Title")
		gotTags, gotClick, gotAuth = r.Header.Get("Tags"), r.Header.Get("Click"), r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte(" secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := config.SinkConfig{Type: "ntfy", URL: srv.URL + "/", Topic: "my-topic", TokenFile: tok}
	if err := Deliver(context.Background(), "phone", s, event(state.ProductNew)); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/my-topic" || gotTitle != "New: TABLERO OSB" || gotTags != "hammer" {
		t.Fatalf("path %q title %q tags %q", gotPath, gotTitle, gotTags)
	}
	if gotAuth != "Bearer secret" || !strings.Contains(gotBody, "28.00") {
		t.Fatalf("auth %q body %q", gotAuth, gotBody)
	}
	if !strings.HasPrefix(gotClick, "https://www.obramat.es/") {
		t.Fatalf("click %q", gotClick)
	}
}

func TestNtfyErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := Deliver(context.Background(), "x", config.SinkConfig{Type: "ntfy", URL: srv.URL, Topic: "t"}, event(state.ProductNew)); err == nil {
		t.Fatal("a 500 from ntfy was ignored")
	}
	s := config.SinkConfig{Type: "ntfy", URL: srv.URL, Topic: "t", TokenFile: filepath.Join(t.TempDir(), "missing")}
	if err := Deliver(context.Background(), "x", s, event(state.ProductNew)); err == nil {
		t.Fatal("a missing token file was ignored")
	}
	if err := Deliver(context.Background(), "x", config.SinkConfig{Type: "ntfy", URL: "://bad", Topic: "t"}, event(state.ProductNew)); err == nil {
		t.Fatal("a bad ntfy URL was accepted")
	}
}

func TestWebhookRaw(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	ev := event(state.PriceChanged)
	if err := Deliver(context.Background(), "chat", config.SinkConfig{Type: "webhook", URL: srv.URL}, ev); err != nil {
		t.Fatal(err)
	}
	var got state.Event
	if err := json.Unmarshal(body, &got); err != nil || got.Type != ev.Type || got.Watch != "osb" {
		t.Fatalf("%s %v", body, err)
	}
}

func TestWebhookTemplates(t *testing.T) {
	for tmpl, key := range map[string]string{"discord": "content", "slack": "text"} {
		t.Run(tmpl, func(t *testing.T) {
			var got map[string]string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&got)
			}))
			defer srv.Close()
			s := config.SinkConfig{Type: "webhook", URL: srv.URL, Template: tmpl}
			if err := Deliver(context.Background(), "chat", s, event(state.PriceChanged)); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(got[key], "Price: TABLERO OSB") || len(got) != 1 {
				t.Fatalf("%s: %v", tmpl, got)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	if err := Deliver(context.Background(), "chat", config.SinkConfig{Type: "webhook", URL: srv.URL}, event(state.ProductNew)); err == nil {
		t.Fatal("a 502 from the webhook was ignored")
	}
	if err := Deliver(context.Background(), "chat", config.SinkConfig{Type: "webhook", URL: "://bad"}, event(state.ProductNew)); err == nil {
		t.Fatal("a bad webhook URL was accepted")
	}
}

func TestExecFailure(t *testing.T) {
	s := config.SinkConfig{Type: "exec", Command: []string{"sh", "-c", "echo boom >&2; exit 3"}}
	err := Deliver(context.Background(), "script", s, event(state.ProductNew))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("%v", err)
	}
}

func TestDeliverRejectsUnknownType(t *testing.T) {
	if err := Deliver(context.Background(), "x", config.SinkConfig{Type: "email"}, event(state.ProductNew)); err == nil {
		t.Fatal("an unknown sink type was delivered")
	}
}
