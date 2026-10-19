# obramat.es: how the site works, as far as the CLI needs it

Recorded 2026-10-18/19 by reading pages, their JavaScript bundles and live responses. Nothing here is
documented by the site; every claim has a captured response in `internal/api/testdata/` and a test.
Feasibility and the bot-protection probe are in [reference-obramat-feasibility.md](reference-obramat-feasibility.md).

## 1. Hosts and routing

| host | role |
|---|---|
| `www.obramat.es` | everything: pages, JSON endpoints, sitemaps, JS bundles |
| `media.adeo.com` | images (`/media/{id}/media.jpg?width=N`; without `width` the original) |

The storefront is ADEO's "Kobi" platform: server-rendered pages composed from versioned
micro-frontends (`/guidance-module/1.267.8/`, `/product-details-module/6.356.0/`,
`/store-header-module/6.39.2/`, `/fox--microfront--store/1.74.0/`). Responses come through a CDN
(`x-served-by: cache-…`) with DataDome in front (`x-datadome: protected`).

Request headers that work:

```http
GET /search?q=yeso HTTP/2
Host: www.obramat.es
User-Agent: Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 obramat-cli/0.1.0
Accept: text/html,application/xhtml+xml;q=0.9,*/*;q=0.8
Accept-Language: es-ES,es;q=0.9
```

A non-browser User-Agent gets `403` with a 773-byte page that loads `ct.captcha-delivery.com/i.js`
(`testdata/datadome_403.html`). A request reusing a flagged `datadome` cookie on a JSON endpoint got
`403 {"url":"https://geo.captcha-delivery.com/interstitial/?..."}` (`testdata/datadome_403.json`).

## 2. Auth model

None for anything the CLI reads. There is no guest token or CSRF for GET requests (pages set an
`lm-csrf` cookie used by the cart, which the CLI does not touch).

**Store context** is the one piece of state. The site keeps it in a cookie:

```
customer_context={"auto_context":"false","nearby_stores":"","city":"Leganés","latitude":"40.3354",
  "main_store":"4","postcode":"28913","stores_name":[{"name":"Obramat+Leganés","id":"4"}],
  "white_zone":"true","longitude":"-3.7656"}   (URL-encoded JSON)
```

set by

```http
GET /store-header-module/services/storeheader/customer-context-cookie?mainStore=4&latitude=40.3354&longitude=-3.7656
Accept: application/json

HTTP/2 200
set-cookie: customer_context=%7B%22auto_context%22%3A%22false%22%2C...; Max-Age=34128000
```

The minimum the server honours is `{"main_store":"4","stores_name":[{"id":"4","name":"…"}]}`; `name`
can be any non-empty string. Without `stores_name[].name`, product pages still show the store's stock
and price but listings render most cards without prices. `main_store` must be the **store code** (4
for Leganés), not the nearby API's internal id (45); with the internal id pages render as if no
store were set. See ADR 0001.

## 3. Catalogue

### 3.1 Search and category listings

```http
GET /search?q=cemento&filters={"attribute-06575":"WEBER","range-price-min":"5","range-price-max":"20"}&sort=price-asc&p=2
GET /madera/tableros/tableros-y-tablas-de-madera/?filters=...&sort=...&p=...
```

- `filters` is a JSON object, URL-encoded: facet code → values joined by `#|`; the price range is
  `range-price-min` / `range-price-max` (found in `Guidance.js`: `Kt`/`Gt` encode and decode it).
- `sort`: `relevance` (default, omitted), `price-asc`, `price-desc`.
- `p`: page, 48 products per page. Past page 10 the page carries `ssrDataType: "dependencyError"`
  (`search_cemento_p11_dependency-error.html.gz`), even when `numberOfPages` says 10 for 657 results.
- A category page ignores `q` (same 94 products with and without it).
- A search for an exact reference answers `307` to the product page.
- With a store set, results are what that store sells: "tablero osb" 8 → 4, "cemento" 657 → 204,
  the boards category 94 → 61.
- Top-level categories (`/madera/`) are landing pages with no listing island.

The page embeds the listing state:

```html
<script type="application/json" id="guidance-ssr-data">
{"ssrData":{"ssrDataType":"browseProducts","body":{
  "pagination":{"currentPage":1,"numberOfPages":10},
  "products":{"groups":[{"id":"eager","codes":["10438246",...],"lazy":false},
                        {"id":"lazy-1","codes":[...],"lazy":true},{"id":"lazy-2","codes":[...],"lazy":true}],
              "number":657},
  "sorts":[{"code":"relevance","label":"Relevancia","applied":true},{"code":"price-asc",...},{"code":"price-desc",...}],
  "filters":{"stockAndDeliveryFilters":[{"type":"toggle","code":"availability","label":"En stock en Almacén",
               "value":"inStockInStore","applied":false,"total":211,"storeSubLabel":"Obramat Leganés"}],
             "productFilters":[{"type":"range","code":"price","label":"Precio","applied":true,"min":5,"max":20,"unit":"€"},
                               {"type":"standard","code":"attribute-06575","label":"Marca","applied":true,
                                "values":[{"value":"WEBER","total":13,"applied":true},...]}]}}}}
</script>
```

`codes` is the ranking. `stockAndDeliveryFilters` carries the site's stock toggle and is populated only
when a store is in context: `{"type":"toggle","code":"availability","label":"En stock en Almacén",
"value":"inStockInStore","applied":false,"total":211}`. It travels in the `filters` parameter like any
other facet (`{"availability":"inStockInStore"}`) and is what `--in-stock` sends.

**Product cards** follow, one `<li class="product-thumbnail product-thumbnail-item …" data-reflm="REF">`
each. What the CLI reads from a card:

| field | markup |
|---|---|
| title, URL | `<a href="/productos/…-REF.html" title="TITLE" class="a-designation …">` |
| image | `<img src="https://media.adeo.com/media/…?width=640" … class="a-illustration__img">` |
| brand | `class="o-thumbnail__brand"><img alt="VALDERRIVAS"` (only some cards) |
| badges | `<span class="a-flag__label">Mejor Precio por Cantidad</span>` |
| price | screen-reader text inside `<p class="m-price -main …">`: `Vendido 28 € IVA por cajas` |
| price without tax | same in `-mainSub`: `Vendido 23,14 € sin IVA por cajas` |
| per-measure price | `-secondary` / `-secondarySub`: `Vendido 8,96 € IVA por m²` |
| stock (store set) | `<span class="stock-status_label">205 en stock en Obramat Leganés</span>` |
| delivery | other `stock-status_label`s: `Recogida 2 h en Almacén`, `Envío a tu Obra en 24 horas` |

Numbers are Spanish-formatted (`1.596`, `8,96`). Without a store some cards carry Canary Islands prices:
`Vendido 35 € IGIC incluido por Unidad` (`search_tablero-osb.html.gz`, ref 25086454). Each card also has
a `dataTms` JSON blob with `unitprice_ati`; it is missing for about half the cards, so the CLI does not
use it.

**Lazy cards.** Cards of the `lazy` groups are placeholders:

```html
<li class="product-thumbnail product-thumbnail-item mozaic-v2 product-thumbnail-placeholder "
    data-reflm="10678052" data-lazy-url="/lazy/com.adeo.product.thumbnails.ProductThumbnailFragment?ktx=…">
```

All placeholders of a group share one `data-lazy-url`; fetching it (with the same store cookie) returns
the group's full cards (`lazy_cemento_store4.html.gz`). How many cards are eager varies per response
(22 of 48 in one capture, 4 of 48 in another), so a page needs up to three more requests.

Repro:

```bash
obramat search cemento --brand WEBER --min-price 5 --max-price 20 --sort price_asc --debug
obramat search --category /madera/tableros/tableros-y-tablas-de-madera/ --store 4
obramat search cemento --store 4 --in-stock --debug   # filters={"availability":"inStockInStore"}
obramat search filters cemento --format pretty
```

### 3.2 Product page

```http
GET /productos/tablero-osb3-250x125x1-5cm-25064668.html
```

Only the trailing reference matters to the user, but the slug is required: `/productos/25064668.html`
is a 404. Read from the page:

- `<script type="application/ld+json" id="jsonld_PRODUCT">`: `name`, `sku`, `gtin` (EAN), `image`,
  `description`, `offers.price`.
- `id="jsonld_BREADCRUMB"`: the category path (second-to-last item).
- `<span class="js-product-ref">25064668</span>`.
- Characteristics: `<th class="m-product-attr-row__name">…</th><td class="m-product-attr-row__value">…</td>`.
- Price block: same `m-price` lines as cards, between the `<h1>` and the first `mc-accordion`
  (recommendation carousels further down have their own prices).
- "How to buy" block, by `data-cerberus="ELEM_availability-in-store|pickup|delivery"`:
  `205 en stock en Obramat Leganés`, `Disponible en 2 horas en Obramat Leganés`,
  `Envío a tu Obra en 24 horas`.
- `"add_to_cart_availability":true|false` in the `dataTms` blob.

Store-dependent: with no store the price is the web price and `available_deliveries[].stock` is `1`, a
flag. With a store not selling the product, the page shows "Este producto no está disponible" and no
price (`product_25030393_store4_unavailable.html.gz`). With a store that sells it only in-store,
`available_deliveries` is empty and the quantity exists only in the text (`product_25030393_store7.html.gz`).

404 page: `<title>404 - Página no encontrada | Obramat` with status 404.

Repro: `obramat product show 25064668 --store 4 --format pretty`

### 3.3 Categories

`GET /sitemap-category1.xml`: 1161 `<loc>` category URLs. No names; the CLI derives them from the path.

Repro: `obramat category list --parent /madera/`

## 4. Stores and stock

### 4.1 Store list

`GET /sitemap-store1.xml`: 41 store pages, `/almacenes/obramat-{slug}.html`.

### 4.2 Stores near a point

```http
GET /fox--microfront--store/api/v1/stores-nearby-by-distance?latitude=40.4168&longitude=-3.7038&isCcdp=true
Accept: application/json

{"currentStore":null,"distance":0,"stores":[{"address":{"city":"Usera (Madrid) ","latitude":null,...},
  "code":"012","id":12,"name":"Obramat Usera","distance":null,
  "opening":{"isOpen":false,"label":"Cerrado. Abierto a las 07:00"},"phone":"913432790",
  "originalStore":{"poiId":12,"address":{"addressLine2":"Av. los Rosales, 24","zipCode":"28041",
    "city":"Usera (Madrid) ","coordinates":{"latitude":40.36506,"longitude":-3.68818}},
    "contacts":[{"type":"PHONE","value":"913432790"},{"type":"EMAIL","value":"info.usera@obramat.es"}],
    "webSite":{"url":"https://www.obramat.es/almacenes/obramat-usera.html"},"distance":5.9099,...}}, ...]}
```

Six stores with `isCcdp=true`, three without (and then `id` is the internal id, 53 for Usera). Paging
parameters (`limit`, `size`, `distance`…) are ignored. `originalStore.webSite.url` is not reliable:
Girona has `www.obramat.es`, Palma `http://localhost`. The CLI keys stores by `poiId` (the store code)
and builds a missing slug from the name.

`store list` = sitemap + this endpoint at fifteen points across Spain (37 of 41 stores) + the store page
of each one missed.

### 4.3 Store page

`GET /almacenes/obramat-usera.html`: `id="jsonld_STORE"` (`name`, `geo`, `address.addressLocality`,
`telephone`, special opening hours) and `data-store-id="12"`.

### 4.4 Stock per store

The product page's "Ver disponibilidad por Almacén" drawer:

```http
GET /store-header-module/services/contextlayer/store-search-result?latitude=40.4168&longitude=-3.7038&productRef=25064668&storeSearchType=STOCK
Accept: text/html
```

```html
<section class="m-store-search-result"> …
<article class="m-store-search-card" data-selectorid="store-search-card" data-store-city="Usera (Madrid) ">
  <span class="m-store-info-header--title" role="heading" aria-level="3">Obramat Usera</span>
  • <span class="m-store-info-header__store-distance">11,2 km</span>
  <div class="stock-status_text"><span class="stock-status_badge stock-status_badge--green"></span> 94 en Stock</div>
  <a … href="/almacenes/obramat-usera.html" …>
  <button … data-store-id="12" aria-label="Elegir este Almacén">
</article> …
```

Six stores nearest the point. Out of stock: `stock-status_badge--red` and `Stock no disponible`
(`stock_25030393_madrid.html.gz`). No cookie needed. Found by following
`initChangeStoreLinkWithStock` (product-details bundle) → `openStoreContextLayerInSearchMode` event →
`fetchStoresAroundRenderingService` (contextlayer bundle).

For one store the CLI reads the product page with that store's cookie instead: exact quantity and that
store's price in one request.

Repro:

```bash
obramat store near --lat 40.42 --lng -3.70
obramat store stock 25064668 --lat 40.42 --lng -3.70
obramat store stock 25064668 --store 4
obramat store stock 25064668 --all
```

## 5. Cart, region, account

Out of scope (read-only, ADR 0002). For the record: the add-to-cart form posts `reflm`, `offerId`,
`quantity` and `contextCode` to the cart module; store selection is the cookie above; the account login
was not explored.

## 6. Gotchas

- `captcha-delivery.com` is in every page's CSP; it is not a block by itself.
- `/store-header-module/services/autocontextualization/customer-context?storeId=` answered a
  maintenance page (503) once and a DataDome interstitial later. The CLI does not use it.
- robots.txt disallows `/search?q=*`, `filters`, `sort`, `/lazy/` and `/rest/` for crawlers. The CLI
  fetches what one browser page view fetches, on a person's request.
- Price numbers: `.` thousands, `,` decimals. Units are the site's words (`Unidad`, `cajas`, `Kg`, `m²`).
