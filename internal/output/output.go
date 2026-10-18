// Package output renders results (json, jsonl, pretty) on stdout and errors
// (text or a JSON envelope) on stderr, and owns the exit-code mapping.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/n0nuser/obramat-cli/internal/api"
)

type Format string

const (
	JSON   Format = "json"
	JSONL  Format = "jsonl"
	Pretty Format = "pretty"
)

func ParseFormat(s string) (Format, error) {
	switch f := Format(s); f {
	case JSON, JSONL, Pretty:
		return f, nil
	}
	return "", Usagef("--format must be json, jsonl or pretty, not %q", s)
}

// Prettier is a result with its own human rendering. Anything else prints as
// indented JSON under --format pretty.
type Prettier interface {
	Pretty(w io.Writer, color bool)
}

type Printer struct {
	W      io.Writer
	Format Format
	Color  bool
}

// Print writes one result. Under jsonl a list is written one element per
// line, which is what streams (watch events) want.
func (p Printer) Print(v any) error {
	switch p.Format {
	case Pretty:
		if pr, ok := v.(Prettier); ok {
			pr.Pretty(p.W, p.Color)
			return nil
		}
	case JSONL:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &items) == nil {
			for _, it := range items {
				if _, err := fmt.Fprintf(p.W, "%s\n", it); err != nil {
					return err
				}
			}
			return nil
		}
		_, err = fmt.Fprintf(p.W, "%s\n", raw)
		return err
	}
	enc := json.NewEncoder(p.W)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Table prints aligned columns; the header is bold when colour is on.
func Table(w io.Writer, color bool, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if header != nil {
		h := strings.Join(header, "\t")
		if color {
			h = "\x1b[1m" + h + "\x1b[0m"
		}
		fmt.Fprintln(tw, h)
	}
	for _, r := range rows {
		for i := range r {
			r[i] = Clean(r[i])
		}
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// Clean drops control characters from site-sourced text before it reaches a
// terminal, so a product title cannot carry escape sequences.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\x1b' || r < 0x20 && r != '\t' || r == 0x7f {
			return -1
		}
		if r == '\t' {
			return ' '
		}
		return r
	}, s)
}

// Truncate cuts s to n runes with an ellipsis.
func Truncate(s string, n int) string {
	r := []rune(Clean(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

func Dim(s string, color bool) string {
	if !color || s == "" {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

// UsageError is a caller mistake caught in the CLI layer (exit 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func Usagef(format string, args ...any) error { return &UsageError{Msg: fmt.Sprintf(format, args...)} }

// Envelope is the machine-readable error printed with --error-format json.
type Envelope struct {
	Code              int      `json:"code"`
	Category          string   `json:"category"`
	Retryable         bool     `json:"retryable"`
	Message           string   `json:"message"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	Endpoint          string   `json:"endpoint,omitempty"`
	SuggestedCommands []string `json:"suggested_commands,omitempty"`
	IssueURL          string   `json:"issue_url,omitempty"`
}

const Repo = "https://github.com/n0nuser/obramat-cli"

func Classify(err error, version, command string) Envelope {
	env := Envelope{Code: 1, Category: "generic", Message: err.Error()}
	var ae *api.Error
	var ue *UsageError
	switch {
	case errors.As(err, &ue):
		env.Code, env.Category = 2, "usage"
	case errors.As(err, &ae):
		env.Code, env.Category = ae.Kind.ExitCode(), ae.Kind.String()
		env.Retryable, env.HTTPStatus, env.Endpoint = ae.Retryable, ae.Status, ae.Endpoint
		switch ae.Kind {
		case api.KindAPIChanged:
			env.IssueURL = IssueURL(version, command, ae)
			env.SuggestedCommands = []string{"obramat doctor", "rerun with --debug"}
		case api.KindBlocked:
			env.SuggestedCommands = []string{"wait a few minutes, then retry once"}
		case api.KindNetwork:
			env.SuggestedCommands = []string{"obramat doctor"}
		}
	}
	return env
}

// IssueURL prefills .github/ISSUE_TEMPLATE/api-change.yml. It carries the
// command path and endpoint template, never the user's arguments.
func IssueURL(version, command string, e *api.Error) string {
	q := url.Values{}
	q.Set("template", "api-change.yml")
	q.Set("title", "[api change] "+e.Endpoint)
	q.Set("version", version)
	q.Set("os", runtime.GOOS+"/"+runtime.GOARCH)
	q.Set("command", command)
	q.Set("endpoint", e.Endpoint)
	if e.Status != 0 {
		q.Set("status", fmt.Sprint(e.Status))
	}
	q.Set("body_excerpt", e.Body)
	return Repo + "/issues/new?" + q.Encode()
}

// PrintError writes err to w and returns the exit code.
func PrintError(w io.Writer, err error, jsonFormat bool, version, command string) int {
	env := Classify(err, version, command)
	if jsonFormat {
		_ = json.NewEncoder(w).Encode(env)
		return env.Code
	}
	fmt.Fprintf(w, "obramat: %s\n", Clean(env.Message))
	if env.IssueURL != "" {
		fmt.Fprintf(w, "\nthe site may have changed. Please report it (the form is prefilled):\n  %s\nrerun with --debug to see the requests.\n", env.IssueURL)
	} else if len(env.SuggestedCommands) > 0 {
		fmt.Fprintf(w, "  try: %s\n", strings.Join(env.SuggestedCommands, "  |  "))
	}
	return env.Code
}

// ColorEnabled follows --no-color, NO_COLOR, TERM=dumb and whether f is a TTY.
func ColorEnabled(noColor bool, f *os.File) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(f)
}

func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
