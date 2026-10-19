# obramat-cli

Read-only terminal client for obramat.es, the Spanish building-materials warehouse chain: search the
catalogue, see a product's price and stock at each store, and get told when either changes.

## Working on it

- `go build ./... && go test ./... && go vet ./...` must stay clean; `make check` adds staticcheck and gofmt.
- After changing a command's help or flags, run `make docs`; CI fails on a stale reference.
- Everything that knows the site lives in `internal/api`. A site change is fixed there, with a new
  capture in `internal/api/testdata/` and a test that pins it.
- Every bug fix gets a regression test whose comment says what broke.
- Tests never call obramat.es. Live checks are manual, at a low rate.
- Spec: `docs/cli-spec.md`. Site notes: `docs/obramat-api-discovery.md`. Decisions: `docs/adr/`.

## Distribution

- Releases are goreleaser archives named `obramat_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows),
  with `checksums.txt`. `scripts/install.sh` is the `curl | sh` installer; it downloads, verifies the
  checksum, and only then installs.
- `npm/` is the `npx` wrapper. It is **not published**: publishing waits for an `NPM_TOKEN` secret, and
  the package version must equal the release tag so the postinstall can find the archive.
- Homebrew, Scoop and AUR are planned but not configured. They need a tap repo, a scoop bucket and an
  AUR key; do not add them to `.goreleaser.yaml` before those exist, or `release.yml` breaks.

## Language

### Access

**Session**:
The user's own browser cookies (and user agent) that the CLI replays to get past bot protection, stored
0600 on the machine. Imported with `obramat auth login`, or read from a real browser with
`obramat auth harvest`. Not an account and not a login.
_Avoid_: credentials, token, profile, cookie jar (except `OBRAMAT_COOKIEJAR`)

**Harvest**:
Reading the live session out of the user's real Chrome profile: copy its cookie store into a temp
profile, start Chrome on the copy, then read cookies over the DevTools protocol. Not an export by hand,
and not a scrape.
_Avoid_: scrape, capture (the fixture captures are a different thing), export

### Catalogue

**Product**:
One article the site sells, identified by its reference ("Ref." on the page, 6 to 10 digits).
_Avoid_: item, SKU, article

**Listing**:
One page of products, from a search or from a category, in the site's ranking order.

**Facet**:
A filter the site offers on a listing (brand, thickness, use...), addressed by its code
(`attribute-06575`). The price range is a facet too.
_Avoid_: attribute filter

**Category**:
A node of the site's category tree, addressed by its path (`/madera/tableros/`). Top-level ones are
landing pages without a listing.

### Stores

**Store**:
One Obramat warehouse ("Almacén"), identified by its code (Leganés is 4).
_Avoid_: shop, warehouse id, POI (the site's internal ids are not store codes)

**Store context**:
The store whose prices and stock a page shows. Absent means the site's default web price and no stock.

**Stock**:
Units of a product at one store, as the site reports them.
_Avoid_: availability (that is the yes/no derived from stock)

### Tracking

**Watch**:
A saved instruction to check a listing or a product, at a pinned store, and report changes. Local only.

**Check**:
One run of a watch: fetch, compare with the last stored state, emit events, store the new state.

**Event**:
One observed change: new product, price changed, stock changed, out of stock, back in stock, removed.

**Sink**:
Where events go besides stdout: ntfy, a webhook, or a program.
