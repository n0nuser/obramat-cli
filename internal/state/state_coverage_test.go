package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestWatchLifecycle(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	w, err := db.AddWatch(ctx, Watch{Name: "b", Kind: KindSearch, Target: json.RawMessage(`{"params":{}}`),
		Sinks: []string{"phone"}, Interval: 2 * time.Hour, Pages: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !w.CreatedAt.After(time.Time{}) || w.ID == 0 {
		t.Fatalf("%+v", w)
	}
	if _, err := db.AddWatch(ctx, Watch{Name: "a", Kind: KindProduct, Target: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	ws, err := db.Watches(ctx)
	if err != nil || len(ws) != 2 || ws[0].Name != "a" || ws[1].Name != "b" {
		t.Fatalf("%+v %v", ws, err)
	}
	if len(ws[1].Sinks) != 1 || ws[1].Sinks[0] != "phone" || ws[1].Interval != 2*time.Hour || ws[1].Pages != 3 {
		t.Fatalf("stored watch lost fields: %+v", ws[1])
	}
	if _, err := db.Watch(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing watch: %v", err)
	}
	if err := db.RemoveWatch(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing remove: %v", err)
	}
}

func TestCommitCheckUpdatesAndRemoves(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	w, err := db.AddWatch(ctx, Watch{Name: "w", Kind: KindProduct, Target: json.RawMessage(`{}`), Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	first, second := 1.0, 2.0
	if err := db.CommitCheck(ctx, w.ID, []Seen{
		{Ref: "1", Title: "one", Price: &first, Snapshot: json.RawMessage(`{}`)},
		{Ref: "2", Title: "two", Snapshot: json.RawMessage(`{}`)},
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitCheck(ctx, w.ID, []Seen{
		{Ref: "1", Title: "one", Price: &second, Snapshot: json.RawMessage(`{}`)},
	}, []string{"2"}, nil); err != nil {
		t.Fatal(err)
	}
	seen, err := db.SeenFor(ctx, w.ID)
	if err != nil || len(seen) != 1 || *seen["1"].Price != 2 {
		t.Fatalf("%+v %v", seen, err)
	}
}

func TestEventsFilters(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	a, _ := db.AddWatch(ctx, Watch{Name: "a", Kind: KindProduct, Target: json.RawMessage(`{}`)})
	b, _ := db.AddWatch(ctx, Watch{Name: "b", Kind: KindProduct, Target: json.RawMessage(`{}`)})
	now := time.Now().UTC()
	for _, ev := range []struct {
		id  int64
		at  time.Time
		typ string
	}{
		{a.ID, now.Add(-3 * time.Hour), ProductNew},
		{a.ID, now, PriceChanged},
		{b.ID, now, ProductRemoved},
	} {
		if err := db.CommitCheck(ctx, ev.id, nil, nil, []Event{{Type: ev.typ, Watch: "x", At: ev.at}}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := db.Events(ctx, "", time.Time{}, 10)
	if len(all) != 3 {
		t.Fatalf("all: %+v", all)
	}
	onlyA, _ := db.Events(ctx, "a", time.Time{}, 10)
	if len(onlyA) != 2 {
		t.Fatalf("watch a: %+v", onlyA)
	}
	recent, _ := db.Events(ctx, "", now.Add(-time.Hour), 10)
	if len(recent) != 2 {
		t.Fatalf("since: %+v", recent)
	}
	one, _ := db.Events(ctx, "", time.Time{}, 1)
	if len(one) != 1 {
		t.Fatalf("limit: %+v", one)
	}
	if one[0].Product != nil || one[0].Change != nil {
		t.Fatalf("empty payload fields became non-nil: %+v", one[0])
	}
}

func TestOpenRefusesABadParent(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(parent, "state.db")); err == nil {
		t.Fatal("opened a database under a regular file")
	}
}
