package watch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/state"
)

func ptr[T any](v T) *T { return &v }

func TestDiffReportsPriceAndAvailability(t *testing.T) {
	w := state.Watch{Name: "w"}
	p := api.Product{Ref: "1", Stock: &api.Stock{}}
	prev := state.Seen{Price: ptr(10.0), Quantity: ptr(5), Available: true}
	cur := state.Seen{Price: ptr(12.0), Quantity: ptr(0), Available: false}
	evs := diff(w, prev, cur, p, true)
	if len(evs) != 2 || evs[0].Type != PriceChanged || evs[1].Type != OutOfStock {
		t.Fatalf("%+v", evs)
	}
	var ch struct{ From, To float64 }
	_ = json.Unmarshal(evs[0].Change, &ch)
	if ch.From != 10 || ch.To != 12 {
		t.Fatalf("change %s", evs[0].Change)
	}
	back := diff(w, cur, prev, p, true)
	if len(back) != 2 || back[1].Type != BackInStock {
		t.Fatalf("%+v", back)
	}
}

// Quantities change with every sale: only product watches report them.
func TestQuantityOnlyForProductWatches(t *testing.T) {
	w := state.Watch{Name: "w"}
	p := api.Product{Ref: "1", Stock: &api.Stock{}}
	prev := state.Seen{Quantity: ptr(50), Available: true}
	cur := state.Seen{Quantity: ptr(49), Available: true}
	if evs := diff(w, prev, cur, p, false); len(evs) != 0 {
		t.Fatalf("search watch reported %+v", evs)
	}
	if evs := diff(w, prev, cur, p, true); len(evs) != 1 || evs[0].Type != StockChanged {
		t.Fatalf("product watch: %+v", evs)
	}
}

// No store: the web price is followed, stock flags never compared.
func TestNoStoreNoStockEvents(t *testing.T) {
	evs := diff(state.Watch{}, state.Seen{Available: true}, state.Seen{}, api.Product{Ref: "1"}, true)
	if len(evs) != 0 {
		t.Fatalf("%+v", evs)
	}
}

// End to end against the captured search page: the first check baselines
// silently, a second one with the same page reports nothing.
func TestSearchWatchBaselinesThenStaysQuiet(t *testing.T) {
	page := readFixture(t, "search_tablero-osb.html.gz")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(page) }))
	defer srv.Close()
	c := api.New("test")
	c.Base, c.Gap = srv.URL, 0
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	target, _ := json.Marshal(SearchTarget{Params: api.ListParams{Query: "tablero osb"}})
	w, err := db.AddWatch(ctx, state.Watch{Name: "osb", Kind: state.KindSearch, Target: target, Interval: time.Hour, Pages: 1})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, err := Check(ctx, c, db, w, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Events) != 0 || len(res.Upserts) != 8 {
			t.Fatalf("check %d: %d events, %d seen", i, len(res.Events), len(res.Upserts))
		}
		if err := db.CommitCheck(ctx, w.ID, res.Upserts, nil, res.Events); err != nil {
			t.Fatal(err)
		}
		w, _ = db.Watch(ctx, "osb")
	}
	seen, _ := db.SeenFor(ctx, w.ID)
	if s := seen["25064668"]; s.Price == nil || *s.Price != 23.89 || !strings.Contains(string(s.Snapshot), "TABLERO") {
		t.Fatalf("%+v", s)
	}
}
