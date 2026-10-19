package cli

import (
	"strings"
	"testing"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/state"
)

func TestStoreListFromCache(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	cacheStores(t, leganes, api.Store{ID: 12, Name: "Obramat Usera", Slug: "obramat-usera", Lat: 40.36506, Lng: -3.68818})
	out, errOut, code := run("store", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if stores := decode[[]api.Store](t, out); len(stores) != 2 {
		t.Fatalf("%+v", stores)
	}
	pretty, _, code := run("store", "list", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "Obramat Leganés") || !strings.Contains(pretty, "STORE") {
		t.Fatalf("%s", pretty)
	}
}

func TestStoreNearShowAndErrors(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/fox--microfront--store/api/v1/stores-nearby-by-distance": "stores_nearby_madrid.json",
	}))
	out, errOut, code := run("store", "near", "--lat", "40.42", "--lng", "-3.70")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if stores := decode[[]api.Store](t, out); len(stores) != 6 || stores[0].DistanceKm == nil {
		t.Fatalf("%+v", stores)
	}
	pretty, _, code := run("store", "near", "--lat", "40.42", "--lng", "-3.70", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "DISTANCE") {
		t.Fatalf("%s", pretty)
	}
	if _, _, code := run("store", "near"); code != 2 {
		t.Fatalf("missing coordinates: %d", code)
	}

	cacheStores(t, leganes)
	out, errOut, code = run("store", "show", "leganes")
	if code != 0 {
		t.Fatalf("show: exit %d: %s", code, errOut)
	}
	if stores := decode[[]api.Store](t, out); len(stores) != 1 || stores[0].ID != 4 {
		t.Fatalf("%+v", stores)
	}
	if _, _, code := run("store", "show", "nowhere"); code != 4 {
		t.Fatalf("unknown store: %d", code)
	}
}

func TestStoreStock(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/search":                    "redirect:/productos/x-25064668.html",
		"/productos/x-25064668.html": "product_25064668_store4.html.gz",
		"/store-header-module/services/contextlayer/store-search-result": "stock_25064668_madrid.html.gz",
	}))
	out, errOut, code := run("store", "stock", "25064668", "--store", "4")
	if code != 0 {
		t.Fatalf("single: exit %d: %s", code, errOut)
	}
	one := decode[storeStockView](t, out)
	if len(one.Stores) != 1 || one.Stores[0].Stock.Quantity == nil || *one.Stores[0].Stock.Quantity != 205 {
		t.Fatalf("%+v", one)
	}
	pretty, _, code := run("store", "stock", "25064668", "--store", "4", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "205") {
		t.Fatalf("%s", pretty)
	}

	out, errOut, code = run("store", "stock", "25064668", "--lat", "40.42", "--lng", "-3.70")
	if code != 0 {
		t.Fatalf("near: exit %d: %s", code, errOut)
	}
	if rows := decode[storeStockView](t, out); len(rows.Stores) != 6 {
		t.Fatalf("%+v", rows)
	}
	if _, _, code := run("store", "stock", "25064668", "--lat", "40.42"); code != 2 {
		t.Fatalf("half a point: %d", code)
	}
}

func TestStoreStockAllReportsWhatItMissed(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/store-header-module/services/contextlayer/store-search-result": "stock_25064668_madrid.html.gz",
	}))
	cacheStores(t, api.Store{ID: 12, Name: "Obramat Usera", Lat: 40.36506, Lng: -3.68818},
		api.Store{ID: 999, Name: "Obramat Nowhere"})
	out, errOut, code := run("store", "stock", "25064668", "--all")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if rows := decode[storeStockView](t, out); len(rows.Stores) != 6 {
		t.Fatalf("%+v", rows)
	}
	if !strings.Contains(errOut, "Obramat Nowhere") {
		t.Fatalf("missing-store notice: %q", errOut)
	}
}

func TestStoreStockNeedsATarget(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	if _, errOut, code := run("store", "stock", "25064668"); code != 2 || !strings.Contains(errOut, "say where") {
		t.Fatalf("exit %d %q", code, errOut)
	}
}

func TestWatchSearchLifecycle(t *testing.T) {
	isolate(t, site(t, map[string]string{"/search": "search_tablero-osb.html.gz"}))

	out, errOut, code := run("watch", "add", "search", "tablero osb", "--name", "osb", "--emit-initial")
	if code != 0 {
		t.Fatalf("add: exit %d: %s", code, errOut)
	}
	if events := decode[[]state.Event](t, out); len(events) != 8 {
		t.Fatalf("initial events: %d", len(events))
	}

	_, errOut, code = run("watch", "add", "search", "tablero osb", "--name", "quiet")
	if code != 0 {
		t.Fatalf("add quiet: exit %d: %s", code, errOut)
	}
	pretty, _, code := run("watch", "list", "--format", "pretty")
	if code != 0 || !strings.Contains(pretty, "osb") || !strings.Contains(pretty, "TARGET") {
		t.Fatalf("%s", pretty)
	}

	// Just checked, so nothing is due.
	quiet, _, code := run("watch", "check")
	if code != 0 || strings.TrimSpace(quiet) != "[]" {
		t.Fatalf("due check: exit %d %q", code, quiet)
	}
	if _, errOut, code := run("watch", "check", "--all"); code != 0 {
		t.Fatalf("--all: exit %d: %s", code, errOut)
	}
	if _, errOut, code := run("watch", "check", "osb"); code != 0 {
		t.Fatalf("named: exit %d: %s", code, errOut)
	}
	out, _, code = run("watch", "events", "--since", "24h", "--limit", "20")
	if code != 0 {
		t.Fatalf("events: %d", code)
	}
	if events := decode[[]state.Event](t, out); len(events) != 8 {
		t.Fatalf("stored events: %d", len(events))
	}
	if _, errOut, code := run("watch", "remove", "osb", "--yes"); code != 0 {
		t.Fatalf("remove: exit %d: %s", code, errOut)
	}
	if _, _, code := run("watch", "remove", "osb", "--yes"); code != 4 {
		t.Fatalf("remove twice: %d", code)
	}
}

func TestWatchAddProductAndErrors(t *testing.T) {
	isolate(t, site(t, map[string]string{
		"/search":                    "redirect:/productos/x-25064668.html",
		"/productos/x-25064668.html": "product_25064668.html.gz",
	}))
	out, errOut, code := run("watch", "add", "product", "25064668", "--name", "board")
	if code != 0 {
		t.Fatalf("add product: exit %d: %s", code, errOut)
	}
	if v := decode[watchView](t, out); v.Products != 1 {
		t.Fatalf("%+v", v)
	}
	if _, _, code := run("watch", "add", "product", "25064668", "--name", "board"); code != 2 {
		t.Fatalf("duplicate: %d", code)
	}
	if _, _, code := run("watch", "add", "search", "x", "--name", "bad name"); code != 2 {
		t.Fatalf("bad name: %d", code)
	}
	if _, _, code := run("watch", "add", "search", "x", "--name", "n", "--interval", "1m"); code != 2 {
		t.Fatalf("short interval: %d", code)
	}
	if _, _, code := run("watch", "add", "search", "x", "--name", "n2", "--notify", "nope"); code != 2 {
		t.Fatalf("unknown sink: %d", code)
	}
}

func TestWatchCheckStopsAtABlock(t *testing.T) {
	good := site(t, map[string]string{"/search": "search_tablero-osb.html.gz"})
	isolate(t, good)
	if _, errOut, code := run("watch", "add", "search", "tablero osb", "--name", "w"); code != 0 {
		t.Fatalf("add: %d %s", code, errOut)
	}
	t.Setenv("OBRAMAT_BASE_URL", site(t, map[string]string{"/search": "403:datadome_403.html"}))
	if _, errOut, code := run("watch", "check", "--all"); code != 5 || !strings.Contains(errOut, "bot protection") {
		t.Fatalf("blocked check: exit %d %q", code, errOut)
	}
}

func TestWatchServiceStatus(t *testing.T) {
	isolate(t, "http://127.0.0.1:1")
	t.Setenv("HOME", t.TempDir())
	out, errOut, code := run("watch", "service", "status")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if v := decode[serviceView](t, out); v.State != "not installed" {
		t.Fatalf("%+v", v)
	}
}
