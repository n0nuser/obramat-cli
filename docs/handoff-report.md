# Handoff report

## Summary

`obramat-cli` is a read-only terminal client for obramat.es. It searches the catalogue with the same
facets the site offers, shows a product's price and stock at each warehouse, and watches searches and
products for price and stock changes. Output is JSON on stdout (`jsonl`, `pretty`, `toon` too), errors
and notices on stderr, exit codes from 0 to 7 and 130.

State of the tree: every command in `docs/cli-spec.md` exists and works against captured responses;
`go build ./... && go test ./... && go vet ./...`, staticcheck, gofmt, the docs-site typecheck and
build, and `make docs-check` are clean.

The last three steps are done and now confirmed live:

1. **Getting past DataDome.** Requests go over a Chrome TLS hello (`utls`) and the CLI can replay the
   user's own browser session (`auth login`). With a fresh session the storefront answers (see
   [Literal output](#literal-output) below).
2. **`--in-stock` uses the site's filter.** The live request sent
   `filters={"availability":"inStockInStore"}` and the site returned the store's in-stock products, 200
   rather than the challenge page. The page was captured as a fixture.
3. **`auth harvest` reads the session out of the user's real Chrome**, so the manual export is no
   longer needed for most users. It copies the trusted cookie store out of the everyday profile into a
   temp profile (Chrome 136+ ignores `--remote-debugging-port` on the default profile, so it cannot be
   an attach), starts Chrome on the copy, reads the session, and deletes the copy. Manual
   `auth login --cookies` stays for scripts, CI and machines without a display.

## Commands and files

Commands: `obramat auth harvest|login|status|logout`, plus the existing catalogue, store, watch, sink,
mcp, config, doctor, skills and update commands.

Key files: `internal/api/transport.go` (Chrome TLS), `internal/api/session.go` (session, export parsers,
cookie jar, `StoreFromCookie`), `internal/config/session.go` (0600 storage, `OBRAMAT_COOKIEJAR`),
`internal/cli/auth.go` and `internal/cli/harvest.go` (the `auth` noun),
`internal/harvest/` (the Chrome/CDP side, integration only). The `--in-stock` filter is in
`internal/api/listing.go` (`ListParams.InStock`) and `internal/cli/search.go`.

## Dependency

`github.com/refraction-networking/utls v1.8.2` (`HelloChrome_Auto`, Chrome 133) for the TLS hello, and
`github.com/chromedp/chromedp v0.21.0` for the harvest's DevTools protocol. No browser is bundled or
downloaded: the harvest drives the system Chrome/Chromium. The updater still uses plain `net/http`.

## Tests

93 test functions. New since the start of this work: the export parsers over four synthetic
cookie/HAR fixtures, cookie host/path/expiry matching, that the `datadome` cookie and session user agent
both go out and a rotated cookie is absorbed, that `--store` beats a session `customer_context`, the
Chrome handshake negotiating HTTP/1.1 while still verifying certificates, 0600 storage and the
env-never-saved rule, the CLI-level `auth` flow, the live `--in-stock` page (`api.TestInStockListing`)
beside the encoding (`api.TestInStockFilterParam`), and for the harvest: the cookie-to-export mapping
through `api.ParseExport`, the browser lookup and override, the DevTools reachability probe, profile
auto-detection order, the copy plan (only the cookie store plus key reference, 0700/0600) and its
exactness, free-port selection, the CDP URL normalisation, and that cleanup removes the temp dir and
stops the process. No committed test launches a browser or talks to obramat.es.

## Open caveats

1. **`auth harvest` needs a display** for a launched browser (it is headed on purpose, so a human can
   clear the challenge). On a headless machine, attach to a running Chrome with `--browser-url`, or use
   `auth login --cookies` / `OBRAMAT_COOKIEJAR`.
2. **The harvest copies the real profile's cookie store** into `harvest-*` under the data dir and deletes
   it on exit, including error paths. It needs the user's keyring/Keychain to decrypt the copy (the
   copied `Local State` carries the reference); if the browser cannot start on the copy, the command
   fails with that guidance. Chrome 136+ only opens a devtools port for a non-default profile, which is
   why it copies instead of attaching.
3. **Sessions expire.** The replayed session is the user's own; when DataDome stops trusting it, run
   `auth harvest` again or re-import. Every site command exits 5 until then. The 10-minute local pause
   after a block still applies, and `auth status --check` tells a working session from a challenged one.
4. **robots.txt** disallows `/search?q=*`, the filter and sort parameters, `/lazy/` and `/rest/`. The
   CLI fetches what one browser page view fetches, on a person's request; this is worth knowing before
   anything bulk is built on it.
5. **No store, no stock.** Without a store in context the site shows a default web price that for some
   products is a Canary Islands (IGIC) price, and no stock numbers.
6. The `L`-severity notes in `docs/review-2026-10-19.md` stand (brand case, category accents,
   quantity-price tiers).

R15 (the live `--in-stock` round trip) is closed; the details are in `docs/review-2026-10-19.md`.
Decisions: [ADR 0004](adr/0004-browser-session-and-chrome-tls.md) and
[ADR 0005](adr/0005-browser-session-harvest.md).

## Literal output

### `obramat auth login --cookies /tmp/opencode/obramat_cookies.json --user-agent "<Chrome 155 desktop UA>"`

```
imported 2 obramat.es cookies (json) into /home/pablo/.config/obramat-cli/session.json
session works: the storefront answered
```

### `obramat search "tablero osb" --store 22 --in-stock --format pretty`

```
obramat: > GET https://www.obramat.es/search?filters=%7B%22availability%22%3A%22inStockInStore%22%7D&q=tablero+osb
obramat: < 200 https://www.obramat.es/search?filters=%7B%22availability%22%3A%22inStockInStore%22%7D&q=tablero+osb (596586 bytes)
REF       PRICE    UNIT    STOCK  PRODUCT                                     URL
25064668  30.50 €  cajas   64     TABLERO OSB3 250X125X1,5CM                  https://www.obramat.es/productos/tablero-osb3-250x125x1-5cm-25064668.html
10952886  13.85 €  Unidad  178    TABLERO OSB 3 MACHIHEMBRADO 205X62,5X1,5CM  https://www.obramat.es/productos/tablero-osb-3-machihembrado-205x62-5x1-5cm-10952886.html
25105480  17.80 €  Unidad  10     TABLERO OSB 3 250X125X0,9CM                 https://www.obramat.es/productos/tablero-osb-3-250x125x0-9cm-25105480.html
25094563  6.95 €   Unidad  257    TABLERO OSB3 165X61X0,9CM                   https://www.obramat.es/productos/tablero-osb3-165x61x0-9cm-25094563.html
4 of 4 products, page 1/1, store Obramat Salamanca (22)
```

### `obramat auth harvest --help`

```
Read the obramat.es session straight out of your real Chrome, instead of
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
as `auth login` stores an export.

Examples:
  obramat auth harvest
  obramat auth harvest --profile ~/.config/google-chrome --timeout 2m
  obramat auth harvest --chrome /usr/bin/chromium
  obramat auth harvest --browser-url http://127.0.0.1:9222

Usage:
  obramat auth harvest [flags]

Flags:
      --browser-url string   attach to this Chrome DevTools endpoint instead of launching (env OBRAMAT_CHROME_URL; default: probe http://localhost:9222)
      --chrome string        Chrome or Chromium binary (env OBRAMAT_CHROME)
  -h, --help                 help for harvest
      --profile string       real Chrome profile to copy the cookies from (env OBRAMAT_CHROME_PROFILE; default: the usual Chrome/Chromium locations)
      --timeout duration     how long to wait for the challenge to clear (default 5m0s)
      --user-agent string    override the user agent (default: the browser's own)
```

`auth harvest` itself was not run here: it opens a visible browser window, so it is a
human-in-the-loop step. Everything it does after reading the cookies goes through the same
`api.ParseExport`/`config.SaveSession` path the outputs above exercised.

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
