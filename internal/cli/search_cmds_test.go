package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
)

func TestSearchJSONAndPretty(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))
	out, errOut, code := run("search", "tablero osb")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	l := decode[api.Listing](t, out)
	if len(l.Products) != 8 || l.Total != 8 || l.Query != "tablero osb" {
		t.Fatalf("%+v", l)
	}
	pretty, _, code := run("search", "tablero osb", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "REF") || !strings.Contains(pretty, "8 of 8 products") {
		t.Fatalf("pretty: %d\n%s", code, pretty)
	}
}

func TestSearchSendsTheFilters(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(fixture(t, "search_cemento_weber_5-20_price-asc.html.gz")))
	}))
	defer srv.Close()
	isolate(t, srv.URL)
	if _, errOut, code := run("search", "cemento", "--brand", "weber", "--min-price", "5", "--max-price", "20",
		"--sort", "price_asc", "--filter", "attribute-00596=15", "--page", "2"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	want := `{"attribute-00596":"15","attribute-06575":"WEBER","range-price-max":"20","range-price-min":"5"}`
	if got.Get("filters") != want || got.Get("sort") != "price-asc" || got.Get("p") != "2" {
		t.Fatalf("query %v", got)
	}
}

func TestSearchPagesAndLimit(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/search": "search_cemento_store4.html.gz",
		"/lazy/":  "lazy_cemento_store4.html.gz",
	}))
	out, errOut, code := run("search", "cemento", "--pages", "2", "--limit", "3", "--format", "jsonl")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d products:\n%s", len(lines), out)
	}
}

func TestSearchInStockUsesTheSiteFilter(t *testing.T) {
	var filters string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filters = r.URL.Query().Get("filters")
		w.Write([]byte(fixture(t, "search_tablero-osb_store22_in-stock.html.gz")))
	}))
	defer srv.Close()
	isolate(t, srv.URL)
	pretty, errOut, code := run("search", "tablero osb", "--store", "22", "--in-stock", "--format", "pretty")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if filters != `{"availability":"inStockInStore"}` {
		t.Fatalf("filters = %s", filters)
	}
	if !strings.Contains(pretty, "4 of 4 products") || !strings.Contains(pretty, "Obramat Salamanca") {
		t.Fatalf("%s", pretty)
	}
}

func TestSearchUsageErrors(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	cases := [][]string{
		{"search"},
		{"search", "x", "--category", "/madera/"},
		{"search", "x", "--sort", "cheapest"},
		{"search", "x", "--min-price", "10", "--max-price", "5"},
		{"search", "x", "--filter", "bad"},
		{"search", "x", "--filter", "price=1"},
		{"search", "x", "--in-stock"},
		{"search", "x", "--page", "0"},
		{"search", "x", "--pages", "0"},
		{"search", "--category", "/almacenes/x/"},
		{"search", "--category", "/productos/x.html"},
		{"search", "--category", "http://evil.example/x/"},
	}
	for _, args := range cases {
		if _, errOut, code := run(args...); code != 2 {
			t.Errorf("%v: exit %d (%s)", args, code, errOut)
		}
	}
}

func TestSearchCategoryAndLandingPage(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/madera/tableros/tableros-y-tablas-de-madera/": "category_tableros-y-tablas_store4.html.gz",
	}))
	out, errOut, code := run("search", "--category", "/madera/tableros/tableros-y-tablas-de-madera/", "--store", "4")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if l := decode[api.Listing](t, out); l.Category == "" || len(l.Products) == 0 {
		t.Fatalf("%+v", l)
	}
	isolate(t, site(t, map[string]string{"/madera/": "category_madera_landing.html.gz"}))
	_, errOut, code = run("search", "--category", "/madera/")
	if code != 2 || !strings.Contains(errOut, "landing page") {
		t.Fatalf("landing: exit %d %q", code, errOut)
	}
}

func TestSearchPastTheLastPage(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_cemento_p11_dependency-error.html.gz"}))
	_, errOut, code := run("search", "cemento", "--page", "11")
	if code != 4 || !strings.Contains(errOut, "no page 11") {
		t.Fatalf("exit %d %q", code, errOut)
	}
}

func TestSearchFiltersCommand(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_cemento_weber_5-20_price-asc.html.gz"}))
	out, errOut, code := run("search", "filters", "cemento")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var filters []api.Filter
	if err := json.Unmarshal([]byte(out), &filters); err != nil || len(filters) == 0 {
		t.Fatalf("%v %s", err, out)
	}
	pretty, _, code := run("search", "filters", "cemento", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "CODE") || !strings.Contains(pretty, "Marca") {
		t.Fatalf("%s", pretty)
	}
}

func TestProductShow(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/search":                        "redirect:/productos/x-25064668.html",
		"/productos/x-25064668.html":     "product_25064668.html.gz",
		"/productos/store-25064668.html": "product_25064668_store4.html.gz",
	}))
	out, errOut, code := run("product", "show", "25064668")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	d := decode[api.ProductDetail](t, out)
	if d.Ref != "25064668" || d.EAN == "" || d.Price == nil {
		t.Fatalf("%+v", d)
	}
	pretty, _, code := run("product", "show", "25064668", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "ref") || !strings.Contains(pretty, "ean") {
		t.Fatalf("%s", pretty)
	}
	// A store page has stock.
	out, errOut, code = run("product", "show", "https://www.obramat.es/productos/store-25064668.html", "--store", "4")
	if code != 0 {
		t.Fatalf("store: exit %d: %s", code, errOut)
	}
	if d := decode[api.ProductDetail](t, out); d.Stock == nil || d.Stock.Quantity == nil || *d.Stock.Quantity != 205 {
		t.Fatalf("%+v", d.Stock)
	}
}

func TestProductShowErrors(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/search":                   "search_tablero-osb.html.gz",
		"/productos/missing-1.html": "404:product_404.html.gz",
	}))
	if _, _, code := run("product", "show", "notaref"); code != 2 {
		t.Fatalf("bad argument: exit %d", code)
	}
	if _, errOut, code := run("product", "show", "25064668"); code != 4 || !strings.Contains(errOut, "no product") {
		t.Fatalf("no redirect: exit %d %q", code, errOut)
	}
	if _, _, code := run("product", "show", "/productos/missing-1.html"); code != 4 {
		t.Fatalf("404: exit %d", code)
	}
}

func TestCategoryList(t *testing.T) {
	isolate(t, site(t, map[string]string{"/sitemap-category1.xml": "sitemap-category.xml.gz"}))
	out, errOut, code := run("category", "list", "--depth", "1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	cats := decode[[]api.Category](t, out)
	if len(cats) == 0 {
		t.Fatal("no categories")
	}
	for _, c := range cats {
		if c.Depth > 1 {
			t.Fatalf("depth filter: %+v", c)
		}
	}
	out, _, code = run("category", "list", "--parent", "/madera/")
	if code != 0 {
		t.Fatalf("parent: %d", code)
	}
	for _, c := range decode[[]api.Category](t, out) {
		if c.Parent != "/madera/" {
			t.Fatalf("parent filter: %+v", c)
		}
	}
	pretty, _, code := run("category", "list", "--parent", "/madera/", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "CATEGORY") {
		t.Fatalf("%s", pretty)
	}
	if _, _, code := run("category", "list", "--parent", "/almacenes/x/"); code != 2 {
		t.Fatalf("bad parent: %d", code)
	}
}
