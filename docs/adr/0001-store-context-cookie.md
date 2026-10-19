# 0001. Store context comes from a cookie the CLI builds

Date: 2026-10-19
Status: accepted

## Context

On obramat.es prices and stock are per warehouse. A visitor picks a store in a drawer; the page then
reloads with a `customer_context` cookie and every price, stock line and listing is for that store. The
cookie is set by `GET /store-header-module/services/storeheader/customer-context-cookie` and holds
JSON: main store, coordinates, postcode, city, store names.

Options:

1. Call the site's endpoint once per store and keep the cookie it sets. One more request per store, and
   a cookie store to keep fresh. The neighbouring endpoint
   (`/autocontextualization/customer-context`) answered a maintenance page and later a bot-protection
   interstitial during discovery.
2. Build the cookie. Probing showed the server needs only `main_store` and one `stores_name` entry with
   any non-empty `name`. Without the name, product pages still render for the store but listings drop
   most prices (5 of 22 cards priced instead of 20).
3. Use the stock drawer endpoint only. It gives stock at six stores around a point but no prices.

## Decision

Build the cookie per request from the store code and (when known) its name:
`{"main_store":"4","stores_name":[{"id":"4","name":"Obramat Leganés"}]}`. Nothing is persisted between
requests. The stock drawer (option 3) is used for multi-store stock, where it answers for six stores in
one request.

## Consequences

- One request per page for any store, no session state, nothing to expire.
- The CLI depends on the cookie format. If the site starts validating it, listings would come back
  without store prices; the regression test `TestStoreCookieCarriesName` and the fixtures pin the
  observed behaviour, and the fix is in `internal/api` only.
- The store code (Usera is 12) is the identifier everywhere. The nearby-stores API also has an internal
  id (Usera is 53) that the cookie does not accept; it is never exposed.
