# 0005. Harvest the session from a real Chrome window

Date: 2026-10-19
Status: accepted

## Context

[ADR 0004](0004-browser-session-and-chrome-tls.md) gets past DataDome by replaying the user's browser
session, imported with `auth login --cookies FILE`. That works, but it asks the user to install a
cookie-export extension (or record a HAR), find the file and pass it, every time the session expires.
It is the wrong kind of friction for a command a person runs on their own machine.

The browser already holds the session. Chrome exposes it over the DevTools protocol, and the CLI can
drive a browser it starts itself.

Options:

1. **Drive the user's everyday Chrome profile** (the one they shop in). It already has the session. But
   taking it over means the CLI reads a profile full of the user's other cookies and tabs, and a
   running Chrome only exposes DevTools on a port it was started with.
2. **A dedicated Chrome profile the CLI owns.** It lives under the data directory, has no other
   cookies, and survives across runs so a solved challenge is not solved again every time.
3. **Keep only manual export.** No new dependency, but the friction stays.

## Decision

Option 2, with option 1 as a fallback.

- `auth harvest` launches Chrome (or Chromium, Brave, Edge) **headed** so a person can clear any
  DataDome challenge it shows, with a persistent user-data-dir at
  `$XDG_DATA_HOME/obramat-cli/chrome`, so the `datadome` and `customer_context` cookies survive across
  runs. The browser is found by name in `PATH` and the usual install locations, or named outright with
  `--chrome` / `OBRAMAT_CHROME`.
- **Attach instead of launch** when a DevTools endpoint is already up: `--browser-url`, then
  `OBRAMAT_CHROME_URL`, then a probe of `http://localhost:9222`. That is how a user who keeps Chrome
  open with `--remote-debugging-port` gets harvested without a second profile.
- It navigates to `https://www.obramat.es/` and polls every second, up to `--timeout` (default 5
  minutes), until the challenge is clear: the title is no longer the challenge's, the body no longer
  asks to enable JavaScript, no `captcha-delivery` frame is present, and a `datadome` cookie exists.
  It never tries to solve the challenge itself.
- It reads the cookies and `navigator.userAgent` over CDP, renders them as a Cookie-Editor-shaped JSON
  export with a `userAgent` field, and hands them to `api.ParseExport` and `config.SaveSession` — the
  same code `auth login` uses. Nothing about storage is reimplemented; the harvest is just another way
  to produce the export. `--user-agent` overrides the browser's.
- The DevTools protocol comes from `github.com/chromedp/chromedp` (pure Go). No browser is bundled or
  downloaded; the system browser is used.

## Consequences

- **A display is needed** for a launched browser, since it is headed on purpose. On Linux with no
  `DISPLAY`/`WAYLAND_DISPLAY` the launch fails with that error; attaching to a running browser still
  works.
- **The profile is a cookie store.** `~/.local/share/obramat-cli/chrome` holds the site session (and
  only what browsing obramat.es sets). It is meant to be long-lived; `auth logout` removes the stored
  session file, not the profile.
- **Not a bypass.** The browser is a real browser, and a human clears the challenge. When DataDome
  challenges again, the harvest waits rather than solving.
- **Manual `auth login` stays**, for scripts, CI (`OBRAMAT_COOKIEJAR`), and machines without a display.
- **Dependency policy.** `chromedp` is added for this; the TLS hello remains `utls`, the HTTPS client
  remains `net/http`. No test launches a browser: the browser step is integration only, and only the
  parsing and mapping are unit-tested.
