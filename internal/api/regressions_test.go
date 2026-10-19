package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeSite serves fixtures by path and records the cookie of each request.
func fakeSite(t *testing.T, routes map[string]string) (*Client, *[]string) {
	t.Helper()
	var cookies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies = append(cookies, r.Header.Get("Cookie"))
		key := r.URL.Path
		if strings.HasPrefix(key, "/lazy/") {
			key = "/lazy/"
		}
		name, ok := routes[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "redirect:") {
			http.Redirect(w, r, strings.TrimPrefix(name, "redirect:"), http.StatusTemporaryRedirect)
			return
		}
		status := http.StatusOK
		if strings.HasPrefix(name, "403:") {
			status, name = http.StatusForbidden, strings.TrimPrefix(name, "403:")
			w.Header().Set("X-Datadome", "protected")
		}
		w.WriteHeader(status)
		w.Write([]byte(fixture(t, name)))
	}))
	t.Cleanup(srv.Close)
	c := New("test")
	c.Base, c.Gap = srv.URL, 0
	return c, &cookies
}

// Regression: the store cookie needs a non-empty store name. With only
// main_store the site still renders product pages for the store, but listings
// lose most prices (5 of 22 cards priced instead of 20, observed 2026-10-19).
func TestStoreCookieCarriesName(t *testing.T) {
	raw := StoreRef{ID: 4}.cookie()
	v, err := url.QueryUnescape(strings.TrimPrefix(raw, "customer_context="))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		MainStore  string                      `json:"main_store"`
		StoresName []struct{ ID, Name string } `json:"stores_name"`
	}
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		t.Fatal(err)
	}
	if c.MainStore != "4" || len(c.StoresName) != 1 || c.StoresName[0].ID != "4" || c.StoresName[0].Name == "" {
		t.Fatalf("cookie %s", v)
	}
}

// Regression: every normal page names captcha-delivery.com in its CSP, so
// that string alone must not be read as a block.
func TestNormalPageIsNotABlock(t *testing.T) {
	c, _ := fakeSite(t, map[string]string{"/search": "search_tablero-osb.html.gz"})
	if _, err := c.List(context.Background(), ListParams{Query: "tablero osb"}); err != nil {
		t.Fatalf("normal page: %v", err)
	}
}

func TestBlockIsExit5(t *testing.T) {
	for _, body := range []string{"datadome_403.html", "datadome_403.json"} {
		c, _ := fakeSite(t, map[string]string{"/search": "403:" + body})
		_, err := c.List(context.Background(), ListParams{Query: "x"})
		e, ok := err.(*Error)
		if !ok || e.Kind != KindBlocked || e.Kind.ExitCode() != 5 || !e.Retryable {
			t.Fatalf("%s: %v", body, err)
		}
	}
}

// Regression: the nearby API's top-level id with isCcdp=false is an internal
// number (Usera 53); the store code the cookie and stock cards use is the
// poiId (Usera 12). Stores must be keyed by the code.
func TestStoreIDIsTheStoreCode(t *testing.T) {
	stores, err := parseNearby([]byte(fixture(t, "stores_nearby_madrid.json")))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stores {
		if s.Name == "Obramat Usera" && s.ID != 12 {
			t.Fatalf("Usera id %d, want the store code 12", s.ID)
		}
	}
}

// Regression: listings render about a third of their cards as placeholders
// that the browser fills from /lazy/ fragments. List must fetch those, with
// the same store cookie, and keep the site's ranking order.
func TestListFetchesLazyCards(t *testing.T) {
	c, cookies := fakeSite(t, map[string]string{
		"/search": "search_cemento_store4.html.gz",
		"/lazy/":  "lazy_cemento_store4.html.gz",
	})
	store := StoreRef{ID: 4, Name: "Obramat Leganés"}
	l, err := c.List(context.Background(), ListParams{Query: "cemento", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 204 || len(l.Products) != 48 { // store 4 sells 204 of the 657
		t.Fatalf("total %d products %d", l.Total, len(l.Products))
	}
	named := 0
	for _, p := range l.Products {
		if p.Title != "" {
			named++
		}
		if p.Store == nil || p.Store.ID != 4 {
			t.Fatalf("product %s without the store context", p.Ref)
		}
	}
	// The fake serves one fragment for every lazy URL, so only the groups it
	// covers fill in; the eager cards plus one group must.
	if named < 30 {
		t.Fatalf("only %d of 48 products have a title", named)
	}
	for i, ck := range *cookies {
		if !strings.Contains(ck, "customer_context=") {
			t.Fatalf("request %d sent without the store cookie", i)
		}
	}
}

// Regression: --store 99 (no such store) was trusted, and the site's default
// web prices were reported as store 99's. The site renders an unknown code
// as no store at all; the page then lacks data-store-id for it.
func TestUnknownStoreIsRefused(t *testing.T) {
	c, _ := fakeSite(t, map[string]string{
		"/search":                    "search_tablero-osb.html.gz",
		"/productos/x-25064668.html": "product_25064668.html.gz",
	})
	ctx := context.Background()
	if _, err := c.List(ctx, ListParams{Query: "osb", Store: StoreRef{ID: 99}}); !isKind(err, KindUsage) {
		t.Fatalf("listing: %v", err)
	}
	if _, err := c.Product(ctx, "/productos/x-25064668.html", StoreRef{ID: 99}); !isKind(err, KindUsage) {
		t.Fatalf("product: %v", err)
	}
	// A known store passes: its pages carry data-store-id="004".
	c4, _ := fakeSite(t, map[string]string{"/search": "search_tablero-osb_store4.html.gz"})
	if _, err := c4.List(ctx, ListParams{Query: "osb", Store: StoreRef{ID: 4}}); err != nil {
		t.Fatal(err)
	}
}

func isKind(err error, k Kind) bool {
	e, ok := err.(*Error)
	return ok && e.Kind == k
}

func TestExactReferenceRedirectsToProduct(t *testing.T) {
	c, _ := fakeSite(t, map[string]string{
		"/search": "redirect:/productos/tablero-osb3-250x125x1-5cm-25064668.html",
		"/productos/tablero-osb3-250x125x1-5cm-25064668.html": "product_25064668.html.gz",
	})
	l, err := c.List(context.Background(), ListParams{Query: "25064668"})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Products) != 1 || l.Products[0].Ref != "25064668" {
		t.Fatalf("%+v", l)
	}
	path, err := c.ProductPath(context.Background(), "25064668")
	if err != nil || path != "/productos/tablero-osb3-250x125x1-5cm-25064668.html" {
		t.Fatalf("%q %v", path, err)
	}
}

func TestProduct404(t *testing.T) {
	c, _ := fakeSite(t, map[string]string{})
	_, err := c.Product(context.Background(), "https://www.obramat.es/productos/no-existe-99999999.html", StoreRef{})
	if e, ok := err.(*Error); !ok || e.Kind != KindNotFound {
		t.Fatalf("%v", err)
	}
}

func TestLandingCategoryIsUsageError(t *testing.T) {
	c, _ := fakeSite(t, map[string]string{"/madera/": "category_madera_landing.html.gz"})
	_, err := c.List(context.Background(), ListParams{Category: "/madera/"})
	if e, ok := err.(*Error); !ok || e.Kind != KindUsage {
		t.Fatalf("%v", err)
	}
}

func TestCategoryPathNormalises(t *testing.T) {
	for in, want := range map[string]string{
		"madera/tableros":                         "/madera/tableros/",
		"https://www.obramat.es/madera/tableros/": "/madera/tableros/",
	} {
		if got, err := CategoryPath(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := CategoryPath("/productos/x-1.html"); err == nil {
		t.Error("product path accepted as a category")
	}
}

func TestSlugify(t *testing.T) {
	if got := Slugify("Obramat Alcalá de Guadaíra "); got != "obramat-alcala-de-guadaira" {
		t.Fatal(got)
	}
}
