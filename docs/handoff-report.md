# Handoff report

## Summary

`obramat-cli` is a read-only terminal client for obramat.es. It searches the catalogue with the same
facets the site offers, shows a product's price and stock at each warehouse, and watches searches and
products for price and stock changes. Output is JSON on stdout (`jsonl`, `pretty`, `toon` too), errors
and notices on stderr, exit codes from 0 to 7 and 130.

State of the tree: every command in `docs/cli-spec.md` exists and works against captured responses;
`go build ./... && go test ./... && go vet ./...` and `make check` (staticcheck, gofmt) are clean, the
docs site typechecks and builds, and the generated command reference (`make docs-check`) is current.

This handoff lands on the last open finding of the adversarial review: `--in-stock` now uses the site's
own "En stock en Almacén" filter instead of filtering only on the CLI side (see caveats).

## Tests

47 test functions (46 before this handoff). Coverage is fixture round-trips over real captured responses
in `internal/api/testdata/`, the watch diff/state/sink layers, the MCP server, the docs generator, and a
binary-contract test with an isolated XDG home. No test touches obramat.es.

New here: `api.TestInStockFilterParam` pins the exact parameter `--in-stock` sends
(`filters={"availability":"inStockInStore"}`), and the docs that claimed the site offered no stock
filter are corrected.

## Commits

- `275a271` feat(search): send the site's own in-stock filter for --in-stock
- (this report and the review-record update) docs: handoff report and review record

Pushed to `feat/initial` only. No tag, no release.

## Open caveats

1. **Live confirmation of the new `--in-stock` filter is pending a DataDome block.** On 2026-10-19,
   about 45 minutes after the sweep that first tripped it, DataDome still refused every request from
   this address with the 773-byte JS-challenge page (`HTTP 403`). The filter encoding is not guesswork:
   the toggle is in every captured listing that had a store, with `name="availability"`
   `value="inStockInStore"`, and its markup id is built from the same two words. A live round trip and a
   captured fixture are still owed once the address is out of the block. The CLI's own block handling
   (`blocked-until` pause, exit 5) is in place, so a retry will not deepen it.
2. **robots.txt** disallows `/search?q=*`, the filter and sort parameters, `/lazy/` and `/rest/`. The CLI
   is not a crawler (one person's search or product view, at a low rate), but this is worth knowing
   before anything bulk is built on it.
3. **No store, no stock.** Without a store in context the site shows a default web price that for some
   products is a Canary Islands (IGIC) price, and no stock numbers. Store-dependent results need
   `--store`, `OBRAMAT_STORE` or `config set store`.
4. The four `L`-severity notes in `docs/review-2026-10-19.md` remain (brand case, category accents,
   quantity-price tiers, and the pending live check above).

## Literal output

### `obramat --help`

```
Search obramat.es and check prices and stock per store from the terminal.

Unofficial and read-only: no account, no cart, no orders.

Output is JSON on stdout so every command pipes into jq; --format pretty is
for reading. Errors and notices go to stderr. Prices and stock depend on the
store: set one with --store, OBRAMAT_STORE or `obramat config set store ID`.

Examples:
  obramat search "tablero osb" --format pretty
  obramat search cemento --brand WEBER --max-price 20 --sort price_asc --store 4
  obramat store near --lat 40.42 --lng -3.70 --format pretty
  obramat store stock 25064668 --all --format pretty
  obramat watch add product 25064668 --store 4 --name osb

Agent usage: obramat skills get obramat-usage

Usage:
  obramat [command]

Available Commands:
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

### `obramat search "tablero osb" --format pretty`

Could not be run: the site refuses this address (caveat 1). The last time the binary tried, it printed
exactly this and exited 5:

```
obramat: obramat.es bot protection refused the request (HTTP 403). Wait a few minutes before retrying, and keep the request rate low
  try: wait a few minutes, then retry once
```

### `obramat store stock 25064668 --all --format pretty`

Could not be run for the same reason. The command is unchanged from the reviewed build: it needs the
store list (`store list`, cached a week) and about fifteen store-search-result requests at the sweeps'
1.5 s spacing, and prints one row per store (`ID`, `STORE`, `STOCK`, `DISTANCE`, `SITE SAYS`).
