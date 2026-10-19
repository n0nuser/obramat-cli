# 0005. Harvest the session from the user's real Chrome profile

Date: 2026-10-19
Status: accepted

## Context

[ADR 0004](0004-browser-session-and-chrome-tls.md) gets past DataDome by replaying the user's browser
session. Importing it by hand (`auth login --cookies FILE`) works but is friction: install an export
extension (or record a HAR), find the file and pass it, every time the session expires.

`auth harvest` first opened a **fresh** profile of its own. That does not work: a brand-new Chrome has
no trusted `datadome` cookie, so DataDome challenges it immediately and a person has to solve a captcha
every run. The trusted cookie lives in the user's everyday profile.

Attaching to the everyday profile is not an option either: **Chrome 136 and up ignores
`--remote-debugging-port` on the default profile**, so a running, normally-started Chrome exposes no
DevTools endpoint to attach to. (Verified on this machine, Chrome 155.)

## Decision

Copy the trusted cookie out of the user's real profile into a temp profile the CLI starts a browser
on, then harvest over CDP.

- **Find the profile.** `--profile` / `OBRAMAT_CHROME_PROFILE`, else auto-detect in order:
  `~/.config/google-chrome`, `~/.config/chromium` (on Linux; the macOS and Windows locations on those
  systems, `$XDG_CONFIG_HOME` honoured). A directory counts when it holds `Default/Cookies` or
  `Default/Network/Cookies`.
- **Copy only what is needed.** `Local State` (the os_crypt key reference), the cookie store
  (`Default/Cookies` and/or `Default/Network/Cookies`, plus `-journal`/`-wal`/`-shm`) and
  `Default/Preferences`. Nothing else — no history, no logins, no other sites. The copy lands in a temp
  directory **0700** under `$XDG_DATA_HOME/obramat-cli/` (the CLI data dir), files 0600.
- **Launch headed on the copy.** A temp *non-default* profile is exactly what makes
  `--remote-debugging-port` work again. The CLI picks a free loopback port, starts Chrome itself with
  `--user-data-dir=<temp> --remote-debugging-port=<n> --remote-debugging-address=127.0.0.1` (visible
  window, no `--headless`), and waits for the endpoint.
- **127.0.0.1 everywhere.** `localhost` can resolve to `[::1]` first, where nothing is bound; a
  `--browser-url` of `localhost` is rewritten to `127.0.0.1`.
- **Clean up, always.** A `defer` kills the temp Chrome process and `os.RemoveAll`s the temp profile on
  every path, error paths included. The directory holds a copy of the user's cookies, so this is not
  optional.
- **Fallbacks.** `--browser-url` still attaches to a Chrome someone started with a devtools port. When
  no real profile is found, a fresh window opens as before (it may need a human captcha). Where the
  copied profile cannot start — the keyring is not shared, or another Chrome holds the profile — the
  command fails with that guidance rather than silently presenting a challengeable window.
- The harvested cookies and `navigator.userAgent` go through `api.ParseExport` and
  `config.SaveSession`, the same path as `auth login`; storage is not reimplemented.
- DevTools comes from `github.com/chromedp/chromedp` (pure Go). No browser is bundled or downloaded.

## Consequences

- **The real profile is only read.** The copy is read-only on the source; the browser runs on the copy,
  so the user's own profile is untouched and no lock is taken.
- **Cross-platform.** Decrypting the copied cookies relies on the OS keyring/Keychain being available
  to the same user, which the copy preserves by carrying `Local State`. If it is not, the launch fails
  with guidance; `auth login` still works.
- **Temp data.** `harvest-*` directories appear under the data dir only for the life of the command.
- **Not a bypass.** The browser is a real browser and the cookie is the user's own; a human still
  clears a captcha when DataDome challenges.
- **Manual `auth login` stays**, for scripts, CI (`OBRAMAT_COOKIEJAR`) and machines without a display.
- **Dependency policy.** `chromedp` for the DevTools protocol, `utls` for the TLS hello, `net/http` for
  the rest. No test launches a browser: detection, the copy plan, the port and the cleanup are
  unit-tested; the browser step is integration only.
