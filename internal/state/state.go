// Package state is the local watch database: watches, the last state each
// watch saw of every product, and the events its checks produced. SQLite
// through modernc.org/sqlite, so the binary stays cgo-free.
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	KindSearch  = "search"
	KindProduct = "product"
)

// Watch is a saved instruction to check one target. Target is kind-specific
// JSON (search parameters, or a product reference), and always pins the
// store, so changing the default store never makes a watch report changes.
type Watch struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Target      json.RawMessage `json:"target"`
	Sinks       []string        `json:"sinks"`
	Interval    time.Duration   `json:"interval"`
	Pages       int             `json:"pages"`
	CreatedAt   time.Time       `json:"created_at"`
	LastCheckAt time.Time       `json:"last_check_at,omitzero"`
	Baselined   bool            `json:"baselined"`
}

// Seen is what a watch last knew about one product. Price and Quantity are
// nil when the site showed none.
type Seen struct {
	Ref       string
	Title     string
	Price     *float64
	Quantity  *int
	Available bool
	Snapshot  json.RawMessage
}

type Event struct {
	Type   string          `json:"type"`
	Watch  string          `json:"watch"`
	At     time.Time       `json:"at"`
	Item   json.RawMessage `json:"product,omitempty"`
	Change json.RawMessage `json:"change,omitempty"`
}

const schema = `
CREATE TABLE IF NOT EXISTS watches (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL,
  target TEXT NOT NULL,
  sinks TEXT NOT NULL DEFAULT '[]',
  interval_sec INTEGER NOT NULL,
  pages INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  last_check_at TEXT,
  baselined INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS seen (
  watch_id INTEGER NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
  ref TEXT NOT NULL,
  title TEXT NOT NULL,
  price REAL,
  quantity INTEGER,
  available INTEGER NOT NULL,
  snapshot TEXT NOT NULL,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  PRIMARY KEY (watch_id, ref)
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY,
  watch_id INTEGER NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  product TEXT,
  change TEXT
);
CREATE INDEX IF NOT EXISTS events_watch_at ON events(watch_id, at);
`

type DB struct{ db *sql.DB }

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// One connection: a scheduled check and a manual one queue instead of
	// failing on a locked database.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("open state database %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return &DB{db: db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

var (
	ErrExists   = errors.New("a watch with that name already exists")
	ErrNotFound = errors.New("no watch with that name")
)

func (d *DB) AddWatch(ctx context.Context, w Watch) (Watch, error) {
	if w.Sinks == nil {
		w.Sinks = []string{}
	}
	sinks, _ := json.Marshal(w.Sinks)
	w.CreatedAt = time.Now().UTC().Truncate(time.Second)
	res, err := d.db.ExecContext(ctx, `INSERT INTO watches(name,kind,target,sinks,interval_sec,pages,created_at) VALUES(?,?,?,?,?,?,?)`,
		w.Name, w.Kind, string(w.Target), string(sinks), int64(w.Interval/time.Second), w.Pages, w.CreatedAt.Format(time.RFC3339))
	if err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
			return w, ErrExists
		}
		return w, err
	}
	w.ID, _ = res.LastInsertId()
	return w, nil
}

const watchCols = `id,name,kind,target,sinks,interval_sec,pages,created_at,COALESCE(last_check_at,''),baselined`

func scanWatch(row interface{ Scan(...any) error }) (Watch, error) {
	var w Watch
	var target, sinks, created, last string
	var interval int64
	var baselined int
	if err := row.Scan(&w.ID, &w.Name, &w.Kind, &target, &sinks, &interval, &w.Pages, &created, &last, &baselined); err != nil {
		return w, err
	}
	w.Target = json.RawMessage(target)
	_ = json.Unmarshal([]byte(sinks), &w.Sinks)
	w.Interval = time.Duration(interval) * time.Second
	w.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if last != "" {
		w.LastCheckAt, _ = time.Parse(time.RFC3339, last)
	}
	w.Baselined = baselined == 1
	return w, nil
}

func (d *DB) Watches(ctx context.Context) ([]Watch, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+watchCols+` FROM watches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Watch{}
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) Watch(ctx context.Context, name string) (Watch, error) {
	w, err := scanWatch(d.db.QueryRowContext(ctx, `SELECT `+watchCols+` FROM watches WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

func (d *DB) RemoveWatch(ctx context.Context, name string) error {
	res, err := d.db.ExecContext(ctx, `DELETE FROM watches WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) SeenFor(ctx context.Context, watchID int64) (map[string]Seen, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT ref,title,price,quantity,available,snapshot FROM seen WHERE watch_id = ?`, watchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Seen{}
	for rows.Next() {
		var s Seen
		var price sql.NullFloat64
		var qty sql.NullInt64
		var avail int
		var snap string
		if err := rows.Scan(&s.Ref, &s.Title, &price, &qty, &avail, &snap); err != nil {
			return nil, err
		}
		if price.Valid {
			s.Price = &price.Float64
		}
		if qty.Valid {
			q := int(qty.Int64)
			s.Quantity = &q
		}
		s.Available, s.Snapshot = avail == 1, json.RawMessage(snap)
		out[s.Ref] = s
	}
	return out, rows.Err()
}

// CommitCheck stores one check's outcome in one transaction: seen rows
// upserted, removed ones deleted, events appended, the watch marked checked.
// The CLI delivers events to sinks before committing, so a crash between
// the two repeats events rather than losing them.
func (d *DB) CommitCheck(ctx context.Context, watchID int64, upserts []Seen, removed []string, events []Event) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range upserts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO seen(watch_id,ref,title,price,quantity,available,snapshot,first_seen,last_seen)
			VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(watch_id,ref) DO UPDATE SET title=excluded.title, price=excluded.price, quantity=excluded.quantity,
			  available=excluded.available, snapshot=excluded.snapshot, last_seen=excluded.last_seen`,
			watchID, s.Ref, s.Title, s.Price, s.Quantity, b2i(s.Available), string(s.Snapshot), now, now); err != nil {
			return err
		}
	}
	for _, ref := range removed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM seen WHERE watch_id = ? AND ref = ?`, watchID, ref); err != nil {
			return err
		}
	}
	for _, ev := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(watch_id,type,at,product,change) VALUES(?,?,?,?,?)`,
			watchID, ev.Type, ev.At.UTC().Format(time.RFC3339Nano), nullable(ev.Item), nullable(ev.Change)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE watches SET last_check_at = ?, baselined = 1 WHERE id = ?`, now, watchID); err != nil {
		return err
	}
	return tx.Commit()
}

// Events returns stored events, newest first. name "" means every watch.
func (d *DB) Events(ctx context.Context, name string, since time.Time, limit int) ([]Event, error) {
	q := `SELECT e.type, w.name, e.at, COALESCE(e.product,''), COALESCE(e.change,'') FROM events e JOIN watches w ON w.id = e.watch_id WHERE 1=1`
	var args []any
	if name != "" {
		q += ` AND w.name = ?`
		args = append(args, name)
	}
	if !since.IsZero() {
		q += ` AND e.at >= ?`
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY e.at DESC, e.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		var at, item, change string
		if err := rows.Scan(&ev.Type, &ev.Watch, &at, &item, &change); err != nil {
			return nil, err
		}
		ev.At, _ = time.Parse(time.RFC3339Nano, at)
		if item != "" {
			ev.Item = json.RawMessage(item)
		}
		if change != "" {
			ev.Change = json.RawMessage(change)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func nullable(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return string(r)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
