# examples

ready-to-run recipes with the real commands. every command prints json on stdout unless you add
`--format pretty`; that is what makes the `jq` lines below work.

set a default store once, then leave it out:

```bash
obramat store list --format pretty        # codes and names
obramat config set store 4                # Leganés
```

or pass `--store 4` / `--store leganes` per command, or set `OBRAMAT_STORE`.

## search

```bash
# a search at your store, human-readable
obramat search "tablero osb" --format pretty

# the site's own brand facet, a price range and a sort
obramat search cemento --brand WEBER --min-price 5 --max-price 20 --sort price_asc

# only what the store has in stock
obramat search "tablero osb" --in-stock --format pretty

# browse a category instead of searching
obramat search --category /madera/tableros/tableros-y-tablas-de-madera/

# list the facets a search offers, then filter on one
obramat search filters cemento --format pretty
obramat search cemento --filter attribute-06575=WEBER
```

## stock across warehouses

```bash
# one store, exact quantity and that store's price
obramat store stock 25064668 --store 4 --format pretty

# the six stores nearest a point
obramat store stock 25064668 --lat 40.42 --lng -3.70 --format pretty

# every warehouse (about 15 requests), one row per store
obramat store stock 25064668 --all --format pretty

# only the stores with at least 40 units
obramat store stock 25064668 --all | jq -r '.stores[] | select(.stock.quantity >= 40) | "\(.stock.quantity)\t\(.store.name)"'

# which warehouse is cheapest for a search
obramat search "tablero osb" --format json | jq -r '.products[] | "\(.price.amount)\t\(.stock.quantity)\t\(.title)"'
```

## watch price and stock

```bash
# one product at one store
obramat watch add product 25064668 --store 4 --name osb --notify phone

# a search, watched as a listing
obramat watch add search "tablero osb" --store 4 --max-price 30 --name osb-search

# one check now, as a stream
obramat watch check --format jsonl

# back-in-stock events from the last day
obramat watch events --since 24h | jq -r 'select(.type == "product.back_in_stock") | .product.url'

# run the checks on a systemd timer / launchd agent
obramat watch service install --interval 30m
obramat watch service status
```

a watch is stored in the local SQLite database, not in a file; `watch add` is the only way to create
one. `examples/config.toml` shows the sinks they can notify.

## check the install and the session

```bash
obramat doctor --format pretty
obramat auth status --check
obramat auth harvest        # borrow the browser session when the site starts challenging
```

## agents

```bash
obramat mcp install claude-code    # or codex, cursor
obramat skills get obramat-usage
```

## cron instead of the timer

```bash
# every 30 minutes, keep the output in a log
*/30 * * * * obramat watch check --format jsonl >> ~/.local/state/obramat-cli/cron.log 2>&1
```
