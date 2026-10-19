// Package watch runs one Check of a watch: fetch the target's current state,
// compare it with what the state database holds, and produce events. It does
// not deliver or store anything; the CLI delivers to sinks, then commits.
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/state"
)

// SearchTarget is a search or category listing, store included. The in-stock
// filter travels inside Params, so there is only one copy of it.
type SearchTarget struct {
	Params api.ListParams `json:"params"`
}

// ProductTarget is one product at one store (or at none: web price only).
type ProductTarget struct {
	Ref   string       `json:"ref"`
	Path  string       `json:"path"`
	Store api.StoreRef `json:"store"`
}

type Result struct {
	Events  []state.Event
	Upserts []state.Seen
	Removed []string
}

type change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// Check runs one check. Unless emitInitial, a watch's first check records a
// silent baseline.
func Check(ctx context.Context, c *api.Client, db *state.DB, w state.Watch, emitInitial bool) (Result, error) {
	seen, err := db.SeenFor(ctx, w.ID)
	if err != nil {
		return Result{}, err
	}
	baseline := !w.Baselined && !emitInitial
	switch w.Kind {
	case state.KindSearch:
		var t SearchTarget
		if err := json.Unmarshal(w.Target, &t); err != nil {
			return Result{}, fmt.Errorf("watch %s has a corrupt target: %w", w.Name, err)
		}
		return checkSearch(ctx, c, w, t, seen, baseline)
	case state.KindProduct:
		var t ProductTarget
		if err := json.Unmarshal(w.Target, &t); err != nil {
			return Result{}, fmt.Errorf("watch %s has a corrupt target: %w", w.Name, err)
		}
		return checkProduct(ctx, c, w, t, seen, baseline)
	}
	return Result{}, fmt.Errorf("watch %s has unknown kind %q", w.Name, w.Kind)
}

func checkSearch(ctx context.Context, c *api.Client, w state.Watch, t SearchTarget, seen map[string]state.Seen, baseline bool) (Result, error) {
	var res Result
	p := t.Params
	p.Page = 1
	for i := 0; i < max(w.Pages, 1); i++ {
		l, err := c.List(ctx, p)
		if err != nil {
			return Result{}, err
		}
		for _, pr := range l.Products {
			if pr.Title == "" {
				continue // a card the site never rendered: nothing to compare
			}
			if t.Params.InStock && (pr.Stock == nil || !pr.Stock.Available) {
				continue
			}
			s := seenOf(pr)
			prev, known := seen[pr.Ref]
			switch {
			case !known && !baseline:
				res.Events = append(res.Events, event(w, state.ProductNew, pr, nil))
			case known:
				// Quantities move with every sale; a search reports only
				// price and in/out changes. Product watches report quantity.
				res.Events = append(res.Events, diff(w, prev, s, pr, false)...)
			}
			res.Upserts = append(res.Upserts, s)
		}
		if p.Page >= l.Pages {
			break
		}
		p.Page++
	}
	return res, nil
}

func checkProduct(ctx context.Context, c *api.Client, w state.Watch, t ProductTarget, seen map[string]state.Seen, baseline bool) (Result, error) {
	var res Result
	d, err := c.Product(ctx, t.Path, t.Store)
	if err != nil {
		var e *api.Error
		if errors.As(err, &e) && e.Kind == api.KindNotFound {
			if prev, ok := seen[t.Ref]; ok {
				var last api.Product
				_ = json.Unmarshal(prev.Snapshot, &last)
				res.Events = append(res.Events, event(w, state.ProductRemoved, last, nil))
				res.Removed = append(res.Removed, t.Ref)
			}
			return res, nil
		}
		return Result{}, err
	}
	pr := d.Product
	s := seenOf(pr)
	if prev, ok := seen[pr.Ref]; ok {
		res.Events = append(res.Events, diff(w, prev, s, pr, true)...)
	} else if !baseline {
		res.Events = append(res.Events, event(w, state.ProductNew, pr, nil))
	}
	res.Upserts = append(res.Upserts, s)
	return res, nil
}

func seenOf(p api.Product) state.Seen {
	s := state.Seen{Ref: p.Ref, Title: p.Title}
	s.Snapshot, _ = json.Marshal(p)
	if p.Price != nil {
		v := p.Price.Amount
		s.Price = &v
	}
	if p.Stock != nil {
		s.Quantity = p.Stock.Quantity
		s.Available = p.Stock.Available
	}
	return s
}

// diff compares a product with what the watch saw last time.
func diff(w state.Watch, prev, cur state.Seen, p api.Product, quantities bool) []state.Event {
	var out []state.Event
	if prev.Price != nil && cur.Price != nil && *prev.Price != *cur.Price {
		out = append(out, event(w, state.PriceChanged, p, change{*prev.Price, *cur.Price}))
	}
	if p.Stock == nil {
		return out // no store, no stock to compare
	}
	switch {
	case prev.Available && !cur.Available:
		out = append(out, event(w, state.OutOfStock, p, nil))
	case !prev.Available && cur.Available:
		out = append(out, event(w, state.BackInStock, p, nil))
	case quantities && prev.Quantity != nil && cur.Quantity != nil && *prev.Quantity != *cur.Quantity:
		out = append(out, event(w, state.StockChanged, p, change{*prev.Quantity, *cur.Quantity}))
	}
	return out
}

func event(w state.Watch, typ string, p api.Product, ch any) state.Event {
	ev := state.Event{Type: typ, Watch: w.Name, At: time.Now().UTC()}
	ev.Product, _ = json.Marshal(p)
	if ch != nil {
		ev.Change, _ = json.Marshal(ch)
	}
	return ev
}

// Jitter spreads checks: up to a tenth of the interval, from seed.
func Jitter(interval time.Duration, seed int64) time.Duration {
	n := int64(interval / 10)
	if n <= 0 {
		return 0
	}
	if seed < 0 {
		seed = -seed
	}
	return time.Duration(seed % n)
}
