package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/output"
	"github.com/n0nuser/obramat-cli/internal/sink"
	"github.com/n0nuser/obramat-cli/internal/state"
	"github.com/n0nuser/obramat-cli/internal/watch"
)

const (
	defaultInterval = 30 * time.Minute
	// minInterval keeps scheduled checks well under the rate that gets a
	// client flagged by the site's bot protection.
	minInterval = 5 * time.Minute
)

func (a *App) db() (*state.DB, error) {
	if a.state == nil {
		db, err := state.Open(a.Paths.StateDB)
		if err != nil {
			return nil, err
		}
		a.state = db
	}
	return a.state, nil
}

func (a *App) watchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch searches and products for price and stock changes",
		Long: `Watch searches and products and get an event when something changes:
a new product in a search, a price change, a product going out of stock or
coming back, a product page disappearing.

Watches live on this machine. "check" runs the due ones once, prints their
events and delivers them to the watch's sinks; "run" loops in the foreground;
"service" installs a systemd timer or launchd agent that runs "check".
A watch pins its store when it is created.

Examples:
  obramat watch add product 25064668 --store leganes --name osb-15
  obramat watch add search "tablero osb" --store 4 --max-price 30 --name osb --notify phone
  obramat watch check --format jsonl | jq -r 'select(.type=="product.back_in_stock") | .product.url'
  obramat watch events --since 24h --format pretty
  obramat watch service install --interval 1h`,
	}
	add := &cobra.Command{Use: "add", Short: "Create a watch"}
	add.AddCommand(a.watchAddSearchCmd(), a.watchAddProductCmd())
	cmd.AddCommand(add, a.watchListCmd(), a.watchRemoveCmd(), a.watchCheckCmd(), a.watchRunCmd(), a.watchEventsCmd(), a.watchServiceCmd())
	return cmd
}

type watchAddFlags struct {
	name        string
	notify      []string
	interval    time.Duration
	pages       int
	emitInitial bool
}

func bindWatchAddFlags(cmd *cobra.Command, f *watchAddFlags, pages bool) {
	cmd.Flags().StringVar(&f.name, "name", "", "watch name: letters, digits, - and _ (required)")
	cmd.Flags().StringArrayVar(&f.notify, "notify", nil, "sink from config to deliver events to, repeatable")
	cmd.Flags().DurationVar(&f.interval, "interval", 0, "how often the watch is due (default: config watch.interval, else 30m; at least 5m)")
	cmd.Flags().BoolVar(&f.emitInitial, "emit-initial", false, "report what exists now as product.new events instead of a silent baseline")
	if pages {
		cmd.Flags().IntVar(&f.pages, "pages", 1, "result pages to compare on each check")
	}
	_ = cmd.MarkFlagRequired("name")
}

func (a *App) interval(flag time.Duration) (time.Duration, error) {
	iv := flag
	if iv == 0 && a.Cfg.Watch.Interval != "" {
		d, err := time.ParseDuration(a.Cfg.Watch.Interval)
		if err != nil {
			return 0, output.Usagef("config watch.interval: %v", err)
		}
		iv = d
	}
	if iv == 0 {
		iv = defaultInterval
	}
	if iv < minInterval {
		return 0, output.Usagef("interval %s is below the %s floor", iv, minInterval)
	}
	return iv, nil
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (a *App) addWatch(ctx context.Context, kind string, target any, f watchAddFlags) error {
	if !validName(f.name) {
		return output.Usagef("watch name %q: use letters, digits, - and _", f.name)
	}
	iv, err := a.interval(f.interval)
	if err != nil {
		return err
	}
	for _, n := range f.notify {
		s, ok := a.Cfg.Sinks[n]
		if !ok {
			return output.Usagef("no sink %q in %s. Add one with `obramat config set sinks.%s.type ntfy` (see `obramat sink list`)", n, a.Paths.ConfigFile, n)
		}
		if err := sink.Validate(n, s); err != nil {
			return output.Usagef("%v", err)
		}
	}
	raw, _ := json.Marshal(target)
	db, err := a.db()
	if err != nil {
		return err
	}
	w, err := db.AddWatch(ctx, state.Watch{Name: f.name, Kind: kind, Target: raw, Sinks: f.notify, Interval: iv, Pages: max(f.pages, 1)})
	if errors.Is(err, state.ErrExists) {
		return output.Usagef("a watch named %q exists. Remove it first or pick another name", f.name)
	}
	if err != nil {
		return err
	}
	// The first check runs now, so the scheduled ones report only changes.
	res, err := watch.Check(ctx, a.Client, db, w, f.emitInitial)
	if err != nil {
		_ = db.RemoveWatch(ctx, w.Name) // a watch whose target cannot be read is not kept
		return err
	}
	if f.emitInitial {
		a.deliver(ctx, w, res.Events)
	}
	if err := db.CommitCheck(ctx, w.ID, res.Upserts, res.Removed, res.Events); err != nil {
		return err
	}
	if f.emitInitial {
		return a.Printer.Print(eventList(res.Events))
	}
	w.Baselined, w.LastCheckAt = true, time.Now().UTC().Truncate(time.Second)
	return a.Printer.Print(watchView{Watch: w, Products: len(res.Upserts)})
}

func (a *App) watchAddSearchCmd() *cobra.Command {
	var sf searchFlags
	var wf watchAddFlags
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Watch a search or category for new products, price and stock changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.listParams(cmd.Context(), cmd, args, sf)
			if err != nil {
				return err
			}
			return a.addWatch(cmd.Context(), state.KindSearch, watch.SearchTarget{Params: p}, wf)
		},
	}
	bindSearchFlags(cmd, &sf)
	bindWatchAddFlags(cmd, &wf, true)
	return cmd
}

func (a *App) watchAddProductCmd() *cobra.Command {
	var wf watchAddFlags
	var store string
	cmd := &cobra.Command{
		Use:   "product PRODUCT",
		Short: "Watch one product's price and stock at a store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			sref, err := a.storeContext(ctx, store)
			if err != nil {
				return err
			}
			if sref.ID == 0 {
				a.notice("no store: this watch follows the web price only, not stock")
			}
			path, err := a.Client.ProductPath(ctx, args[0])
			if err != nil {
				return err
			}
			return a.addWatch(ctx, state.KindProduct, watch.ProductTarget{Ref: refFromPath(path, args[0]), Path: path, Store: sref}, wf)
		},
	}
	cmd.Flags().StringVar(&store, "store", "", storeFlagUsage)
	bindWatchAddFlags(cmd, &wf, false)
	return cmd
}

type watchView struct {
	state.Watch
	Products int `json:"products"`
}

func (v watchView) Pretty(w io.Writer, color bool) { watchList{v.Watch}.Pretty(w, color) }

type watchList []state.Watch

func describe(w state.Watch) string {
	switch w.Kind {
	case state.KindSearch:
		var t watch.SearchTarget
		_ = json.Unmarshal(w.Target, &t)
		what := t.Params.Query
		if t.Params.Category != "" {
			what = t.Params.Category
		}
		return what + storeSuffix(t.Params.Store)
	case state.KindProduct:
		var t watch.ProductTarget
		_ = json.Unmarshal(w.Target, &t)
		return t.Ref + storeSuffix(t.Store)
	}
	return ""
}

func storeSuffix(s api.StoreRef) string {
	if s.ID == 0 {
		return ""
	}
	return fmt.Sprintf(" @%d", s.ID)
}

func (l watchList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, wt := range l {
		last := "never"
		if !wt.LastCheckAt.IsZero() {
			last = wt.LastCheckAt.Local().Format("01-02 15:04")
		}
		rows = append(rows, []string{wt.Name, wt.Kind, output.Truncate(describe(wt), 44), wt.Interval.String(), strings.Join(wt.Sinks, ","), last})
	}
	output.Table(w, color, []string{"WATCH", "KIND", "TARGET", "EVERY", "SINKS", "CHECKED"}, rows)
}

func (a *App) watchListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List watches", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := a.db()
			if err != nil {
				return err
			}
			ws, err := db.Watches(cmd.Context())
			if err != nil {
				return err
			}
			return a.Printer.Print(watchList(ws))
		},
	}
}

func (a *App) confirm(yes bool, prompt string) error {
	if yes {
		return nil
	}
	if !a.Interactive {
		return output.Usagef("%s Pass --yes to confirm without a prompt", prompt)
	}
	fmt.Fprintf(a.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	if s := strings.ToLower(strings.TrimSpace(line)); s != "y" && s != "yes" {
		return output.Usagef("cancelled")
	}
	return nil
}

func (a *App) watchRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "remove NAME", Short: "Delete a watch and its event history", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := a.db()
			if err != nil {
				return err
			}
			if err := a.confirm(yes, fmt.Sprintf("Delete watch %q and its events?", args[0])); err != nil {
				return err
			}
			if err := db.RemoveWatch(cmd.Context(), args[0]); errors.Is(err, state.ErrNotFound) {
				return &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("no watch named %q", args[0])}
			} else if err != nil {
				return err
			}
			return a.Printer.Print(map[string]string{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return cmd
}

type eventList []state.Event

func (l eventList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, ev := range l {
		title, detail, link := sink.Summary(ev)
		rows = append(rows, []string{output.Dim(ev.At.Local().Format("01-02 15:04"), color), ev.Watch, ev.Type, output.Truncate(title, 44), output.Truncate(detail, 34), output.Dim(link, color)})
	}
	output.Table(w, color, []string{"WHEN", "WATCH", "EVENT", "PRODUCT", "DETAIL", "URL"}, rows)
}

func (a *App) deliver(ctx context.Context, w state.Watch, events []state.Event) {
	for _, ev := range events {
		for _, name := range w.Sinks {
			if err := sink.Deliver(ctx, name, a.Cfg.Sinks[name], ev); err != nil {
				a.notice("sink %s: %v", name, err)
			}
		}
	}
}

// runChecks checks each watch, delivers, then commits. One failing watch is
// reported and the others still run; the first error decides the exit code.
func (a *App) runChecks(ctx context.Context, db *state.DB, ws []state.Watch) ([]state.Event, error) {
	all := []state.Event{}
	var firstErr error
	for _, w := range ws {
		res, err := watch.Check(ctx, a.Client, db, w, false)
		if err != nil {
			if ctx.Err() != nil {
				return all, ctx.Err()
			}
			a.notice("watch %s: %v", w.Name, err)
			if firstErr == nil {
				firstErr = err
			}
			var e *api.Error
			if errors.As(err, &e) && e.Kind == api.KindBlocked {
				return all, err // more requests now only deepen the block
			}
			continue
		}
		a.deliver(ctx, w, res.Events)
		if err := db.CommitCheck(ctx, w.ID, res.Upserts, res.Removed, res.Events); err != nil {
			return all, err
		}
		all = append(all, res.Events...)
	}
	return all, firstErr
}

func (a *App) selectWatches(ctx context.Context, db *state.DB, names []string, all bool) ([]state.Watch, error) {
	if len(names) > 0 {
		out := make([]state.Watch, 0, len(names))
		for _, n := range names {
			w, err := db.Watch(ctx, n)
			if errors.Is(err, state.ErrNotFound) {
				return nil, &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("no watch named %q", n)}
			}
			if err != nil {
				return nil, err
			}
			out = append(out, w)
		}
		return out, nil
	}
	ws, err := db.Watches(ctx)
	if err != nil || all {
		return ws, err
	}
	due := ws[:0]
	for _, w := range ws {
		if w.LastCheckAt.IsZero() || time.Since(w.LastCheckAt) >= w.Interval-time.Minute {
			due = append(due, w)
		}
	}
	return due, nil
}

func (a *App) watchCheckCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "check [NAME...]",
		Short: "Run due watches once and print their events",
		Long: `Run watches once. Named watches run whatever their schedule; without names
the due ones run, or every one with --all. Prints the events (an empty list
when nothing changed, still exit 0) and delivers them to sinks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a.rotateServiceLog()
			db, err := a.db()
			if err != nil {
				return err
			}
			ws, err := a.selectWatches(cmd.Context(), db, args, all)
			if err != nil {
				return err
			}
			events, err := a.runChecks(cmd.Context(), db, ws)
			if perr := a.Printer.Print(eventList(events)); perr != nil {
				return perr
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "run every watch, due or not")
	return cmd
}

func (a *App) watchRunCmd() *cobra.Command {
	var tick time.Duration
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Check due watches in a loop until Ctrl-C",
		Long: `Foreground loop: every tick runs the due watches and prints their events as
they happen (--format jsonl streams one per line). The tick is the shortest
watch interval unless --interval says otherwise, plus up to 10% jitter.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if tick != 0 && tick < minInterval {
				return output.Usagef("--interval is below the %s floor", minInterval)
			}
			db, err := a.db()
			if err != nil {
				return err
			}
			for {
				ws, err := a.selectWatches(ctx, db, nil, false)
				if err != nil {
					return err
				}
				if _, paused := a.pausedUntil(); paused {
					ws = nil // sit the pause out, then carry on
				}
				events, err := a.runChecks(ctx, db, ws)
				if ctx.Err() != nil {
					return nil
				}
				a.recordBlock(err)
				for _, ev := range events {
					if err := a.Printer.Print(ev); err != nil {
						return err
					}
				}
				next := tick
				if next == 0 {
					next = defaultInterval
					all, _ := db.Watches(ctx)
					for _, w := range all {
						next = min(next, w.Interval)
					}
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(next + watch.Jitter(next, time.Now().UnixNano())):
				}
			}
		},
	}
	cmd.Flags().DurationVar(&tick, "interval", 0, "tick length (default: the shortest watch interval)")
	return cmd
}

func (a *App) watchEventsCmd() *cobra.Command {
	var since time.Duration
	var limit int
	cmd := &cobra.Command{
		Use: "events [NAME]", Short: "Show stored events, newest first", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := a.db()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			var from time.Time
			if since > 0 {
				from = time.Now().Add(-since)
			}
			evs, err := db.Events(cmd.Context(), name, from, limit)
			if err != nil {
				return err
			}
			return a.Printer.Print(eventList(evs))
		},
	}
	cmd.Flags().DurationVar(&since, "since", 0, "only events newer than this, for example 24h")
	cmd.Flags().IntVar(&limit, "limit", 100, "at most this many events")
	return cmd
}
