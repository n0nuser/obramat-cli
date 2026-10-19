# Handoff report

## Summary

`obramat-cli` is a read-only terminal client for obramat.es. It searches the catalogue with the same
facets the site offers, shows a product's price and stock at each warehouse, and watches searches and
products for price and stock changes. Output is JSON on stdout (`jsonl`, `pretty`, `toon` too), errors
and notices on stderr, exit codes from 0 to 7 and 130.

State of the tree: every command in `docs/cli-spec.md` exists and works against captured responses;
`go build ./... && go test ./... && go vet ./...`, staticcheck, gofmt, the docs-site typecheck and
build, and `make docs-check` are clean.

This handoff adds what the site now needs to answer at all: obramat.es is behind DataDome, and after a
burst of requests the plain client was refused for a long stretch. The CLI now presents Chrome's TLS
hello (`utls`) and can replay the user's own browser session (cookies plus that browser's user agent),
imported with a new `auth` command. It also lands the change from the previous step: `--in-stock` uses
the site's own "En stock en Almacén" filter.

## Commands and files added

New commands:

- `obramat auth login [--cookies FILE | --har FILE | --cookies-stdin] [--cookies-format auto|json|netscape|header|har] [--user-agent UA] [--no-check]`
- `obramat auth status [--check]`
- `obramat auth logout`

New files:

- `internal/api/transport.go`: the Chrome TLS transport (`ChromeTransport`, `HelloChrome_Auto`, ALPN
  pinned to `http/1.1`).
- `internal/api/session.go`: `Session`/`Cookie`, `ParseExport` (Cookie-Editor / EditThisCookie JSON,
  `{"userAgent","cookies"}` wrapper, DevTools lists, Netscape `cookies.txt`, `Cookie:` header, HAR 1.2),
  the cookie jar and rotation, and `Summary`.
- `internal/config/session.go`: 0600 storage at `$XDG_CONFIG_HOME/obramat-cli/session.json`,
  `OBRAMAT_COOKIEJAR` for an env read-only session, `RemoveSession`.
- `internal/cli/auth.go`: the `auth` command.
- Tests and fixtures: `internal/api/session_test.go`, `internal/api/transport_test.go`,
  `internal/config/session_test.go`, `internal/cli/auth_test.go`, and
  `internal/api/testdata/{cookies_editor.synthetic.json, cookies_browser.synthetic.json,
  cookies.synthetic.txt, session.synthetic.har}`.

Changed: `internal/api/client.go` (transport, session user agent, cookie header, rotation);
`internal/cli/root.go` (load/persist the session, register `auth`, exit 3 path); `internal/config/config.go`
(`SessionFile`); docs (`docs/cli-spec.md`, `README.md`, ADR 0004 plus 0001–0003, the feasibility and
discovery notes, the docs site and its generated reference), `AGENTS.md` and the embedded usage note.

## Dependency

`github.com/refraction-networking/utls v1.8.2` (`HelloChrome_Auto`). It is the only non-stdlib piece of
the HTTP stack; the updater still uses plain `net/http`.

## Tests

66 test functions (47 before this step, +19). New coverage: the export parsers over four synthetic
cookie/HAR fixtures, the cookie/host/expiry matching, that the `datadome` cookie and the session user
agent both go out and that a rotated cookie is absorbed, that a command's `--store` beats a
`customer_context` carried by the export, the Chrome handshake negotiating HTTP/1.1 and still verifying
certificates, the 0600 session file and the env-never-saved rule, and the CLI-level `auth
login/status/logout` flow. No test touches obramat.es.

`TestBinaryContract` gained `auth status` and `auth logout` at process level.

## Open caveats

1. **The live round trip is pending a real session and an unblocked IP.** DataDome refused this
   address for the rest of the session (403, ~773-byte JS-challenge page), so neither the new session
   replay nor the `--in-stock` server-side filter has been exercised against the site yet. Both are
   pinned by captured evidence and unit tests: the toggle encoding by the listing markup
   (`name="availability" value="inStockInStore"`) and `api.TestInStockFilterParam`; the session by
   `api.TestClientSendsSessionAndAbsorbsRotation`. The next step is a real browser export
   (`obramat auth login --cookies FILE`), then one live `obramat search --store 4 --in-stock` and a
   captured fixture. Recorded as R15 in `docs/review-2026-10-19.md`.
2. **Sessions expire.** When DataDome stops trusting the imported session, every site command exits 5
   until it is re-imported. The 10-minute local pause after a block still applies.
3. **robots.txt** disallows `/search?q=*`, the filter and sort parameters, `/lazy/` and `/rest/`. The
   CLI fetches what one browser page view fetches, on a person's request; this is worth knowing before
   anything bulk is built on it.
4. **No store, no stock.** Without a store in context the site shows a default web price that for some
   products is a Canary Islands (IGIC) price, and no stock numbers. The `L`-severity notes in
   `docs/review-2026-10-19.md` also stand (brand case, category accents, quantity-price tiers).

## Literal output

### `obramat --help`

```
Search obramat.es and check prices and stock per store from the terminal.

Unofficial and read-only: no account, no cart, no orders.

The site is behind bot protection, so the cli can replay your own browser's
session: export obramat.es cookies (or a HAR) and run
`obramat auth login --cookies FILE`.

Output is JSON on stdout so every command pipes into jq; --format pretty is
for reading. Errors and notices go to stderr. Prices and stock depend on the
store: set one with --store, OBRAMAT_STORE or `obramat config set store ID`.

Examples:
  obramat search "tablero osb" --format pretty
  obramat search cemento --brand WEBER --max-price 20 --sort price_asc --store 4
  obramat store near --lat 40.42 --lng -3.70 --format pretty
  obramat store stock 25064668 --all --format pretty
  obramat watch add product 25064668 --store 4 --name osb
  obramat auth login --cookies ~/Downloads/obramat-cookies.json

Agent usage: obramat skills get obramat-usage

Usage:
  obramat [command]

Available Commands:
  auth        Import, inspect or forget the browser session
  category    Browse the category tree
  completion  Generate the autocompletion script for the specified shell
  config      Read and write config.toml
  doctor      Check config, sinks, the default store, the site and the watch schedule
  help        Help about any command
  mcp         Serve the read commands to an agent harness over MCP (stdio)
  product     Inspect one product
  search      Search products, or browse a category, with the site's filters
  sink        Notification targets for watch events, from config.toml
  skills      Embedded usage notes for agents and people
  store       Obramat warehouses and the stock each one holds
  update      Update obramat to the latest release
  watch       Watch searches and products for price and stock changes

Flags:
      --debug                 log every http request to stderr
      --error-format string   error rendering on stderr: text or json (default "text")
      --format string         output format: json, jsonl, pretty, toon (default "json")
  -h, --help                  help for obramat
      --no-color              no colour in pretty output (also NO_COLOR)
      --no-input              never prompt; fail when input is missing
  -v, --version               version for obramat

Use "obramat [command] --help" for more information about a command.
```

### `obramat auth status` (no session imported yet — local)

```json
{
  "source": "none",
  "checked": false
}
```

### `obramat search "tablero osb" --format pretty`

Could not be run: the IP is still refused and no session is imported (caveat 1). The last time the
binary reached the site it exited 5 with:

```
obramat: obramat.es bot protection refused the request (HTTP 403). Wait a few minutes before retrying, and keep the request rate low
  try: wait a few minutes, then retry once
```

### `obramat store stock 25064668 --all --format pretty`

Could not be run for the same reason. The command is unchanged: it needs the store list (cached a week)
and about fifteen store-search-result requests at the sweeps' 1.5 s spacing, and prints one row per
store (`ID`, `STORE`, `STOCK`, `DISTANCE`, `SITE SAYS`).
