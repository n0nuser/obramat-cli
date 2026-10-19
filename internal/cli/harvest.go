package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/harvest"
	"github.com/n0nuser/obramat-cli/internal/output"
)

// chromeURLEnv and chromePathEnv point the harvest at a browser without a flag.
const (
	chromeURLEnv  = "OBRAMAT_CHROME_URL"
	chromePathEnv = "OBRAMAT_CHROME"
)

type harvestFlags struct {
	browserURL string
	chrome     string
	userAgent  string
	timeout    time.Duration
}

func (a *App) harvestCmd() *cobra.Command {
	var f harvestFlags
	cmd := &cobra.Command{
		Use:   "harvest",
		Short: "Read the session out of a real Chrome window",
		Long: `Open obramat.es in a real Chrome and read the session it earns, instead of
exporting cookies by hand.

The browser opens headed so you can clear any DataDome check it shows; the
window stays open while the command waits. If a Chrome debug endpoint is
already running (--browser-url, OBRAMAT_CHROME_URL, or the default
http://localhost:9222), the command attaches to it instead of launching.

A launched browser keeps its profile under the data directory, so the session
survives across runs. The harvested cookies and the browser's own user agent
are stored 0600 exactly as ` + "`auth login`" + ` stores an export.

Examples:
  obramat auth harvest
  obramat auth harvest --chrome /usr/bin/chromium --timeout 2m
  obramat auth harvest --browser-url http://localhost:9222`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if os.Getenv(config.CookieJarEnv) != "" {
				return output.Usagef("%s is set, so a stored session would be ignored. Unset it to harvest", config.CookieJarEnv)
			}
			ctx := cmd.Context()
			browserURL, chromePath := resolveChromeEnv(f.browserURL, f.chrome)
			opts := harvest.Options{
				BrowserURL:  browserURL,
				Chrome:      chromePath,
				UserDataDir: a.Paths.ChromeDir,
				UserAgent:   f.userAgent,
				Timeout:     f.timeout,
			}
			if opts.BrowserURL == "" && harvest.DebugURLReachable(ctx, harvest.DefaultDebugURL, nil) {
				opts.BrowserURL = harvest.DefaultDebugURL
				a.notice("attaching to the Chrome already listening on %s", opts.BrowserURL)
			}
			res, err := harvest.Run(ctx, opts)
			if err != nil {
				return err
			}
			ua := res.UserAgent
			if f.userAgent != "" {
				ua = f.userAgent
			}
			raw, err := harvest.CookiesToExport(res.Cookies, ua)
			if err != nil {
				return err
			}
			imported, err := api.ParseExport(raw, api.FormatJSON)
			if err != nil {
				return err
			}
			s := &config.StoredSession{Session: imported.Session, ImportedAt: time.Now().UTC().Truncate(time.Second), ImportFormat: "chrome"}
			if err := config.SaveSession(a.Paths, s); err != nil {
				return err
			}
			a.Session = s
			a.Client.Session = &s.Session
			out := harvestResult{Path: s.Path, Format: s.ImportFormat, Skipped: imported.Skipped, Session: s.Summarize()}
			if ref, ok := storeFromSession(&s.Session); ok {
				out.Store = &ref
			}
			return a.Printer.Print(out)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.browserURL, "browser-url", "", "attach to this Chrome DevTools endpoint instead of launching (env "+chromeURLEnv+"; default: probe "+harvest.DefaultDebugURL+")")
	fl.StringVar(&f.chrome, "chrome", "", "Chrome or Chromium binary (env "+chromePathEnv+")")
	fl.StringVar(&f.userAgent, "user-agent", "", "override the user agent (default: the browser's own)")
	fl.DurationVar(&f.timeout, "timeout", harvest.DefaultTimeout, "how long to wait for the challenge to clear")
	return cmd
}

// storeFromSession reads the store the browser had selected out of the
// customer_context cookie, when the harvest brought one.
func storeFromSession(s *api.Session) (api.StoreRef, bool) {
	c, ok := s.Cookie("customer_context")
	if !ok {
		return api.StoreRef{}, false
	}
	return api.StoreFromCookie(c.Value)
}

type harvestResult struct {
	Path    string        `json:"path"`
	Format  string        `json:"import_format"`
	Skipped int           `json:"skipped_other_domains"`
	Session api.Summary   `json:"session"`
	Store   *api.StoreRef `json:"store,omitempty"`
}

func (r harvestResult) Pretty(w io.Writer, color bool) {
	fmt.Fprintf(w, "harvested %d obramat.es cookies (%s) into %s\n", r.Session.Cookies, r.Format, r.Path)
	if r.Skipped > 0 {
		fmt.Fprintln(w, output.Dim(fmt.Sprintf("skipped %d cookies for other sites", r.Skipped), color))
	}
	if r.Store != nil {
		fmt.Fprintf(w, "store %s (%d)\n", r.Store.Name, r.Store.ID)
	}
	if !r.Session.HasDataDome {
		fmt.Fprintln(w, "warning: no datadome cookie; the site will likely challenge the cli")
	}
	if r.Session.UserAgent != "" {
		fmt.Fprintln(w, output.Dim("user agent "+r.Session.UserAgent, color))
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveChromeEnv picks the DevTools endpoint and the browser binary: the
// flag wins, then the environment.
func resolveChromeEnv(browserURL, chrome string) (string, string) {
	return firstNonEmpty(browserURL, os.Getenv(chromeURLEnv)), firstNonEmpty(chrome, os.Getenv(chromePathEnv))
}
