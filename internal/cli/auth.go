package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/output"
)

func (a *App) authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Import, inspect or forget the browser session",
		Long: `Import, inspect or forget the browser session the cli replays.

obramat.es sits behind bot protection that answers scripts with a challenge
page. The cli gets through by replaying your own browser's cookies (and its
user agent) over a Chrome-like TLS handshake. No password is ever asked for,
and an account is not needed: a guest session with a store selected is
enough.`,
	}
	cmd.AddCommand(a.loginCmd(), a.harvestCmd(), a.statusCmd(), a.logoutCmd())
	return cmd
}

type loginResult struct {
	Path       string      `json:"path"`
	Format     string      `json:"format"`
	Skipped    int         `json:"skipped_other_domains"`
	Session    api.Summary `json:"session"`
	Checked    bool        `json:"checked"`
	Valid      *bool       `json:"session_valid,omitempty"`
	CheckError string      `json:"check_error,omitempty"`
}

func (r loginResult) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "imported %d obramat.es cookies (%s) into %s\n", r.Session.Cookies, r.Format, r.Path)
	if r.Skipped > 0 {
		fmt.Fprintln(w, output.Dim(fmt.Sprintf("skipped %d cookies for other sites", r.Skipped), color))
	}
	if !r.Session.HasDataDome {
		fmt.Fprintln(w, "warning: no datadome cookie; the site will likely challenge the cli")
	}
	switch {
	case !r.Checked:
		fmt.Fprintln(w, output.Dim("not checked (--no-check)", color))
	case r.Valid != nil && *r.Valid:
		fmt.Fprintln(w, "session works: the storefront answered")
	default:
		fmt.Fprintf(w, "session saved, but the storefront refused it: %s\n", r.CheckError)
	}
}

func (a *App) loginCmd() *cobra.Command {
	var cookies, har, format, userAgent string
	var fromStdin, noCheck bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Import obramat.es cookies or a HAR from your browser",
		Long: `Import obramat.es cookies or a HAR exported from your browser.

1. Open obramat.es in Chrome and choose your store.
2. Export the cookies with a cookie-export extension (JSON or cookies.txt),
   or save a HAR from DevTools > Network with "Allow to generate HAR with
   sensitive data" enabled.
3. Pass the file here.

Accepted: cookie JSON (array or {"url","cookies"} wrapper), DevTools cookie
lists, Netscape cookies.txt, a Cookie: header, HAR 1.2. Only obramat.es
cookies are kept, so a whole-browser export is fine. A HAR also carries your
browser's user agent, which the cli then sends.

The session is stored 0600 under the config directory. For CI, point
OBRAMAT_COOKIEJAR at an export instead: it is read on each run and never
written to disk.

Examples:
  obramat auth login --cookies ~/Downloads/obramat-cookies.json
  obramat auth login --har ~/Downloads/obramat.har --no-check
  pbpaste | obramat auth login --cookies-stdin --cookies-format header`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv(config.CookieJarEnv) != "" {
				return output.Usagef("%s is set, so a stored session would be ignored. Unset it to log in", config.CookieJarEnv)
			}
			raw, fmtHint, err := a.readExport(cookies, har, fromStdin, format)
			if err != nil {
				return err
			}
			res, err := api.ParseExport(raw, fmtHint)
			if err != nil {
				return err
			}
			if userAgent != "" {
				res.Session.UserAgent = userAgent
			}
			s := &config.StoredSession{Session: res.Session, ImportedAt: time.Now().UTC().Truncate(time.Second), ImportFormat: res.Format}
			if err := config.SaveSession(a.Paths, s); err != nil {
				return err
			}
			a.Session = s
			a.Client.Session = &s.Session
			out := loginResult{Path: s.Path, Format: res.Format, Skipped: res.Skipped, Session: s.Summarize()}
			if !noCheck {
				out.Checked = true
				ok, err := a.checkStorefront(cmd.Context())
				out.Valid = &ok
				if err != nil {
					out.CheckError = err.Error()
				}
			}
			return a.Printer.Print(out)
		},
	}
	f := cmd.Flags()
	f.StringVar(&cookies, "cookies", "", "cookie export file (JSON, cookies.txt or a Cookie: header)")
	f.StringVar(&har, "har", "", "HAR file saved from DevTools with sensitive data included")
	f.BoolVar(&fromStdin, "cookies-stdin", false, "read the export from stdin")
	f.StringVar(&format, "cookies-format", "auto", "auto, json, netscape, header or har")
	f.StringVar(&userAgent, "user-agent", "", "user agent of the browser the cookies came from (a HAR provides it)")
	f.BoolVar(&noCheck, "no-check", false, "save without asking the storefront whether the session works")
	cmd.MarkFlagsMutuallyExclusive("cookies", "har", "cookies-stdin")
	_ = cmd.RegisterFlagCompletionFunc("cookies-format", cobra.FixedCompletions([]string{"auto", "json", "netscape", "header", "har"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// readExport returns the raw export from the flag that was given, or from a
// prompt when interactive. Secrets never come from argv.
func (a *App) readExport(cookies, har string, fromStdin bool, format string) ([]byte, string, error) {
	switch {
	case har != "":
		raw, err := os.ReadFile(config.ExpandHome(har))
		if format == "auto" {
			format = api.FormatHAR
		}
		return raw, format, err
	case cookies != "":
		raw, err := os.ReadFile(config.ExpandHome(cookies))
		return raw, format, err
	case fromStdin:
		raw, err := io.ReadAll(io.LimitReader(a.Stdin, 64<<20))
		return raw, format, err
	case !a.Interactive:
		return nil, "", output.Usagef("pass --cookies FILE, --har FILE or --cookies-stdin (no terminal to prompt on)")
	}
	fmt.Fprint(a.Stderr, "path to an obramat.es cookie export or HAR: ")
	line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	path := config.ExpandHome(strings.Trim(strings.TrimSpace(line), `"'`))
	if path == "" {
		return nil, "", output.Usagef("no file given")
	}
	raw, err := os.ReadFile(path)
	return raw, format, err
}

// checkStorefront asks for the home page with the session. A challenge means
// the session is not (or no longer) trusted.
func (a *App) checkStorefront(ctx context.Context) (bool, error) {
	if err := a.Client.Reachable(ctx); err != nil {
		return false, err
	}
	return true, nil
}

type statusResult struct {
	Source     string       `json:"source"`
	Path       string       `json:"path,omitempty"`
	ImportedAt *time.Time   `json:"imported_at,omitempty"`
	Format     string       `json:"import_format,omitempty"`
	Session    *api.Summary `json:"session,omitempty"`
	Checked    bool         `json:"checked"`
	Valid      *bool        `json:"session_valid,omitempty"`
	CheckError string       `json:"check_error,omitempty"`
}

func (r statusResult) Pretty(w io.Writer, color bool) {
	if r.Session == nil {
		fmt.Fprintln(w, "no session: run `obramat auth login --cookies FILE`")
		return
	}
	rows := [][]string{
		{"source", r.Source + " " + r.Path},
		{"cookies", strings.Join(r.Session.Names, ", ")},
		{"datadome", fmt.Sprint(r.Session.HasDataDome)},
	}
	if r.ImportedAt != nil {
		rows = append(rows, []string{"imported", r.ImportedAt.Format(time.RFC3339)})
	}
	if r.Session.UserAgent != "" {
		rows = append(rows, []string{"user agent", r.Session.UserAgent})
	}
	if r.Checked {
		v := "works"
		if r.Valid == nil || !*r.Valid {
			v = "refused: " + r.CheckError
		}
		rows = append(rows, []string{"storefront", v})
	}
	output.Table(w, color, nil, rows)
}

func (a *App) statusCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the session in use (cookie names only, never values)",
		Long: `Show where the session comes from and what it carries. Values are never
printed. With --check it asks the storefront whether the session still works:
one request, and the answer the site gives a challenged session.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := statusResult{Source: "none"}
			if s := a.Session; s != nil {
				sum := s.Summarize()
				out.Source, out.Path, out.Session, out.Format = s.Source, s.Path, &sum, s.ImportFormat
				if !s.ImportedAt.IsZero() {
					out.ImportedAt = &s.ImportedAt
				}
			}
			if check {
				if err := a.requireSession(); err != nil {
					return err
				}
				out.Checked = true
				ok, err := a.checkStorefront(cmd.Context())
				out.Valid = &ok
				if err != nil {
					out.CheckError = err.Error()
				}
			}
			return a.Printer.Print(out)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "also ask the storefront whether the session works (one request)")
	return cmd
}

func (a *App) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the stored browser session",
		Long:  "Delete the stored session. Your browser session is not touched.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := config.RemoveSession(a.Paths)
			if err != nil {
				return err
			}
			a.Session = nil
			return a.Printer.Print(map[string]any{"removed": removed})
		},
	}
}
