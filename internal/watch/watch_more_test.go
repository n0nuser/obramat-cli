package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/state"
)

func newDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newClient(t *testing.T, h http.HandlerFunc) *api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := api.New("test")
	c.Base, c.Gap = srv.URL, 0
	return c
}

func addWatch(t *testing.T, db *state.DB, kind string, target any, pages int) state.Watch {
	t.Helper()
	raw, _ := json.Marshal(target)
	w, err := db.AddWatch(context.Background(), state.Watch{Name: "w", Kind: kind, Target: raw,
		Interval: time.Hour, Pages: pages})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// A product watch baselines, stays quiet, then reports the page disappearing.
func TestProductWatchFlow(t *testing.T) {
	ctx := context.Background()
	var gone atomic.Bool
	page := readFixture(t, "product_25064668_store4.html.gz")
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if gone.Load() {
			http.NotFound(w, r)
			return
		}
		w.Write(page)
	})
	db := newDB(t)
	target := ProductTarget{Ref: "25064668", Path: "/productos/x-25064668.html", Store: api.StoreRef{ID: 4, Name: "Obramat Leganés"}}
	w := addWatch(t, db, state.KindProduct, target, 1)

	res, err := Check(ctx, c, db, w, false)
	if err != nil || len(res.Events) != 0 || len(res.Upserts) != 1 {
		t.Fatalf("baseline: %+v %v", res, err)
	}
	if err := db.CommitCheck(ctx, w.ID, res.Upserts, res.Removed, res.Events); err != nil {
		t.Fatal(err)
	}
	w, _ = db.Watch(ctx, "w")

	res, err = Check(ctx, c, db, w, false)
	if err != nil || len(res.Events) != 0 {
		t.Fatalf("unchanged: %+v %v", res, err)
	}
	w, _ = db.Watch(ctx, "w")

	gone.Store(true)
	res, err = Check(ctx, c, db, w, false)
	if err != nil || len(res.Events) != 1 || res.Events[0].Type != state.ProductRemoved {
		t.Fatalf("removed: %+v %v", res, err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "25064668" {
		t.Fatalf("removed refs: %v", res.Removed)
	}
}

// emitInitial reports what exists now as new.
func TestProductWatchEmitInitial(t *testing.T) {
	ctx := context.Background()
	page := readFixture(t, "product_25064668.html.gz")
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write(page) })
	db := newDB(t)
	w := addWatch(t, db, state.KindProduct, ProductTarget{Ref: "25064668", Path: "/productos/x.html"}, 1)
	res, err := Check(ctx, c, db, w, true)
	if err != nil || len(res.Events) != 1 || res.Events[0].Type != state.ProductNew {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSearchWatchPages(t *testing.T) {
	ctx := context.Background()
	page := readFixture(t, "search_cemento_store4.html.gz")
	lazy := readFixture(t, "lazy_cemento_store4.html.gz")
	var searches atomic.Int32
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			searches.Add(1)
			w.Write(page)
			return
		}
		w.Write(lazy)
	})
	db := newDB(t)
	w := addWatch(t, db, state.KindSearch, SearchTarget{Params: api.ListParams{Query: "cemento", Store: api.StoreRef{ID: 4}}}, 2)
	res, err := Check(ctx, c, db, w, false)
	if err != nil || len(res.Upserts) == 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if searches.Load() != 2 {
		t.Fatalf("%d search requests, want 2 pages", searches.Load())
	}
}

// An in-stock search watch drops the cards the site returned without a stock
// line.
func TestSearchWatchInStockSkipsUnstocked(t *testing.T) {
	ctx := context.Background()
	page := readFixture(t, "search_tablero-osb.html.gz") // no store: no stock lines
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write(page) })
	db := newDB(t)
	w := addWatch(t, db, state.KindSearch, SearchTarget{Params: api.ListParams{Query: "tablero osb", InStock: true}}, 1)
	res, err := Check(ctx, c, db, w, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Upserts) != 0 {
		t.Fatalf("cards without stock were kept: %v", res.Upserts)
	}
}

func TestCheckRejectsCorruptTargets(t *testing.T) {
	ctx := context.Background()
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {})
	db := newDB(t)
	for _, kind := range []string{state.KindSearch, state.KindProduct} {
		w, err := db.AddWatch(ctx, state.Watch{Name: kind, Kind: kind, Target: json.RawMessage(`{`), Interval: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Check(ctx, c, db, w, false); err == nil {
			t.Errorf("%s: a corrupt target was accepted", kind)
		}
	}
	w, err := db.AddWatch(ctx, state.Watch{Name: "x", Kind: "other", Target: json.RawMessage(`{}`), Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, c, db, w, false); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
}

func TestJitter(t *testing.T) {
	if got := Jitter(time.Second, 0); got != 0 {
		t.Fatalf("Jitter zero = %s", got)
	}
	if got := Jitter(time.Second, 5); got <= 0 || got >= time.Second/10 {
		t.Fatalf("Jitter = %s", got)
	}
	if got := Jitter(time.Second, -5); got <= 0 {
		t.Fatalf("negative seed Jitter = %s", got)
	}
	if got := Jitter(time.Nanosecond, 1); got != 0 {
		t.Fatalf("tiny interval Jitter = %s", got)
	}
}
