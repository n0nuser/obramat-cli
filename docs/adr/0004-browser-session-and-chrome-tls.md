# 0004. Replay the browser session over a Chrome TLS fingerprint

Date: 2026-10-19
Status: accepted
Supersedes: [0003](0003-browser-user-agent.md)

## Context

[ADR 0003](0003-browser-user-agent.md) got by with a browser User-Agent and Go's own TLS client. That
held for the discovery sweeps, then stopped: after a burst of requests, DataDome refused every request
from the same address with a 403 challenge page, `curl` as well as the CLI, for the rest of the session
(recorded in `docs/review-2026-10-19.md`, R1 and R15). The site was reachable, the address and the
handshake were the tell.

DataDome scores several signals together: the TLS ClientHello, the `datadome` cookie, the User-Agent,
and the `Sec-CH-UA*` hints. Matching only the User-Agent leaves the pair inconsistent: a real session
cookie sent over Go's distinctive hello, with no browser around it to have earned it.

Options:

1. **Solve the challenge.** A stealth browser or a captcha-solving service. An explicit bypass and an
   arms race. Rejected.
2. **Drive a real browser from the CLI** (Chrome DevTools Protocol). Works, but heavy, slow and needs a
   display.
3. **Replay the user's own browser session.** The person browses obramat.es in Chrome, picks a store,
   and exports the cookies (or a HAR). The CLI sends those cookies, that browser's user agent, and
   presents Chrome's TLS hello.

## Decision

Option 3, and the hello that makes it consistent.

- **Session import.** `auth login --cookies FILE | --har FILE | --cookies-stdin [--cookies-format ...]`
  parses Cookie-Editor / EditThisCookie JSON (array or `{"url","cookies"}` wrapper), DevTools cookie
  lists, Netscape `cookies.txt`, a raw `Cookie:` header and HAR 1.2. Only `obramat.es` cookies are kept,
  so a whole-browser export is safe. The session (cookies, user agent, `Sec-CH-UA*` hints from a HAR) is
  stored 0600 at `$XDG_CONFIG_HOME/obramat-cli/session.json`, written atomically. `OBRAMAT_COOKIEJAR=FILE`
  reads an export every run without storing it, and the CLI refuses to write an env-sourced session to
  disk. Cookies the site rotates (DataDome rotates on most responses) are written back for file
  sessions. `auth status` lists cookie names only; values are never printed. `auth logout` forgets it.
- **Chrome TLS fingerprint.** Requests go through `github.com/refraction-networking/utls` with
  `HelloChrome_Auto`, isolated in `internal/api/transport.go` behind `ChromeTransport()`. ALPN is pinned
  to `http/1.1` so a stock `net/http.Transport` can carry the connection. JA3 hashes extension types, not
  ALPN values, so the JA3 stays Chrome's; JA4 encodes the first ALPN value (`h1` instead of Chrome's
  `h2`). If that ever matters, the fix is HTTP/2 over the same dialer and nothing outside `transport.go`
  moves.
- **The pair matches.** With a session, the browser's User-Agent is sent verbatim (the CLI's own name is
  appended only to the default User-Agent used when there is no session). The store context the command
  asks for wins over any `customer_context` the export carries, so `--store` still decides the store.
- **The store cookie is unchanged.** `customer_context` is still built per request
  ([ADR 0001](0001-store-context-cookie.md)); the session is what rides beside it.
- **Everything else keeps [ADR 0003](0003-browser-user-agent.md)'s manners.** Requests are spaced 400 ms
  apart, 1.5 s in sweeps; a DataDome 403 or a 429 is exit 5 and starts the 10-minute pause.

## Consequences

- **No account, still read-only.** This is not a login: no password is ever asked for and none of the
  account, cart or order commands arrive. [ADR 0002](0002-read-only.md) stands, except that exit code 3
  is now produced: `auth status --check` with no session exits 3 and says how to import one. The other
  commands run without a session and let the site answer.
- **Re-importing.** When DataDome stops trusting the session, every site command exits 5 until the user
  reloads the site in the same browser and imports the session again. `auth status --check` and
  `auth login` (which checks by default) tell a working session from a challenged one.
- **Fingerprint drift.** Chrome changes its hello every few releases; updating utls is part of keeping
  the CLI working.
- **Not a solver.** utls does not run JavaScript and cannot pass a challenge. It only avoids presenting
  a Go-shaped handshake beside a browser's cookie.
- **Dependency policy.** utls is the only non-stdlib piece of the HTTP stack. The updater (GitHub) is not
  the storefront and keeps plain `net/http`.
- **Testing.** Tests use a local TLS server for the handshake (`transport_test.go`) and hand-written
  `*.synthetic.*` cookie fixtures (`session_test.go`); real exports carry live tokens and are never
  committed. No test talks to obramat.es.
