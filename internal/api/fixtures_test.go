package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every fixture in testdata is a real response captured from obramat.es
// (2026-10-18/19). HTML pages are gzipped to keep the repo small; session
// cookies (datadome, lm-csrf) are replaced by REDACTED.

func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(name, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		r = gz
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// roundTrip re-encodes v and decodes it again; the JSON contract must be
// stable under that.
func roundTrip[T any](t *testing.T, v T) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back T
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, back) {
		t.Fatalf("round trip changed the value:\n%#v\n%#v", v, back)
	}
}

func TestSearchCardsWithoutStore(t *testing.T) {
	page := fixture(t, "search_tablero-osb.html.gz")
	ssr, err := parseSSR(page, "GET /search")
	if err != nil {
		t.Fatal(err)
	}
	if ssr.Products.Number != 8 || ssr.Pagination.NumberOfPages != 1 {
		t.Fatalf("number=%d pages=%d", ssr.Products.Number, ssr.Pagination.NumberOfPages)
	}
	cards, _ := parseCards(page, StoreRef{})
	p, ok := cards["25064668"]
	if !ok {
		t.Fatalf("missing 25064668 among %d cards", len(cards))
	}
	if p.Title != "TABLERO OSB3 250X125X1,5CM" || !strings.HasSuffix(p.URL, "/productos/tablero-osb3-250x125x1-5cm-25064668.html") {
		t.Fatalf("card: %+v", p)
	}
	if p.Price == nil || p.Price.Amount != 23.89 || p.Price.Unit != "cajas" || p.Price.AmountExVAT == nil || *p.Price.AmountExVAT != 19.74 {
		t.Fatalf("price: %+v", p.Price)
	}
	if p.Price.PerMeasure == nil || p.Price.PerMeasure.Unit != "m²" {
		t.Fatalf("per measure: %+v", p.Price.PerMeasure)
	}
	if p.Store != nil || p.Stock != nil {
		t.Fatalf("no store context, but store=%v stock=%v", p.Store, p.Stock)
	}
	for ref, c := range cards {
		if c.Price == nil {
			t.Errorf("card %s has no price", ref)
		}
	}
	if c := cards["25086454"]; c.Price.Tax != "IGIC" || c.Price.Amount != 35 || c.Price.Unit != "Unidad" {
		t.Errorf("Canary Islands price: %+v", c.Price)
	}
	roundTrip(t, p)
}

func TestSearchCardsWithStore(t *testing.T) {
	page := fixture(t, "search_tablero-osb_store4.html.gz")
	cards, _ := parseCards(page, StoreRef{ID: 4, Name: "Obramat Leganés"})
	p := cards["25064668"]
	if p.Price == nil || p.Price.Amount != 28 {
		t.Fatalf("store price: %+v", p.Price)
	}
	if p.Stock == nil || p.Stock.Quantity == nil || *p.Stock.Quantity != 205 || !p.Stock.Available {
		t.Fatalf("stock: %+v", p.Stock)
	}
	if p.Store == nil || p.Store.ID != 4 {
		t.Fatalf("store: %+v", p.Store)
	}
	if len(p.Delivery) == 0 || len(p.Badges) == 0 {
		t.Fatalf("delivery %v badges %v", p.Delivery, p.Badges)
	}
	roundTrip(t, p)
}

func TestFiltersAndSortApplied(t *testing.T) {
	page := fixture(t, "search_cemento_weber_5-20_price-asc.html.gz")
	ssr, err := parseSSR(page, "GET /search")
	if err != nil {
		t.Fatal(err)
	}
	var brand, price bool
	for _, f := range ssr.filters() {
		if f.Code == "attribute-06575" && f.Applied && f.Label == "Marca" {
			brand = true
		}
		if f.Code == "price" && f.Applied && f.Type == "range" {
			price = true
		}
	}
	applied := ""
	for _, s := range ssr.Sorts {
		if s.Applied {
			applied = s.Code
		}
	}
	if !brand || !price || applied != "price-asc" {
		t.Fatalf("brand=%v price=%v sort=%q", brand, price, applied)
	}
}

func TestFiltersParam(t *testing.T) {
	lo, hi := 5.0, 20.0
	p := ListParams{Query: "cemento", Filters: map[string][]string{"attribute-06575": {"WEBER", "MONTÓ"}}, MinPrice: &lo, MaxPrice: &hi, Sort: "price-asc", Page: 2}
	r := p.request()
	got := r.query.Get("filters")
	want := `{"attribute-06575":"WEBER#|MONTÓ","range-price-max":"20","range-price-min":"5"}`
	if got != want {
		t.Fatalf("filters = %s, want %s", got, want)
	}
	if r.query.Get("sort") != "price-asc" || r.query.Get("p") != "2" || r.path != "/search" {
		t.Fatalf("query %v path %s", r.query, r.path)
	}
}

// TestInStockFilterParam pins how --in-stock reaches the site: the
// "En stock en Almacén" toggle is an ordinary facet, code availability, value
// inStockInStore (see the toggle's input name/value in a listing with a store).
func TestInStockFilterParam(t *testing.T) {
	p := ListParams{Query: "cemento", Store: StoreRef{ID: 4}, InStock: true}
	if got := p.request().query.Get("filters"); got != `{"availability":"inStockInStore"}` {
		t.Fatalf("filters = %s", got)
	}
	plain := ListParams{Query: "cemento", Store: StoreRef{ID: 4}}
	if got := plain.request().query.Get("filters"); got != "" {
		t.Fatalf("in-stock off still sent filters = %s", got)
	}
}

func TestPastLastPageIsNotFound(t *testing.T) {
	_, err := parseSSR(fixture(t, "search_cemento_p11_dependency-error.html.gz"), "GET /search")
	if e, ok := err.(*Error); !ok || e.Kind != KindNotFound {
		t.Fatalf("got %v", err)
	}
}

func TestNoResults(t *testing.T) {
	ssr, err := parseSSR(fixture(t, "search_no-results.html.gz"), "GET /search")
	if err != nil || ssr.Products.Number != 0 || len(ssr.Products.Groups) != 0 {
		t.Fatalf("%v %+v", err, ssr)
	}
}

func TestCategoryPage(t *testing.T) {
	page := fixture(t, "category_tableros-y-tablas_store4.html.gz")
	ssr, err := parseSSR(page, "GET /{category}/")
	if err != nil {
		t.Fatal(err)
	}
	// With a store set the site lists only what that store sells (94 without).
	if ssr.Products.Number != 61 || ssr.Pagination.NumberOfPages != 2 {
		t.Fatalf("%d %d", ssr.Products.Number, ssr.Pagination.NumberOfPages)
	}
	if jsonIsland(fixture(t, "category_madera_landing.html.gz"), "guidance-ssr-data") != "" {
		t.Fatal("landing page unexpectedly has a listing")
	}
}

func TestLazyFragment(t *testing.T) {
	page := fixture(t, "search_cemento_store4.html.gz")
	cards, lazy := parseCards(page, StoreRef{ID: 4})
	if len(lazy) == 0 {
		t.Fatal("expected lazy placeholders")
	}
	more, _ := parseCards(fixture(t, "lazy_cemento_store4.html.gz"), StoreRef{ID: 4})
	if len(more) < 10 {
		t.Fatalf("fragment gave %d cards", len(more))
	}
	priced := 0
	for _, p := range more {
		if p.Price != nil {
			priced++
		}
	}
	if priced < len(more)/2 {
		t.Fatalf("only %d of %d lazy cards priced", priced, len(more))
	}
	_ = cards
}

func TestProductWithoutStore(t *testing.T) {
	d, err := parseProduct(fixture(t, "product_25064668.html.gz"), "/productos/tablero-osb3-250x125x1-5cm-25064668.html", StoreRef{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Ref != "25064668" || d.EAN != "8421468411849" || d.Category != "/madera/tableros/tableros-y-tablas-de-madera/" {
		t.Fatalf("%+v", d)
	}
	if d.Price == nil || d.Price.Amount != 23.89 {
		t.Fatalf("price %+v", d.Price)
	}
	if d.Stock != nil {
		t.Fatalf("no store, but stock %+v", d.Stock)
	}
	if len(d.Attributes) < 5 || !d.Buyable {
		t.Fatalf("attributes %d buyable %v", len(d.Attributes), d.Buyable)
	}
	roundTrip(t, *d)
}

func TestProductWithStore(t *testing.T) {
	d, err := parseProduct(fixture(t, "product_25064668_store4.html.gz"), "/productos/x-25064668.html", StoreRef{ID: 4, Name: "Obramat Leganés"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Price == nil || d.Price.Amount != 28 || d.Price.PerMeasure == nil || d.Price.PerMeasure.Amount != 8.96 {
		t.Fatalf("price %+v", d.Price)
	}
	if d.Stock == nil || d.Stock.Quantity == nil || *d.Stock.Quantity != 205 {
		t.Fatalf("stock %+v", d.Stock)
	}
	roundTrip(t, *d)
}

func TestProductStockOnlyInStore(t *testing.T) {
	// available_deliveries is empty here; the quantity is only in the text.
	d, err := parseProduct(fixture(t, "product_25030393_store7.html.gz"), "/productos/x-25030393.html", StoreRef{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if d.Stock == nil || d.Stock.Quantity == nil || *d.Stock.Quantity != 5 || d.Store.Name != "Obramat Rivas" {
		t.Fatalf("stock %+v store %+v", d.Stock, d.Store)
	}
	if d.Price == nil || d.Price.Amount != 8.5 || d.Price.Unit != "Unidad" {
		t.Fatalf("price %+v", d.Price)
	}
}

func TestProductUnavailableAtStore(t *testing.T) {
	d, err := parseProduct(fixture(t, "product_25030393_store4_unavailable.html.gz"), "/productos/x-25030393.html", StoreRef{ID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if d.Stock == nil || d.Stock.Available || d.Stock.Quantity == nil || *d.Stock.Quantity != 0 {
		t.Fatalf("stock %+v", d.Stock)
	}
	if d.Price != nil {
		t.Fatalf("price for a store that does not sell it: %+v", d.Price)
	}
}

func TestNearby(t *testing.T) {
	stores, err := parseNearby([]byte(fixture(t, "stores_nearby_madrid.json")))
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 6 {
		t.Fatalf("%d stores", len(stores))
	}
	s := stores[0]
	if s.ID != 12 || s.Name != "Obramat Usera" || s.Slug != "obramat-usera" || s.Lat == 0 || s.Postcode != "28041" {
		t.Fatalf("%+v", s)
	}
	roundTrip(t, s)
}

func TestStockCards(t *testing.T) {
	rows, err := parseStockCards([]byte(fixture(t, "stock_25064668_madrid.html.gz")))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 || rows[0].Store.ID != 12 || rows[0].Store.Name != "Obramat Usera" || *rows[0].Stock.Quantity != 94 || *rows[0].DistanceKm != 11.2 {
		t.Fatalf("%+v", rows[0])
	}
	rows, err = parseStockCards([]byte(fixture(t, "stock_25030393_madrid.html.gz")))
	if err != nil {
		t.Fatal(err)
	}
	var out, in int
	for _, r := range rows {
		if r.Stock.Available {
			in++
		} else if r.Stock.Quantity != nil && *r.Stock.Quantity == 0 {
			out++
		}
	}
	if in != 1 || out != 5 {
		t.Fatalf("in=%d out=%d: %+v", in, out, rows)
	}
	roundTrip(t, rows)
}

func TestStorePage(t *testing.T) {
	s, err := parseStorePage(fixture(t, "store_obramat-usera.html.gz"), "obramat-usera")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != 12 || s.Name != "Obramat Usera" || s.Lat != 40.36506 {
		t.Fatalf("%+v", s)
	}
}

func TestSitemaps(t *testing.T) {
	slugs, err := parseStoreSitemap([]byte(fixture(t, "sitemap-store.xml.gz")))
	if err != nil || len(slugs) != 41 {
		t.Fatalf("%v %d", err, len(slugs))
	}
	cats, err := parseCategorySitemap([]byte(fixture(t, "sitemap-category.xml.gz")))
	if err != nil || len(cats) < 1000 {
		t.Fatalf("%v %d", err, len(cats))
	}
	var tools *Category
	for i := range cats {
		if cats[i].Path == "/herramientas/" {
			tools = &cats[i]
		}
	}
	if tools == nil || tools.Depth != 1 || tools.Children == 0 {
		t.Fatalf("%+v", tools)
	}
}
