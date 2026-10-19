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
auth       login | harvest | status | logout
mcp        (no verb: serves) | install claude-code|codex|cursor
config     path | list | get | set
doctor
skills     list | get
update
completion bash|zsh|fish|powershell
```

No account commands: the CLI is read-only and the site's public pages carry everything it reads. `auth`
imports the user's own browser session to get past bot protection; it is not a login and no password is
ever asked for. See [ADR 0002](adr/0002-read-only.md) and [ADR 0004](adr/0004-browser-session-and-chrome-tls.md).

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
- `--in-stock` keeps products with stock at the store, through the site's own "En stock en Almacén" filter
  (`availability=inStockInStore`, offered only when a store is set; it requires one). Cards the site still
  returns without a stock line for that store are dropped as well.
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

### auth

The site is behind bot protection that answers scripts with a challenge page. The CLI gets through by
replaying the user's own browser session over a Chrome-like TLS handshake; no account and no password
are involved.

- `auth login [--cookies FILE | --har FILE | --cookies-stdin] [--cookies-format auto|json|netscape|header|har]
  [--user-agent UA] [--no-check]`: import an export. Accepted: Cookie-Editor / EditThisCookie JSON (array
  or `{"url","cookies"}` wrapper), DevTools cookie lists, Netscape `cookies.txt`, a raw `Cookie:` header,
  HAR 1.2. Only `obramat.es` cookies are kept, so a whole-browser export is fine. A HAR also carries the
  browser's user agent and `Sec-CH-UA*` hints. The session is stored 0600 at
  `$XDG_CONFIG_HOME/obramat-cli/session.json`; `login` then asks the storefront whether it works unless
  `--no-check`.
- `auth harvest [--profile DIR] [--browser-url URL] [--chrome PATH] [--user-agent UA] [--timeout D]`:
  read the session straight out of the user's real Chrome, so no export is needed. The trusted
  `datadome` cookie lives in the everyday profile, so the command copies only that profile's cookie
  store and key reference (`Local State`, `Default/Cookies` and/or `Default/Network/Cookies` with their
  journal files, `Default/Preferences`) into a temp profile (0700, under the data dir, removed on exit)
  and opens Chrome headed on it. Chrome 136+ ignores `--remote-debugging-port` on the default profile,
  which is why it is a copy launched with an explicit loopback port rather than an attach. It
  polls up to `--timeout` (default 5m) until the challenge is clear, then reads the cookies and
  `navigator.userAgent` over CDP and stores them like `login`. `--profile`/`OBRAMAT_CHROME_PROFILE`
  names the profile (default: the usual Chrome/Chromium locations, first with a cookie store);
  `--chrome`/`OBRAMAT_CHROME` names the binary. With no real profile it opens a fresh window, which may
  need a captcha; with `--browser-url`/`OBRAMAT_CHROME_URL` (or a Chrome already on
  `http://localhost:9222`) it attaches instead.
- `auth status [--check]`: where the session comes from and what it carries (cookie names only, never
  values). `--check` makes one request.
- `auth logout`: forget the stored session. The browser is not touched.

`OBRAMAT_COOKIEJAR=FILE` uses an export on every run instead of the stored session and is never written
to disk; `auth login` refuses to shadow it. Rotated cookies (DataDome rotates on most responses) are
written back for file sessions only. `auth status --check` with no session exits 3; the other commands
run without one and let the site answer.

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
| 3 | auth: `auth status --check` was run with no browser session (`obramat auth login`) |
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

Every request goes to `https://www.obramat.es` (override with `OBRAMAT_BASE_URL` for tests) over a
Chrome TLS handshake (`utls`, `HelloChrome_Auto`, ALPN pinned to `http/1.1`). With an imported session
the browser's User-Agent is sent verbatim; without one, a browser User-Agent ending in
`obramat-cli/<version>` is used. `Accept: application/json` or `text/html` and
`Accept-Language: es-ES`. The site's bot protection scores the handshake, the `datadome` cookie, the
User-Agent and the client hints together; see [ADR 0004](adr/0004-browser-session-and-chrome-tls.md).
Requests from one process are spaced at least 400 ms apart, 1.5 s inside sweeps (`store list`,
`store stock --all`). The store cookie is built for each request that needs it; the session's cookies
ride beside it, and a `customer_context` from the export is dropped when `--store` sets one.

After a block (exit 5) the CLI writes `$XDG_CACHE_HOME/obramat-cli/blocked-until` and, for the next
10 minutes, refuses every command that would contact the site with exit 5 and the time the pause ends,
without sending anything. Local commands (`config`, `watch list`, `watch events`, `sink`, `skills`) keep
working; `watch run` sits the pause out. Deleting the file overrides it. Observed 2026-10-19: two
back-to-back `--all` sweeps at 400 ms got a 403 that was still in place 45 s later.

## 10. Files and environment

| what | path | mode |
|---|---|---|
| config | `$XDG_CONFIG_HOME/obramat-cli/config.toml` (or `OBRAMAT_CONFIG`) | 0600 |
| browser session | `$XDG_CONFIG_HOME/obramat-cli/session.json` (or `OBRAMAT_COOKIEJAR=FILE` at runtime) | 0600 |
| harvest temp profiles | `$XDG_DATA_HOME/obramat-cli/harvest-*/` created and removed per run | 0700 |
| harvest fallback profile | `$XDG_DATA_HOME/obramat-cli/chrome` (only when no real profile is found) | |
| watch state | `$XDG_DATA_HOME/obramat-cli/state.db` | 0600 |
| store list cache, update stamp | `$XDG_CACHE_HOME/obramat-cli/` | 0600 |
| scheduler log | `$XDG_STATE_HOME/obramat-cli/obramat-watch.log` | |

Writes are atomic (temp file and rename). Env: `OBRAMAT_STORE`, `OBRAMAT_CONFIG`, `OBRAMAT_COOKIEJAR`,
`OBRAMAT_CHROME`, `OBRAMAT_CHROME_PROFILE`, `OBRAMAT_CHROME_URL`, `OBRAMAT_ERROR_FORMAT`,
`OBRAMAT_NO_UPDATE_CHECK`, `OBRAMAT_BASE_URL`, `NO_COLOR`. Precedence: flag > env > config.

## 11. Stack

cobra + pflag, `net/http`, `refraction-networking/utls` (the Chrome handshake),
`chromedp/chromedp` (the harvest's DevTools protocol), `pelletier/go-toml/v2`, `adrg/xdg`,
`modernc.org/sqlite`, `toon-go`. No MCP SDK, no HTML parser: the pages are read through their JSON
islands and the site's own class names. No browser is bundled: the harvest drives the system Chrome.
Tests use stdlib `testing`, `httptest` serving captured responses from `internal/api/testdata/` and
hand-written `*.synthetic.*` cookie fixtures, and a binary-level test with an isolated XDG home. No
test launches a browser or calls obramat.es.

## 12. Layout

```
cmd/obramat/       entrypoint; under cmd/ so `go install .../cmd/obramat` names the binary obramat
cmd/docsgen/       writes docs-site/content/docs/reference from the cobra tree
internal/api/      everything that knows obramat.es: client, transport (Chrome TLS), session, listing, product, stores, categories
internal/cli/      one file per noun (auth.go and harvest.go are the session); skills/ holds the embedded notes
internal/harvest/  reads a live session out of Chrome over CDP (integration only)
internal/state/    SQLite watch state
internal/watch/    one check: fetch, diff, events
internal/sink/     ntfy, webhook, exec
internal/mcp/      MCP server, tool table, harness install
internal/output/   json/jsonl/pretty/toon, error envelope, exit codes
internal/config/   paths, config.toml, the stored session, atomic writes
internal/updater/  release check and checksum-verified replace
```

## 13. Not in v1

Cart, checkout, orders, account, quotes (read-only by design). Quantity-price tiers (the "Palet" price
on some product pages). Search autocomplete. Postcode-to-coordinates lookup for `store near`.
Homebrew, Scoop, AUR and npm packages.
