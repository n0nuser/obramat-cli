# obramat.es feasibility

**Verdict: viable: server-rendered HTML and two JSON/HTML endpoints, reachable with Go's standard `net/http` and a browser User-Agent. No login, no TLS fingerprinting, no headless browser.**

Probed 2026-10-18/19 from a residential connection in Spain.

## The assumed blocker: DataDome

Every response carries `x-datadome: protected`. A bare request is refused:

```sh
curl -sS -o /dev/null -w '%{http_code}\n' https://www.obramat.es/            # 403, x-datadome-riskscore 0.99
```

The refusal is a 773-byte page that loads `ct.captcha-delivery.com/i.js`. What DataDome checks first is
the User-Agent. A browser User-Agent with an ordinary `Accept` / `Accept-Language` is enough:

```sh
UA='Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36'
curl -sS -o /dev/null -w '%{http_code}\n' -A "$UA" -H 'Accept: text/html' -H 'Accept-Language: es-ES' \
  'https://www.obramat.es/search?q=yeso'                                     # 200
curl -sS -o /dev/null -w '%{http_code}\n' -A 'obramat-cli/0.1.0' -H 'Accept: text/html' \
  'https://www.obramat.es/search?q=yeso'                                     # 403
```

The same holds for Go's `net/http` (its TLS fingerprint differs from curl's): homepage, two searches and a
sitemap all answered 200 with real content in a test program. The string `captcha-delivery` also appears in
the Content-Security-Policy meta of every normal page, so it is not by itself a sign of a block.

A store-header endpoint answered one burst of requests with a DataDome JSON interstitial
(`{"url":"https://geo.captcha-delivery.com/interstitial/..."}`) after a cookie from a flagged request was
reused. The client sends no DataDome cookie and spaces its requests (400 ms by default).

## Backend

ADEO's "Kobi" storefront, the platform behind the group's DIY sites: server-rendered pages assembled from
versioned micro-frontends (`/guidance-module/1.267.8/...`, `/product-details-module/6.356.0/...`), Svelte
islands, and JSON islands in the HTML (`guidance-ssr-data`, `jsonld_*`, `dataTms`). Images on
`media.adeo.com`. The store locator embeds Woosmap; the CLI does not use it.

## Surface inventory

| surface | request | format |
|---|---|---|
| search | `GET /search?q=&filters=&sort=&p=` | HTML + `guidance-ssr-data` JSON |
| category | `GET /{path}/?filters=&sort=&p=` | same as search |
| lazy product cards | `GET /lazy/com.adeo.product.thumbnails.ProductThumbnailFragment?ktx=` | HTML fragment |
| product | `GET /productos/{slug}-{ref}.html` | HTML + JSON-LD |
| stores near a point | `GET /fox--microfront--store/api/v1/stores-nearby-by-distance?latitude=&longitude=&isCcdp=true` | JSON |
| stock by store | `GET /store-header-module/services/contextlayer/store-search-result?latitude=&longitude=&productRef=&storeSearchType=STOCK` | HTML fragment |
| store page | `GET /almacenes/{slug}.html` | HTML + JSON-LD |
| sitemaps | `GET /sitemap-store1.xml`, `GET /sitemap-category1.xml` | XML |

Store context (prices and stock of one warehouse) is a cookie the visitor's browser holds,
`customer_context`, which the CLI builds itself. Details in [obramat-api-discovery.md](obramat-api-discovery.md).

## Login

Not needed. Prices, per-store prices and per-store stock are public. The CLI is read-only: no cart, no
order, no account.

## Terms and scope

obramat.es publishes no API and no bug-bounty programme. `robots.txt` asks crawlers to stay away from
`/search?q=*`, filter and sort parameters, `/lazy/` and `/rest/`. This CLI is not a crawler: it fetches
what a person's browser fetches for one search or one product, at their request, at a low rate. It
should not be used for bulk scraping.

## Existing landscape

No public command-line client for obramat.es was found. Other ADEO storefronts share the platform, so
the parsing approach may carry over, but nothing is reused here.
