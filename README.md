<p align="center">
  <img src=".github/assets/obramat-cli-logo.svg" width="640" alt="obramat cli logo">
</p>

<p align="center">
  <a href="https://github.com/n0nuser/obramat-cli/releases"><img src="https://img.shields.io/github/v/release/n0nuser/obramat-cli?display_name=tag&style=flat-square&label=release&color=000000" alt="release badge"></a>
  <a href="https://github.com/n0nuser/obramat-cli/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/n0nuser/obramat-cli/ci.yml?branch=main&style=flat-square&label=ci&color=000000" alt="ci badge"></a>
  <a href="https://pkg.go.dev/github.com/n0nuser/obramat-cli"><img src="https://img.shields.io/badge/go-reference-000000?style=flat-square" alt="go reference badge"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-mit-000000?style=flat-square" alt="license badge"></a>
</p>

---

`obramat` is an unofficial terminal cli for obramat.es. it searches the catalogue with the same filters the site has, shows what a product costs at each warehouse and how many units each one holds, and watches products and searches for price and stock changes. it is read-only: no account, no cart, no orders. it compiles to one static Go binary. output is json by default so everything pipes into `jq`; `--format pretty` is for humans.

[documentation](https://DOCS_DOMAIN/docs) | [command reference](https://DOCS_DOMAIN/docs/reference) | [github](https://github.com/n0nuser/obramat-cli)

```console
$ obramat store stock 25064668 --lat 40.42 --lng -3.70 --format pretty
ID  STORE                STOCK  DISTANCE  SITE SAYS
12  Obramat Usera        94     10.7 km   94 en Stock
4   Obramat Leganés      205    13.0 km   205 en Stock
24  Obramat Alcobendas   68     17.1 km   68 en Stock
7   Obramat Rivas        86     19.4 km   86 en Stock
11  Obramat Majadahonda  266    22.2 km   266 en Stock
18  Obramat Alcorcón     198    22.4 km   198 en Stock
```

## why

buying materials for a job means knowing which warehouse has forty boards of the right thickness before you drive there, and what they cost at that warehouse, because prices differ between stores. the site answers that one product and one store at a time, behind a store-picker drawer. this cli answers it for a product across every store at once, for a whole search at your store, and keeps answering on a timer so you hear when the boards are back. json output means scripts, agents and shell pipelines all read the same thing.

## quickstart

### go install

```bash
go install github.com/n0nuser/obramat-cli/cmd/obramat@latest
obramat --version
```

### release binaries

download the archive for your platform from [releases](https://github.com/n0nuser/obramat-cli/releases) (linux, macos and windows, amd64 and arm64), unpack it and put `obramat` on your `PATH`.

every release ships `checksums.txt` with a github provenance attestation:

```sh
gh release download --repo n0nuser/obramat-cli --pattern checksums.txt
gh attestation verify checksums.txt --repo n0nuser/obramat-cli
```

### check it works

```bash
obramat doctor --format pretty
```

`doctor` checks that the config parses, sinks are valid, a test search answers and parses, the default store resolves, the watch database opens, and whether the watch timer is installed.

### pick your store

prices and stock belong to a store. find yours and make it the default:

```bash
obramat store near --lat 40.42 --lng -3.70 --format pretty
obramat store list --format pretty
obramat config set store 4
```

or pass `--store` per command (code, slug or name: `--store 4`, `--store leganes`), or set `OBRAMAT_STORE`. without a store the site shows a default web price (some products then show a canary islands IGIC price) and no stock.

## updates

```bash
obramat update --check
obramat update --yes
```

interactive commands check for a new release at most once a day and ask before updating. scripts, `mcp`, `watch` and `--no-input` never check. `OBRAMAT_NO_UPDATE_CHECK=1` turns it off. the download is verified against the release's `checksums.txt` before it replaces the binary. builds from source are not replaced.

## command surface

| command | what it does |
|---|---|
| `search [keywords] / search filters` | search or browse a category with brand, price range, sort and any facet; `filters` lists the facets |
| `product show` | price, stock at your store, delivery options, ean and characteristics |
| `category list` | category paths for `search --category` |
| `store list / near / show` | warehouses with their codes, the nearest ones, opening state |
| `store stock PRODUCT` | stock at one store, at the six nearest a point, or at all of them |
| `watch add search\|product` | watch a search or a product at a store |
| `watch check / run / events / list / remove` | one-shot check, foreground loop, history |
| `watch service install / uninstall / status` | systemd user timer or launchd agent running `watch check` |
| `sink list / test` | notification targets from config |
| `mcp / mcp install` | serve the read commands to an agent harness |
| `config path / list / get / set` | config.toml |
| `doctor` | checks config, sinks, the site, the store, the watch state |
| `skills list / get` | embedded usage notes for agents |
| `update` | replace the binary with the latest release |
| `completion bash\|zsh\|fish\|powershell` | shell completion |

global flags: `--format json|jsonl|pretty|toon`, `--no-color`, `--error-format text|json`, `--no-input`, `--debug`.

## examples

```bash
obramat search "tablero osb" --format pretty
obramat search cemento --brand WEBER --min-price 5 --max-price 20 --sort price_asc --store 4
obramat search "tablero osb" --store leganes --in-stock --format pretty
obramat search --category /madera/tableros/tableros-y-tablas-de-madera/ --filter attribute-00596=15
obramat search filters cemento --format pretty
obramat product show 25064668 --store 4 --format pretty
obramat store stock 25064668 --all --format pretty

obramat search "tablero osb" --store 4 | jq -r '.products[] | "\(.stock.quantity)\t\(.price.amount)\t\(.title)"'
obramat store stock 25064668 --all | jq '[.stores[] | select(.stock.quantity >= 40) | .store.name]'
```

## watches

a watch saves a search or a product, pinned to a store, and each check reports what changed: `product.new`, `product.price_changed`, `product.stock_changed`, `product.out_of_stock`, `product.back_in_stock`, `product.removed`.

```bash
obramat watch add product 25064668 --store leganes --name osb-15 --notify phone
obramat watch add search "tablero osb" --store 4 --max-price 30 --name osb
obramat watch check --format jsonl | jq -r 'select(.type=="product.back_in_stock") | .product.url'
obramat watch service install --interval 1h
```

## sinks config

```toml
[sinks.phone]
type = "ntfy"
url = "https://ntfy.sh"
topic = "my-obramat-alerts"

[sinks.chat]
type = "webhook"
url = "https://discord.com/api/webhooks/..."
template = "discord"

[sinks.script]
type = "exec"
command = ["~/bin/on-obramat-event"]   # event json on stdin
```

## agents

```bash
obramat mcp install claude-code    # or codex, cursor
obramat skills get obramat-usage
```

the mcp server exposes the read commands as tools (`search`, `product_show`, `store_stock`, `store_list`, `store_near`, `category_list`, `watch_check`). each tool returns exactly what the command prints.

## exit codes

`0` ok, `1` generic, `2` usage, `4` not found, `5` blocked by the site's bot protection or rate limited (the cli then stays away from the site for 10 minutes), `6` network or site down, `7` the site changed shape (stderr carries a prefilled issue link), `130` interrupted.

## building from source

```bash
git clone https://github.com/n0nuser/obramat-cli && cd obramat-cli
make build     # bin/obramat
make check     # vet, staticcheck, tests, gofmt
```

## documentation

the docs site source is in `docs-site` (fumadocs, exported as static pages); its command reference is generated from the binary with `make docs`.

- [docs/cli-spec.md](docs/cli-spec.md): the interface contract
- [docs/obramat-api-discovery.md](docs/obramat-api-discovery.md): how the site works, endpoint by endpoint
- [AGENTS.md](AGENTS.md): vocabulary and working rules
- [docs/adr](docs/adr): decisions

## contributing

issues and pull requests are welcome. when the site changes, the cli exits `7` and prints a prefilled issue link; that is the most useful report you can file.

## disclaimer

this is an unofficial client, not affiliated with, endorsed by or connected to obramat or its parent company. "obramat" is a trademark of its owner and is used here only to say which site the tool works with. the site publishes no api; the cli reads the same public pages a browser does, for interoperability. it is read-only. use it responsibly and keep the request rate low: no bulk scraping, keep watch intervals reasonable. prices and stock shown are what the site showed at that moment; check the site before you buy.

## license

[mit license](LICENSE)
