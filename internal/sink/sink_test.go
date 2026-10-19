package sink

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/state"
)

func TestValidate(t *testing.T) {
	bad := []config.SinkConfig{{Type: "ntfy", URL: "https://ntfy.sh"}, {Type: "webhook"}, {Type: "exec"}, {Type: "email"}, {Type: "webhook", URL: "u", Template: "teams"}}
	for _, s := range bad {
		if Validate("x", s) == nil {
			t.Errorf("accepted %+v", s)
		}
	}
	if err := Validate("x", config.SinkConfig{Type: "ntfy", URL: "https://ntfy.sh", Topic: "t"}); err != nil {
		t.Error(err)
	}
}

func TestExecGetsEventOnStdin(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ev.json")
	item, _ := json.Marshal(map[string]any{"title": "OSB", "price": map[string]any{"amount": 28}})
	ev := state.Event{Type: "product.price_changed", Watch: "osb", At: time.Now(), Item: item, Change: json.RawMessage(`{"from":30,"to":28}`)}
	s := config.SinkConfig{Type: "exec", Command: []string{"sh", "-c", `cat > "$0"; echo "$OBRAMAT_EVENT_TYPE" >> "$0"`, out}}
	if err := Deliver(context.Background(), "x", s, ev); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	if !strings.Contains(string(raw), `"watch":"osb"`) || !strings.HasSuffix(string(raw), "product.price_changed\n") {
		t.Fatalf("%s", raw)
	}
	title, detail, _ := Summary(ev)
	if title != "Price: OSB" || !strings.Contains(detail, "30 € -> 28 €") {
		t.Fatalf("%q %q", title, detail)
	}
}
