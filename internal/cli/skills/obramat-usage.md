# obramat usage notes

`obramat` reads obramat.es: products, prices, stores and stock per store. It is read-only
and needs no account. Output is JSON on stdout; `--format pretty` for people, `jsonl` for
streams, `toon` for compact model context. Errors go to stderr; `--error-format json` gives
an envelope with `code`, `category` and `retryable`.

## Bot protection

obramat.es is behind DataDome and refuses scripts. Replay the user's own browser session:
`obramat auth harvest` copies the cookie store and key reference out of the user's real Chrome
profile into a temp profile, opens Chrome headed on it, waits while the user clears any captcha,
and reads the session out over CDP (Chrome 136+ ignores --remote-debugging-port on the default
profile, hence the copy; use `--browser-url` to attach to a Chrome already running with a devtools
port, `--profile` to name the profile). Without a display, import an export instead:
`obramat auth login --cookies FILE` (or `--har FILE`, or `--cookies-stdin`); a HAR also carries the
browser's user agent. `obramat auth status --check` asks the site whether the session still works;
`obramat auth logout` forgets it. Without a session, commands that need the storefront exit 3.
Sessions expire: harvest or re-import when the site starts challenging (exit 5).

## Store context

Prices and stock belong to a store. Pass `--store` (code, slug or part of the name),
set `OBRAMAT_STORE`, or `obramat config set store ID`. Without a store you get the site's
default web price (some products show a Canary Islands IGIC price) and no stock.

```
obramat store list                       # codes, names, cities (cached a week)
obramat store near --lat 40.42 --lng -3.70
```

## Search and browse

```
obramat search "tablero osb" --store 4
obramat search cemento --brand WEBER --min-price 5 --max-price 20 --sort price_asc
obramat search filters cemento           # facet codes for --filter CODE=VALUE
obramat search --category /madera/tableros/tableros-y-tablas-de-madera/ --in-stock --store 4
obramat category list --parent /madera/
```

`{"query","category","store","page","pages","total","sort","products":[{ref,title,url,image,brand,price:{amount,amount_ex_vat,tax,unit,currency,per_measure},store,stock:{quantity,available,label},badges,delivery}]}`.
The site serves at most 10 pages. `--in-stock` needs a store and uses the site's own
"En stock en Almacén" filter.

## Products and stock

```
obramat product show 25064668 --store 4          # price, stock, delivery, characteristics, EAN
obramat store stock 25064668 --store 4            # one store, exact
obramat store stock 25064668 --lat 41.39 --lng 2.17   # six nearest stores
obramat store stock 25064668 --all                # every store (about 15 requests)
```

## Watches

```
obramat watch add product 25064668 --store 4 --name osb
obramat watch add search "tablero osb" --store 4 --name osb-search --notify phone
obramat watch check --format jsonl
obramat watch events --since 24h
```

Events: `product.new`, `product.price_changed`, `product.stock_changed` (product watches),
`product.out_of_stock`, `product.back_in_stock`, `product.removed`. Each carries `watch`, `at`,
`product` and, for changes, `change: {from, to}`.

## Exit codes

0 ok (also "no changes"), 1 generic, 2 usage, 3 no browser session for a command that needs
the site (`obramat auth login`), 4 not found, 5 blocked by the site's bot protection or rate
limited (wait minutes, do not loop), 6 network or site down, 7 the site changed shape (stderr
carries a prefilled issue link), 130 interrupted.

Keep the request rate low: one command is one to a few requests; `--all` and `--pages` add more.
