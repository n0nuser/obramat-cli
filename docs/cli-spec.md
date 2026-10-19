# obramat-cli spec

The interface contract of the `obramat` binary. Vocabulary in [`../AGENTS.md`](../AGENTS.md). Site facts
come from read-only probes on 2026-10-18/19 and are recorded in
[obramat-api-discovery.md](obramat-api-discovery.md); the site publishes no API and may change.

## 1. Name

Binary `obramat`. Module `github.com/n0nuser/obramat-cli`. Go 1.26, one static binary (`CGO_ENABLED=0`).

## 2. One-liner

Search obramat.es and check prices and stock per store from the terminal.

## 3. Usage

```
obramat [global flags] <noun> <verb> [args] [flags]
```

Noun-verb, except `search` (a verb on the catalogue) and `doctor`.

## 4. Command tree

```
search     [keywords...] | filters [keywords...]
product    show PRODUCT                (alias: item)
category   list
store      list | near | show STORE | stock PRODUCT
watch      add (search|product) | list | remove | check | run | events | service (install|uninstall|status)
sink       list | test
mcp        (no verb: serves) | install claude-code|codex|cursor
config     path | list | get | set
doctor
skills     list | get
update
completion bash|zsh|fish|powershell
```

No account commands: the CLI is read-only and the site's public pages carry everything it reads. No cart,
no orders, no login. See [ADR 0002](adr/0002-read-only.md).

### Arguments

- **PRODUCT**: a reference (the `Ref.` number on the product page, 6 to 10 digits, e.g. `25064668`), a
  product URL, or a `/productos/...html` path. A reference costs one extra request: the site redirects an
  exact-reference search to the product page.
- **STORE**: a store code (`4`), a slug (`obramat-leganes`) or a unique part of the slug (`leganes`).
  Codes are the numbers the site uses in its store cookie and stock drawer; `store list` shows them.

### Store context

Prices and stock belong to one store. Every command that shows them takes `--store STORE`; otherwise
`OBRAMAT_STORE`, then config `store`. `--store none` forces no store. Without a store the site shows its
default web price, which for some products is a Canary Islands price with IGIC instead of IVA, and no
stock quantities. With a store, listings contain only what that store sells. See
[ADR 0001](adr/0001-store-context-cookie.md).

### search

`search [keywords...] [--category PATH] [--brand B]... [--filter CODE=VALUE]... [--min-price N]
[--max-price N] [--sort relevance|price_asc|price_desc] [--store S] [--in-stock] [--page N] [--pages N]
[--limit N]`

- Keywords search; `--category PATH` browses a category instead. Both together is a usage error: the
  site's category pages ignore a query.
- `--brand` and `--filter` map to the site's facets (`filters` query parameter); `--min-price` and
  `--max-price` to its price range. `search filters` lists the facets, their codes and value counts for a
  search or category.
- `--in-stock` keeps products with stock at the store. The site offers no such filter, so the CLI filters
  the page it got; it requires a store.
- The site serves 48 products per page and at most 10 pages; `--page 11` is exit 4.
- JSON: `{query, category, store, page, pages, total, sort, products: [Product]}`. JSONL: one Product per
  line.

Product: `{ref, title, url, image, brand, price, store, stock, badges, delivery}` where
`price = {amount, amount_ex_vat, tax: "IVA"|"IGIC", unit, currency, per_measure: {amount, amount_ex_vat, unit}}`
or null, `store = {id, name}` or null, and `stock = {quantity, available, label}` present only with a store.
`quantity` is null when the site shows no number; without a store it is never reported, because the
site's own stock flag is then a 0/1 marker, not a count.

### product

`product show PRODUCT [--store S]`: Product plus `ean, description, category, breadcrumb,
attributes: [{name, value}], buyable`. If the store does not sell it, `stock.quantity` is 0 and `price`
is null.

### category

`category list [--parent PATH] [--depth N]` from the category sitemap: `{path, name, depth, parent,
children}`. Names are derived from the path; top-level paths are landing pages without a product list
(searching one is a usage error that names its subcategories command).

### store

- `store list [--refresh]`: every store `{id, name, slug, url, street, postcode, city, lat, lng, phone,
  email}`. Built from the store sitemap, the nearby-stores endpoint at fifteen points and, for any store
  those miss, its own page; cached for a week under `$XDG_CACHE_HOME/obramat-cli/stores.json`.
- `store near --lat --lng`: the six nearest stores, with `distance_km`, `open` and `hours`.
- `store show STORE`: one store, with opening state.
- `store stock PRODUCT`:
  - `--store S` (or the default store): that store's product page. Exact quantity.
  - `--lat --lng`: the site's "availability by store" drawer for the six stores nearest the point.
  - `--all`: the drawer around each store until every store has answered, about one request per three
    stores.

  JSON `{product, stores: [{store, slug, distance_km, stock}]}`.

### watch

Watches, checks, events and sinks are local (`$XDG_DATA_HOME/obramat-cli/state.db`, SQLite).

- `watch add search [search flags] --name NAME [--notify SINK]... [--interval D] [--pages N] [--emit-initial]`
- `watch add product PRODUCT [--store S] --name NAME [--notify SINK]... [--interval D] [--emit-initial]`

  `add` runs the first check at once as a silent baseline (reported as `product.new` with
  `--emit-initial`). The target pins the store resolved at creation; a later change of the default store
  does not affect it. A target that cannot be read is not saved.
- `watch check [NAME...] [--all]`: due watches (or the named ones, or all) once. Events printed and
  delivered to the watch's sinks, then committed: a crash between the two repeats events, never loses
  them. Exit 0 when nothing changed. A block (exit 5) stops the run so it does not deepen.
- `watch run [--interval D]`: the same in a loop, with up to 10% jitter.
- `watch events [NAME] [--since D] [--limit N]`, `watch list`, `watch remove NAME [--yes]`.
- `watch service install [--interval D] | uninstall | status`: a systemd user timer (Linux) or launchd
  agent (macOS) running `obramat watch check --format jsonl`, output appended to
  `$XDG_STATE_HOME/obramat-cli/obramat-watch.log` (rotated at 1 MiB). The units pin `HOME`, the XDG
  variables, `OBRAMAT_CONFIG` and `OBRAMAT_STORE` as they were at install. Windows gets the `schtasks`
  line to run.

Interval: default 30m, floor 5m.

Events:

| type | when | `change` |
|---|---|---|
| `product.new` | a search lists a product it had not listed | |
| `product.price_changed` | price differs from the last check | `{from, to}` |
| `product.stock_changed` | product watches: quantity differs | `{from, to}` |
| `product.out_of_stock` | the store had it, now does not | |
| `product.back_in_stock` | the store did not have it, now does | |
| `product.removed` | product watches: the page is gone (404) | |

Search watches ignore quantity changes, which happen with every sale.
Event: `{type, watch, at, product: Product, change}`.

### sink

Declared in config.toml:

```toml
[sinks.phone]
type = "ntfy"            # url, topic, token_file (optional)
url = "https://ntfy.sh"
topic = "my-obramat"

[sinks.chat]
type = "webhook"         # url; template = "discord" | "slack" sends text, otherwise the event JSON
url = "https://discord.com/api/webhooks/..."
template = "discord"

[sinks.script]
type = "exec"            # command: event JSON on stdin, OBRAMAT_EVENT_TYPE and OBRAMAT_WATCH set
command = ["/home/me/bin/on-obramat-event"]
```

`sink list` shows each and what is wrong with it; `sink test NAME` sends a made-up event.

### mcp

`obramat mcp` serves MCP over stdio (JSON-RPC 2.0, one message per line; protocol revisions 2024-11-05,
2025-03-26, 2025-06-18). Tools are command lines run in-process, so a tool returns exactly what the
command prints; a failure returns the `--error-format json` envelope with `isError: true`. Tools:
`search`, `product_show`, `store_stock`, `store_list`, `store_near`, `category_list`, `watch_check`.
Unknown arguments are refused (JSON-RPC -32602). `mcp install` writes `command` and `args` for this
binary into the harness config, keeps every other entry, and reports `added`, `updated` or `unchanged`.

### config / doctor / skills / update

- `config path | list | get KEY | set KEY VALUE` on config.toml. Keys: `store`, `watch.interval`,
  `sinks.NAME.*`. Values are checked by decoding the whole file strictly before it is written.
- `doctor`: config parses, sinks are valid, a probe search answers and parses, the default store
  resolves, the state database opens, the schedule state. Exit 1 if anything fails.
- `skills list | get NAME`: embedded usage notes for agents.
- `update [--check] [--yes]`: replaces the binary with the latest GitHub release after checking it
  against `checksums.txt`. Development builds are not replaced. Interactive commands check at most once a
  day and ask; scripts, `mcp` and `watch` never do; `OBRAMAT_NO_UPDATE_CHECK=1` turns it off.

## 5. Global flags

| flag | default | meaning |
|---|---|---|
| `--format json\|jsonl\|pretty\|toon` | `json` | stdout rendering |
| `--no-color` | off | also `NO_COLOR`, `TERM=dumb`, non-TTY |
| `--error-format text\|json` | `text` | stderr error rendering (also `OBRAMAT_ERROR_FORMAT`) |
| `--no-input` | off | never prompt |
| `--debug` | off | one stderr line per request and response |
| `-h, --help`, `-v, --version` | | |

No `--profile`: without accounts there is nothing to switch between; the store is the only context and
has its own flag.

## 6. I/O contract

- stdout carries data only: one JSON document, or one object per line with `jsonl`.
- stderr carries errors, notices and prompts. Text from the site is stripped of control characters
  before it reaches a terminal.
- Prompts only when stdin, stdout and stderr are terminals and `--no-input` is absent.

## 7. Exit codes

| code | meaning |
|---|---|
| 0 | success, including "nothing changed" |
| 1 | generic failure |
| 2 | usage: bad flags or arguments, missing store for `--in-stock`, landing-page category |
| 3 | reserved for auth; unused |
| 4 | not found: product, page past the last, store, watch |
| 5 | blocked: the site's bot protection refused (HTTP 403 from DataDome) or HTTP 429 |
| 6 | network, timeout, or the site down (5xx, maintenance page) |
| 7 | the site changed: a known page no longer has the expected shape |
| 130 | interrupted |

## 8. Errors

Text errors are one line, `obramat: ` prefixed. `--error-format json`: `{code, category, retryable,
message, http_status, endpoint, suggested_commands, issue_url}`. Exit 7 carries `issue_url`, a prefilled
`api-change.yml` issue with CLI version, OS/arch, command path, endpoint template, HTTP status and the
first 300 bytes of the response; never the user's arguments.

## 9. Transport

Every request goes to `https://www.obramat.es` (override with `OBRAMAT_BASE_URL` for tests) with a
browser User-Agent ending in `obramat-cli/<version>`, `Accept` and `Accept-Language: es-ES`. The site's
bot protection refuses a non-browser User-Agent; see [ADR 0003](adr/0003-browser-user-agent.md).
Requests from one process are spaced at least 400 ms apart, 1.5 s inside sweeps (`store list`,
`store stock --all`). No cookies are kept between requests; the store cookie is built for each request
that needs it.

After a block (exit 5) the CLI writes `$XDG_CACHE_HOME/obramat-cli/blocked-until` and, for the next
10 minutes, refuses every command that would contact the site with exit 5 and the time the pause ends,
without sending anything. Local commands (`config`, `watch list`, `watch events`, `sink`, `skills`) keep
working; `watch run` sits the pause out. Deleting the file overrides it. Observed 2026-10-19: two
back-to-back `--all` sweeps at 400 ms got a 403 that was still in place 45 s later.

## 10. Files and environment

| what | path | mode |
|---|---|---|
| config | `$XDG_CONFIG_HOME/obramat-cli/config.toml` (or `OBRAMAT_CONFIG`) | 0600 |
| watch state | `$XDG_DATA_HOME/obramat-cli/state.db` | 0600 |
| store list cache, update stamp | `$XDG_CACHE_HOME/obramat-cli/` | 0600 |
| scheduler log | `$XDG_STATE_HOME/obramat-cli/obramat-watch.log` | |

Writes are atomic (temp file and rename). Env: `OBRAMAT_STORE`, `OBRAMAT_CONFIG`, `OBRAMAT_ERROR_FORMAT`,
`OBRAMAT_NO_UPDATE_CHECK`, `OBRAMAT_BASE_URL`, `NO_COLOR`. Precedence: flag > env > config.

## 11. Stack

cobra + pflag, `net/http`, `pelletier/go-toml/v2`, `adrg/xdg`, `modernc.org/sqlite`, `toon-go`.
No MCP SDK, no HTML parser: the pages are read through their JSON islands and the site's own class
names. Tests use stdlib `testing`, `httptest` serving captured responses from
`internal/api/testdata/`, and a binary-level test with an isolated XDG home. No live calls in tests.

## 12. Layout

```
cmd/obramat/       entrypoint; under cmd/ so `go install .../cmd/obramat` names the binary obramat
cmd/docsgen/       writes docs-site/content/docs/reference from the cobra tree
internal/api/      everything that knows obramat.es: client, listing, product, stores, categories
internal/cli/      one file per noun; skills/ holds the embedded notes
internal/state/    SQLite watch state
internal/watch/    one check: fetch, diff, events
internal/sink/     ntfy, webhook, exec
internal/mcp/      MCP server, tool table, harness install
internal/output/   json/jsonl/pretty/toon, error envelope, exit codes
internal/config/   paths, config.toml, atomic writes
internal/updater/  release check and checksum-verified replace
```

## 13. Not in v1

Cart, checkout, orders, account, quotes (read-only by design). Quantity-price tiers (the "Palet" price
on some product pages). Search autocomplete. Postcode-to-coordinates lookup for `store near`.
Homebrew, Scoop, AUR and npm packages.
