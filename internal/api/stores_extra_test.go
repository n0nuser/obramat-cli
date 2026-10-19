package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStoreEndpoints(t *testing.T) {
	ctx := context.Background()
	c, _ := fakeSite(t, map[string]string{
		"/fox--microfront--store/api/v1/stores-nearby-by-distance": "stores_nearby_madrid.json",
		"/sitemap-store1.xml":           "sitemap-store.xml.gz",
		"/almacenes/obramat-usera.html": "store_obramat-usera.html.gz",
		"/store-header-module/services/contextlayer/store-search-result": "stock_25064668_madrid.html.gz",
		"/sitemap-category1.xml": "sitemap-category.xml.gz",
	})

	stores, err := c.Nearby(ctx, 40.42, -3.70)
	if err != nil || len(stores) != 6 || stores[0].ID != 12 || stores[0].Lat == 0 {
		t.Fatalf("nearby: %+v %v", stores, err)
	}
	if got := stores[0].Ref(); got.ID != 12 || got.Name != "Obramat Usera" {
		t.Fatalf("ref: %+v", got)
	}
	slugs, err := c.StoreSlugs(ctx)
	if err != nil || len(slugs) != 41 {
		t.Fatalf("slugs: %d %v", len(slugs), err)
	}
	s, err := c.StorePage(ctx, "obramat-usera")
	if err != nil || s.ID != 12 || s.Slug != "obramat-usera" {
		t.Fatalf("store page: %+v %v", s, err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) < 1000 {
		t.Fatalf("categories: %d %v", len(cats), err)
	}
	rows, err := c.StockNear(ctx, "25064668", 40.42, -3.70)
	if err != nil || len(rows) != 6 || rows[0].Store.ID != 12 || rows[0].DistanceKm == nil {
		t.Fatalf("stock near: %+v %v", rows, err)
	}
}

// AllStores sweeps the nearby endpoint and falls back to each missed store's
// page. Both are local responses here.
func TestAllStores(t *testing.T) {
	nearby := fixture(t, "stores_nearby_madrid.json")
	sitemap := fixture(t, "sitemap-store.xml.gz")
	page := fixture(t, "store_obramat-usera.html.gz")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sitemap-store1.xml":
			w.Write([]byte(sitemap))
		case r.URL.Path == "/fox--microfront--store/api/v1/stores-nearby-by-distance":
			w.Write([]byte(nearby))
		case strings.HasPrefix(r.URL.Path, "/almacenes/"):
			w.Write([]byte(page))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New("test")
	c.Base, c.Gap, c.SweepGap = srv.URL, 0, 0
	all, err := c.AllStores(context.Background())
	if err != nil || len(all) != 6 {
		t.Fatalf("%d stores, %v", len(all), err)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Fatalf("not sorted: %q > %q", all[i-1].Name, all[i].Name)
		}
	}
	if all[0].DistanceKm != nil || all[0].Hours != "" {
		t.Fatalf("sweep left per-point fields: %+v", all[0])
	}
}

// StockEverywhere asks around each store until every store has answered.
func TestStockEverywhere(t *testing.T) {
	stock := fixture(t, "stock_25064668_madrid.html.gz")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(stock))
	}))
	defer srv.Close()
	c := New("test")
	c.Base, c.Gap, c.SweepGap = srv.URL, 0, 0
	stores := []Store{{ID: 12, Lat: 40.4, Lng: -3.7}, {ID: 4, Lat: 40.3, Lng: -3.7}, {ID: 999}}
	rows, err := c.StockEverywhere(context.Background(), "25064668", stores)
	if err != nil || len(rows) != 6 {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
	for _, r := range rows {
		if r.DistanceKm != nil {
			t.Fatalf("distance left in a sweep row: %+v", r)
		}
	}
}

func TestStoreParseErrors(t *testing.T) {
	if _, err := parseNearby([]byte(`{"stores":[{"name":""}]}`)); err == nil {
		t.Fatal("a store without poiId or name was accepted")
	}
	if _, err := parseNearby([]byte(`{`)); err == nil {
		t.Fatal("broken nearby JSON was accepted")
	}
	if _, err := parseStorePage("<html></html>", "x"); err == nil {
		t.Fatal("a page without jsonld was accepted")
	}
	noID := strings.ReplaceAll(fixture(t, "store_obramat-usera.html.gz"), `data-store-id="12"`, "")
	if _, err := parseStorePage(noID, "x"); err == nil {
		t.Fatal("a page without a store id was accepted")
	}
	if _, err := parseStoreSitemap([]byte("<urlset></urlset>")); err == nil {
		t.Fatal("an empty sitemap was accepted")
	}
	if _, err := parseStockCards([]byte("<html></html>")); err == nil {
		t.Fatal("a body without stock cards was accepted")
	}
}

func TestKindNamesAndCodes(t *testing.T) {
	for k, want := range map[Kind]string{
		KindGeneric: "generic", KindUsage: "usage", KindAuth: "auth", KindNotFound: "not_found",
		KindBlocked: "blocked", KindNetwork: "network", KindAPIChanged: "api_changed", Kind(99): "",
	} {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
	for k, want := range map[Kind]int{
		KindGeneric: 1, KindUsage: 2, KindAuth: 3, KindNotFound: 4,
		KindBlocked: 5, KindNetwork: 6, KindAPIChanged: 7, Kind(99): 1,
	} {
		if got := k.ExitCode(); got != want {
			t.Errorf("Kind(%d).ExitCode() = %d, want %d", k, got, want)
		}
	}
	err := &Error{Kind: KindBlocked, Msg: "blocked"}
	if err.Error() != "blocked" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestSummarizeAndCookieString(t *testing.T) {
	s := &Session{
		UserAgent: "UA/1",
		Cookies: []Cookie{
			{Name: "datadome", Value: "secret", Domain: ".obramat.es", Expires: time.Now().Add(time.Hour)},
			{Name: "customer_context", Value: "x", Domain: ".obramat.es"},
		},
	}
	sum := s.Summarize()
	if sum.Cookies != 2 || !sum.HasDataDome || sum.Expires == nil || sum.UserAgent != "UA/1" {
		t.Fatalf("%+v", sum)
	}
	if strings.Contains(strings.Join(sum.Names, ","), "secret") {
		t.Fatal("a value leaked into the names")
	}
	if got := (Cookie{Name: "datadome", Value: "secret", Domain: ".obramat.es"}).String(); strings.Contains(got, "secret") {
		t.Fatalf("Cookie.String leaked: %q", got)
	}
	if sum := (&Session{}).Summarize(); sum.HasDataDome || sum.Expires != nil {
		t.Fatalf("empty session: %+v", sum)
	}
}

func TestDebugLogsRequests(t *testing.T) {
	var log bytes.Buffer
	c, _ := fakeSite(t, map[string]string{"/search": "search_tablero-osb.html.gz"})
	c.Debug = &log
	if _, err := c.List(context.Background(), ListParams{Query: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "> GET") || !strings.Contains(log.String(), "< 200") {
		t.Fatalf("debug log:\n%s", log.String())
	}
}

func TestSweepRaisesTheGap(t *testing.T) {
	c := New("test")
	c.Gap = time.Millisecond
	c.SweepGap = 10 * time.Millisecond
	restore := c.sweep()
	if c.Gap != c.SweepGap {
		t.Fatalf("gap inside a sweep = %s", c.Gap)
	}
	restore()
	if c.Gap != time.Millisecond {
		t.Fatalf("gap after the sweep = %s", c.Gap)
	}
}
