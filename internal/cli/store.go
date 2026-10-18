package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/config"
	"github.com/n0nuser/obramat-cli/internal/output"
)

// storeCacheTTL: the store list changes when a warehouse opens, a few times
// a year. A week keeps `store list` and name lookups to one sweep per week.
const storeCacheTTL = 7 * 24 * time.Hour

func (a *App) storeCachePath() string { return filepath.Join(a.Paths.CacheDir, "stores.json") }

// stores returns every store, from the cache when it is fresh.
func (a *App) stores(ctx context.Context, refresh bool) ([]api.Store, error) {
	path := a.storeCachePath()
	if !refresh {
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < storeCacheTTL {
			var cached []api.Store
			if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &cached) == nil && len(cached) > 0 {
				return cached, nil
			}
		}
		a.notice("building the store list (about 20 requests, cached for a week)")
	}
	all, err := a.Client.AllStores(ctx)
	if err != nil {
		return nil, err
	}
	if raw, err := json.Marshal(all); err == nil {
		if err := config.WriteAtomic(path, raw, 0o600); err != nil {
			a.notice("could not cache the store list: %v", err)
		}
	}
	return all, nil
}

// findStore resolves a store argument: its code, its slug, or a unique part
// of its name ("leganes", "obramat-usera", "12").
func (a *App) findStore(ctx context.Context, arg string) (api.Store, error) {
	all, err := a.stores(ctx, false)
	if err != nil {
		return api.Store{}, err
	}
	if id, err := strconv.Atoi(arg); err == nil {
		for _, s := range all {
			if s.ID == id {
				return s, nil
			}
		}
		return api.Store{}, &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("no store with code %d. See `obramat store list`", id)}
	}
	want := api.Slugify(arg)
	var hits []api.Store
	for _, s := range all {
		if s.Slug == want || s.Slug == "obramat-"+want {
			return s, nil
		}
		if strings.Contains(s.Slug, want) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return api.Store{}, &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("no store matches %q. See `obramat store list`", arg)}
	}
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = fmt.Sprintf("%s (%d)", h.Name, h.ID)
	}
	return api.Store{}, output.Usagef("%q matches several stores: %s", arg, strings.Join(names, ", "))
}

// storeContext resolves the store a command acts for: the --store flag, then
// OBRAMAT_STORE, then config. "none" clears it. A numeric code needs no
// network: the cookie only needs some name, and a cached one is used if any.
func (a *App) storeContext(ctx context.Context, flag string) (api.StoreRef, error) {
	v := flag
	if v == "" {
		v = os.Getenv("OBRAMAT_STORE")
	}
	if v == "" && a.Cfg.Store > 0 {
		v = strconv.Itoa(a.Cfg.Store)
	}
	if v == "" || v == "none" || v == "0" {
		return api.StoreRef{}, nil
	}
	if id, err := strconv.Atoi(v); err == nil && id > 0 {
		ref := api.StoreRef{ID: id}
		if raw, err := os.ReadFile(a.storeCachePath()); err == nil {
			var cached []api.Store
			if json.Unmarshal(raw, &cached) == nil {
				for _, s := range cached {
					if s.ID == id {
						ref.Name = s.Name
					}
				}
			}
		}
		return ref, nil
	}
	s, err := a.findStore(ctx, v)
	if err != nil {
		return api.StoreRef{}, err
	}
	return s.Ref(), nil
}

const storeFlagUsage = "store code, slug or name for prices and stock (default: OBRAMAT_STORE, then config `store`; `none` for the web price)"

type storeList []api.Store

func (l storeList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, s := range l {
		dist := ""
		if s.DistanceKm != nil {
			dist = fmt.Sprintf("%.1f km", *s.DistanceKm)
		}
		rows = append(rows, []string{strconv.Itoa(s.ID), s.Name, output.Truncate(s.City, 24), s.Postcode, dist, output.Truncate(s.Hours, 34)})
	}
	output.Table(w, color, []string{"ID", "STORE", "CITY", "POSTCODE", "DISTANCE", "HOURS"}, rows)
}

type storeStockView struct {
	Product string           `json:"product"`
	Stores  []api.StoreStock `json:"stores"`
}

func (v storeStockView) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(v.Stores))
	for _, s := range v.Stores {
		qty := "-"
		if s.Stock.Quantity != nil {
			qty = strconv.Itoa(*s.Stock.Quantity)
		}
		dist := ""
		if s.DistanceKm != nil {
			dist = fmt.Sprintf("%.1f km", *s.DistanceKm)
		}
		rows = append(rows, []string{strconv.Itoa(s.Store.ID), s.Store.Name, qty, dist, output.Dim(s.Stock.Label, color)})
	}
	output.Table(w, color, []string{"ID", "STORE", "STOCK", "DISTANCE", "SITE SAYS"}, rows)
}

func (a *App) storeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Obramat warehouses and the stock each one holds",
		Long: `Obramat warehouses ("Almacenes") and the stock each one holds.

A store is named by its code (the number the site uses, Usera is 12), its slug
(obramat-usera) or a unique part of its name (usera).

Examples:
  obramat store list --format pretty
  obramat store near --lat 41.39 --lng 2.17 --format pretty
  obramat store show leganes
  obramat store stock 25064668 --store 4
  obramat store stock 25064668 --lat 40.42 --lng -3.70 --format pretty
  obramat store stock 25064668 --all --format pretty`,
	}
	var refresh bool
	list := &cobra.Command{
		Use: "list", Short: "List every store with its code", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := a.stores(cmd.Context(), refresh)
			if err != nil {
				return err
			}
			return a.Printer.Print(storeList(all))
		},
	}
	list.Flags().BoolVar(&refresh, "refresh", false, "rebuild the cached list now")

	var lat, lng float64
	near := &cobra.Command{
		Use: "near", Short: "The six stores nearest a point, with opening state", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("lat") || !cmd.Flags().Changed("lng") {
				return output.Usagef("store near needs --lat and --lng")
			}
			s, err := a.Client.Nearby(cmd.Context(), lat, lng)
			if err != nil {
				return err
			}
			return a.Printer.Print(storeList(s))
		},
	}
	near.Flags().Float64Var(&lat, "lat", 0, "latitude")
	near.Flags().Float64Var(&lng, "lng", 0, "longitude")

	show := &cobra.Command{
		Use: "show STORE", Short: "One store's details", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.findStore(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			// The nearby lookup at the store's own position adds opening state.
			if near, err := a.Client.Nearby(cmd.Context(), s.Lat, s.Lng); err == nil {
				for _, n := range near {
					if n.ID == s.ID {
						n.DistanceKm = nil
						s = n
					}
				}
			}
			return a.Printer.Print(storeList{s})
		},
	}

	var store string
	var all bool
	stock := &cobra.Command{
		Use:   "stock PRODUCT",
		Short: "A product's stock per store: one store, the stores near a point, or all",
		Long: `A product's stock per store. PRODUCT is a reference (Ref. on the product page)
or a product URL.

With --store (or a default store) it reads that store's product page: exact
stock and that store's price. With --lat/--lng it lists the six stores nearest
the point, as the site's "availability by store" drawer does. With --all it
sweeps every store, about one request per three stores.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			path, err := a.Client.ProductPath(ctx, args[0])
			if err != nil {
				return err
			}
			ref := refFromPath(path, args[0])
			hasPoint := cmd.Flags().Changed("lat") || cmd.Flags().Changed("lng")
			switch {
			case all:
				stores, err := a.stores(ctx, false)
				if err != nil {
					return err
				}
				rows, err := a.Client.StockEverywhere(ctx, ref, stores)
				if err != nil {
					return err
				}
				return a.Printer.Print(storeStockView{Product: ref, Stores: rows})
			case hasPoint:
				if !cmd.Flags().Changed("lat") || !cmd.Flags().Changed("lng") {
					return output.Usagef("give both --lat and --lng")
				}
				rows, err := a.Client.StockNear(ctx, ref, lat, lng)
				if err != nil {
					return err
				}
				return a.Printer.Print(storeStockView{Product: ref, Stores: rows})
			}
			sref, err := a.storeContext(ctx, store)
			if err != nil {
				return err
			}
			if sref.ID == 0 {
				return output.Usagef("say where: --store STORE, --lat/--lng, or --all (or set a default with `obramat config set store ID`)")
			}
			d, err := a.Client.Product(ctx, path, sref)
			if err != nil {
				return err
			}
			row := api.StoreStock{Store: *d.Store}
			if d.Stock != nil {
				row.Stock = *d.Stock
			}
			return a.Printer.Print(storeStockView{Product: d.Ref, Stores: []api.StoreStock{row}})
		},
	}
	stock.Flags().StringVar(&store, "store", "", storeFlagUsage)
	stock.Flags().Float64Var(&lat, "lat", 0, "latitude: list the six stores nearest this point")
	stock.Flags().Float64Var(&lng, "lng", 0, "longitude")
	stock.Flags().BoolVar(&all, "all", false, "every store (about 15 requests)")

	cmd.AddCommand(list, near, show, stock)
	return cmd
}

// refFromPath takes the reference from the end of a product path
// (/productos/tablero-...-25064668.html).
func refFromPath(path, fallback string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".html")
	if i := strings.LastIndexByte(base, '-'); i >= 0 && api.IsRef(base[i+1:]) {
		return base[i+1:]
	}
	return fallback
}
