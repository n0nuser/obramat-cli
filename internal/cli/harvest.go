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
	chromeURLEnv     = "OBRAMAT_CHROME_URL"
	chromePathEnv    = "OBRAMAT_CHROME"
	chromeProfileEnv = "OBRAMAT_CHROME_PROFILE"
)

type harvestFlags struct {
	browserURL string
	chrome     string
	profile    string
	userAgent  string
	timeout    time.Duration
}

func (a *App) harvestCmd() *cobra.Command {
	var f harvestFlags
	cmd := &cobra.Command{
		Use:   "harvest",
		Short: "Read the session out of a real Chrome window",
		Long: `Read the obramat.es session straight out of your real Chrome, instead of
exporting cookies by hand.

The trusted datadome cookie lives in your everyday profile, not in a fresh one,
so the command copies just that profile's cookie store and keyfile into a temp
profile (0700, deleted afterwards) and opens Chrome on it. Chrome 136 and up
ignores --remote-debugging-port on the default profile, which is why it is a
copy and not an attach. The window is headed so you can clear any check it
shows; the command waits up to --timeout for that.

The profile is --profile / OBRAMAT_CHROME_PROFILE when set, else the first of
the usual Chrome and Chromium locations that has cookies. Without one, or with
--browser-url, the behaviour changes:

  --browser-url URL   attach to a Chrome already listening on a devtools port
                      (env OBRAMAT_CHROME_URL, or the default
                      http://localhost:9222). Chrome 136+ only exposes that on
                      a profile started with it.
  no real profile     open a fresh window instead; it has no trusted cookie, so
                      a captcha may need clearing by hand.

The harvested cookies and the browser's own user agent are stored 0600 exactly
as ` + "`auth login`" + ` stores an export.

Examples:
  obramat auth harvest
  obramat auth harvest --profile ~/.config/google-chrome --timeout 2m
  obramat auth harvest --chrome /usr/bin/chromium
  obramat auth harvest --browser-url http://127.0.0.1:9222`,
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
				Profile:     resolveProfile(f.profile),
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
	fl.StringVar(&f.profile, "profile", "", "real Chrome profile to copy the cookies from (env "+chromeProfileEnv+"; default: the usual Chrome/Chromium locations)")
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

// resolveProfile picks the real Chrome profile to copy: the flag wins, then
// the environment, then the caller's auto-detection.
func resolveProfile(flag string) string { return firstNonEmpty(flag, os.Getenv(chromeProfileEnv)) }
