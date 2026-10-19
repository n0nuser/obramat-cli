# Handoff report

## Summary

`obramat-cli` is a read-only terminal client for obramat.es. It searches the catalogue with the same
facets the site offers, shows a product's price and stock at each warehouse, and watches searches and
products for price and stock changes. Output is JSON on stdout (`jsonl`, `pretty`, `toon` too), errors
and notices on stderr, exit codes from 0 to 7 and 130.

State of the tree: every command in `docs/cli-spec.md` exists and works against captured responses;
`go build ./... && go test ./... && go vet ./...`, staticcheck, gofmt, the docs-site typecheck and
build, and `make docs-check` are clean.

The last two steps are done and now confirmed live:

1. **Getting past DataDome.** Requests go over a Chrome TLS hello (`utls`) and the CLI can replay the
   user's own browser session (`auth login`). With a fresh session the storefront answers (see
   [Literal output](#literal-output) below).
2. **`--in-stock` uses the site's filter.** The live request sent
   `filters={"availability":"inStockInStore"}` and the site returned the store's in-stock products, 200
   rather than the challenge page. The page was captured as a fixture.

## Commands and files

Commands: `obramat auth login|status|logout`, plus the existing catalogue, store, watch, sink, mcp,
config, doctor, skills and update commands.

Key files: `internal/api/transport.go` (Chrome TLS), `internal/api/session.go` (session, export parsers,
cookie jar), `internal/config/session.go` (0600 storage, `OBRAMAT_COOKIEJAR`), `internal/cli/auth.go`.
The `--in-stock` filter is in `internal/api/listing.go` (`ListParams.InStock`) and
`internal/cli/search.go`.

## Dependency

`github.com/refraction-networking/utls v1.8.2` (`HelloChrome_Auto`, Chrome 133). It is the only
non-stdlib piece of the HTTP stack; the updater still uses plain `net/http`.

## Tests

67 test functions. New since the start of this work: the export parsers over four synthetic
cookie/HAR fixtures, cookie host/path/expiry matching, that the `datadome` cookie and session user agent
both go out and a rotated cookie is absorbed, that `--store` beats a session `customer_context`, the
Chrome handshake negotiating HTTP/1.1 while still verifying certificates, 0600 storage and the
env-never-saved rule, the CLI-level `auth` flow, and the live `--in-stock` page
(`api.TestInStockListing`) beside the encoding (`api.TestInStockFilterParam`). No committed test talks
to obramat.es.

## Open caveats

1. **Sessions expire.** The replayed session is the user's own; when DataDome stops trusting it, every
   site command exits 5 until it is re-imported (`obramat auth login`). The 10-minute local pause after
   a block still applies, and `auth status --check` tells a working session from a challenged one.
2. **robots.txt** disallows `/search?q=*`, the filter and sort parameters, `/lazy/` and `/rest/`. The
   CLI fetches what one browser page view fetches, on a person's request; this is worth knowing before
   anything bulk is built on it.
3. **No store, no stock.** Without a store in context the site shows a default web price that for some
   products is a Canary Islands (IGIC) price, and no stock numbers.
4. The `L`-severity notes in `docs/review-2026-10-19.md` stand (brand case, category accents,
   quantity-price tiers).

R15 (the live `--in-stock` round trip) is closed; the details are in `docs/review-2026-10-19.md`.

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
