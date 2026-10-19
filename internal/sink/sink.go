// Package sink delivers watch events to places other than stdout: an ntfy
// topic, a JSON webhook, or a local program. Sinks are declared in config.toml.
package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/state"
)

// Validate checks a sink's fields without sending anything.
func Validate(name string, s config.SinkConfig) error {
	switch s.Type {
	case "ntfy":
		if s.URL == "" || s.Topic == "" {
			return fmt.Errorf("sink %q: ntfy needs url and topic", name)
		}
	case "webhook":
		if s.URL == "" {
			return fmt.Errorf("sink %q: webhook needs url", name)
		}
		if s.Template != "" && s.Template != "discord" && s.Template != "slack" {
			return fmt.Errorf("sink %q: template must be discord or slack", name)
		}
	case "exec":
		if len(s.Command) == 0 {
			return fmt.Errorf("sink %q: exec needs command", name)
		}
	default:
		return fmt.Errorf("sink %q: type must be ntfy, webhook or exec, not %q", name, s.Type)
	}
	return nil
}

// Deliver sends one event. A failure is returned, never fatal: one dead
// webhook must not stop the other sinks or the state commit.
func Deliver(ctx context.Context, name string, s config.SinkConfig, ev state.Event) error {
	if err := Validate(name, s); err != nil {
		return err
	}
	switch s.Type {
	case "ntfy":
		return ntfy(ctx, s, ev)
	case "webhook":
		return webhook(ctx, s, ev)
	}
	return run(ctx, s, ev)
}

// Summary is the short human form of an event, for push notifications and
// chat webhooks: a title, a detail line and the product URL.
func Summary(ev state.Event) (title, detail, link string) {
	var p struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Price *struct {
			Amount float64 `json:"amount"`
		} `json:"price"`
		Store *struct {
			Name string `json:"name"`
		} `json:"store"`
		Stock *struct {
			Quantity *int `json:"quantity"`
		} `json:"stock"`
	}
	_ = json.Unmarshal(ev.Item, &p)
	var ch struct{ From, To any }
	_ = json.Unmarshal(ev.Change, &ch)
	price := ""
	if p.Price != nil {
		price = fmt.Sprintf("%.2f €", p.Price.Amount)
	}
	where := ""
	if p.Store != nil && p.Store.Name != "" {
		where = " at " + p.Store.Name
	}
	switch ev.Type {
	case "product.new":
		title, detail = "New: "+p.Title, price
	case "product.price_changed":
		title, detail = "Price: "+p.Title, fmt.Sprintf("%v € -> %v €%s", ch.From, ch.To, where)
	case "product.stock_changed":
		title, detail = "Stock: "+p.Title, fmt.Sprintf("%v -> %v units%s", ch.From, ch.To, where)
	case "product.out_of_stock":
		title, detail = "Out of stock: "+p.Title, strings.TrimPrefix(where, " at ")
	case "product.back_in_stock":
		title = "Back in stock: " + p.Title
		detail = price + where
		if p.Stock != nil && p.Stock.Quantity != nil {
			detail = fmt.Sprintf("%d units%s, %s", *p.Stock.Quantity, where, price)
		}
	case "product.removed":
		title = "Removed: " + p.Title
	default:
		title = ev.Type + ": " + p.Title
	}
	return title, strings.TrimSpace(detail + " [" + ev.Watch + "]"), p.URL
}

var client = &http.Client{Timeout: 15 * time.Second}

func ntfy(ctx context.Context, s config.SinkConfig, ev state.Event) error {
	title, detail, link := Summary(ev)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.URL, "/")+"/"+s.Topic, strings.NewReader(detail))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	req.Header.Set("Tags", "hammer")
	if link != "" {
		req.Header.Set("Click", link)
	}
	if s.TokenFile != "" {
		tok, err := os.ReadFile(home(s.TokenFile))
		if err != nil {
			return fmt.Errorf("ntfy token file: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	}
	return post(req)
}

func webhook(ctx context.Context, s config.SinkConfig, ev state.Event) error {
	var body any = ev
	if s.Template != "" {
		title, detail, link := Summary(ev)
		key := "content" // discord
		if s.Template == "slack" {
			key = "text"
		}
		body = map[string]string{key: title + "\n" + detail + "\n" + link}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return post(req)
}

func post(req *http.Request) error {
	req.Header.Set("User-Agent", "obramat-cli")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return nil
}

// run starts the program with the event JSON on stdin and its type and watch
// in the environment.
func run(ctx context.Context, s config.SinkConfig, ev state.Event) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv := make([]string, len(s.Command))
	for i, a := range s.Command {
		argv[i] = home(a)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Env = append(os.Environ(), "OBRAMAT_EVENT_TYPE="+ev.Type, "OBRAMAT_WATCH="+ev.Watch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func home(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return p
}
