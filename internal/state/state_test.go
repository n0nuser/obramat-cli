package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestCommitAndEvents(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	w, err := db.AddWatch(ctx, Watch{Name: "a", Kind: KindProduct, Target: json.RawMessage(`{}`), Interval: time.Hour, Pages: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddWatch(ctx, Watch{Name: "a", Kind: KindProduct, Target: json.RawMessage(`{}`)}); err != ErrExists {
		t.Fatalf("duplicate: %v", err)
	}
	q, p := 5, 9.5
	ev := Event{Type: "product.new", Watch: "a", At: time.Now(), Product: json.RawMessage(`{"ref":"1"}`)}
	if err := db.CommitCheck(ctx, w.ID, []Seen{{Ref: "1", Title: "x", Price: &p, Quantity: &q, Available: true, Snapshot: json.RawMessage(`{}`)}}, nil, []Event{ev}); err != nil {
		t.Fatal(err)
	}
	seen, _ := db.SeenFor(ctx, w.ID)
	if s := seen["1"]; *s.Price != 9.5 || *s.Quantity != 5 || !s.Available {
		t.Fatalf("%+v", s)
	}
	evs, _ := db.Events(ctx, "", time.Time{}, 10)
	if len(evs) != 1 || evs[0].Watch != "a" {
		t.Fatalf("%+v", evs)
	}
	got, _ := db.Watch(ctx, "a")
	if !got.Baselined || got.LastCheckAt.IsZero() {
		t.Fatalf("%+v", got)
	}
	if err := db.RemoveWatch(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if evs, _ := db.Events(ctx, "", time.Time{}, 10); len(evs) != 0 {
		t.Fatal("events outlived their watch")
	}
}
