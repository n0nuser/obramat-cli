package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/obramat-cli/internal/api"
	"github.com/n0nuser/obramat-cli/internal/output"
)

// brandFacet is the facet code of "Marca". It is the same in every listing
// checked (cement, boards, tools).
// ponytail: fixed code; read it from the page's facets if a listing ever
// labels another code "Marca".
const brandFacet = "attribute-06575"

// searchFlags are shared by `search`, `search filters` and `watch add search`.
type searchFlags struct {
	category string
	brands   []string
	filters  []string
	minPrice float64
	maxPrice float64
	sort     string
	store    string
	inStock  bool
}

func bindSearchFlags(cmd *cobra.Command, f *searchFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.category, "category", "", "browse this category path instead of searching (see `obramat category list`)")
	fl.StringArrayVar(&f.brands, "brand", nil, "brand, repeatable (the site's Marca facet)")
	fl.StringArrayVar(&f.filters, "filter", nil, "facet CODE=VALUE, repeatable (see `obramat search filters`)")
	fl.Float64Var(&f.minPrice, "min-price", 0, "minimum price in EUR")
	fl.Float64Var(&f.maxPrice, "max-price", 0, "maximum price in EUR")
	fl.StringVar(&f.sort, "sort", "relevance", "order: relevance, price_asc, price_desc")
	fl.StringVar(&f.store, "store", "", storeFlagUsage)
	fl.BoolVar(&f.inStock, "in-stock", false, "only products with stock at the store (the site's \"En stock en Almacén\" filter; needs a store)")
	_ = cmd.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions([]string{"relevance", "price_asc", "price_desc"}, cobra.ShellCompDirectiveNoFileComp))
}

func (a *App) listParams(ctx context.Context, cmd *cobra.Command, keywords []string, f searchFlags) (api.ListParams, error) {
	p := api.ListParams{Query: strings.TrimSpace(strings.Join(keywords, " ")), Filters: map[string][]string{}}
	if f.category != "" {
		if p.Query != "" {
			return p, output.Usagef("give keywords or --category, not both: the site's category pages ignore a query")
		}
		c, err := api.CategoryPath(f.category)
		if err != nil {
			return p, err
		}
		p.Category = c
	} else if p.Query == "" {
		return p, output.Usagef("say what to search for, or browse a category with --category PATH")
	}
	s, ok := api.Sorts[f.sort]
	if !ok {
		return p, output.Usagef("--sort must be relevance, price_asc or price_desc")
	}
	p.Sort = s
	if cmd.Flags().Changed("min-price") {
		v := f.minPrice
		p.MinPrice = &v
	}
	if cmd.Flags().Changed("max-price") {
		v := f.maxPrice
		p.MaxPrice = &v
	}
	if p.MinPrice != nil && p.MaxPrice != nil && *p.MinPrice > *p.MaxPrice {
		return p, output.Usagef("--min-price is above --max-price")
	}
	for _, b := range f.brands {
		p.Filters[brandFacet] = append(p.Filters[brandFacet], strings.ToUpper(b))
	}
	for _, kv := range f.filters {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" || v == "" {
			return p, output.Usagef("--filter takes CODE=VALUE, for example --filter attribute-00596=15. See `obramat search filters`")
		}
		if k == "price" {
			return p, output.Usagef("use --min-price and --max-price for the price range")
		}
		p.Filters[k] = append(p.Filters[k], v)
	}
	store, err := a.storeContext(ctx, f.store)
	if err != nil {
		return p, err
	}
	p.Store = store
	if f.inStock {
		if store.ID == 0 {
			return p, output.Usagef("--in-stock needs a store: stock is per store (--store, OBRAMAT_STORE or `obramat config set store ID`)")
		}
		p.InStock = true
	}
	return p, nil
}

// inStock keeps products the store has. The site's own toggle already asks for
// them; this drops any card the site still returned without a stock line (the
// toggle is tied to the store in context, so a page can still carry
// neighbouring-store products), and filtering each page before counting lets
// --limit fill up from later pages.
func inStock(ps []api.Product) []api.Product {
	kept := ps[:0]
	for _, p := range ps {
		if p.Stock != nil && p.Stock.Available {
			kept = append(kept, p)
		}
	}
	return kept
}

type listingView struct{ *api.Listing }

func (v listingView) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(v.Products))
	for _, p := range v.Products {
		rows = append(rows, productRow(p, color))
	}
	output.Table(w, color, []string{"REF", "PRICE", "UNIT", "STOCK", "PRODUCT", "URL"}, rows)
	where := "web price (no store set)"
	if v.Store != nil {
		where = fmt.Sprintf("store %s (%d)", v.Store.Name, v.Store.ID)
	}
	fmt.Fprintln(w, output.Dim(fmt.Sprintf("%d of %d products, page %d/%d, %s", len(v.Products), v.Total, v.Page, max(v.Pages, 1), where), color))
}

func productRow(p api.Product, color bool) []string {
	price, unit := "-", ""
	if p.Price != nil {
		price = fmt.Sprintf("%.2f €", p.Price.Amount)
		unit = p.Price.Unit
		if p.Price.Tax == "IGIC" {
			price += " IGIC"
		}
	}
	stock := ""
	if p.Stock != nil {
		switch {
		case p.Stock.Quantity != nil:
			stock = strconv.Itoa(*p.Stock.Quantity)
		case !p.Stock.Available:
			stock = "no"
		}
	}
	return []string{p.Ref, price, unit, stock, output.Truncate(p.Title, 52), output.Dim(p.URL, color)}
}

func (a *App) searchCmd() *cobra.Command {
	var f searchFlags
	var page, pages, limit int
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Search products, or browse a category, with the site's filters",
		Long: `Search products, or browse a category with --category, using the filters the
site offers: brand, price range, sort order and any facet from
` + "`obramat search filters`" + `.

Prices and stock are for the store in context. Without one the site shows a
default web price, which for some products is a Canary Islands (IGIC) price,
and no stock. A store also narrows results to what it sells, and turns on the
site's "En stock en Almacén" filter, which --in-stock uses.

Examples:
  obramat search "tablero osb" --format pretty
  obramat search cemento --brand WEBER --min-price 5 --max-price 20 --sort price_asc
  obramat search "tablero osb" --store leganes --in-stock --format pretty
  obramat search --category /madera/tableros/tableros-y-tablas-de-madera/ --filter attribute-00596=15
  obramat search cemento --pages 3 --format jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p, err := a.listParams(ctx, cmd, args, f)
			if err != nil {
				return err
			}
			if page < 1 || pages < 1 {
				return output.Usagef("--page and --pages start at 1")
			}
			p.Page = page
			var out *api.Listing
			for i := 0; i < pages; i++ {
				l, err := a.Client.List(ctx, p)
				if err != nil {
					return err
				}
				if f.inStock {
					l.Products = inStock(l.Products)
				}
				if out == nil {
					out = l
				} else {
					out.Products = append(out.Products, l.Products...)
					out.Page = l.Page
				}
				if p.Page >= l.Pages || (limit > 0 && len(out.Products) >= limit) {
					break
				}
				p.Page++
			}
			if limit > 0 && len(out.Products) > limit {
				out.Products = out.Products[:limit]
			}
			if a.Printer.Format == output.JSONL {
				return a.Printer.Print(out.Products)
			}
			return a.Printer.Print(listingView{out})
		},
	}
	bindSearchFlags(cmd, &f)
	cmd.Flags().IntVar(&page, "page", 1, "result page to start at (the site serves at most 10)")
	cmd.Flags().IntVar(&pages, "pages", 1, "how many pages to fetch")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many products (0: whole pages)")
	cmd.AddCommand(a.searchFiltersCmd())
	return cmd
}

type filtersView []api.Filter

func (v filtersView) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(v))
	for _, f := range v {
		vals := make([]string, 0, len(f.Values))
		for _, x := range f.Values {
			vals = append(vals, fmt.Sprintf("%s (%d)", x.Value, x.Count))
		}
		rows = append(rows, []string{f.Code, f.Label, output.Truncate(strings.Join(vals, ", "), 70)})
	}
	output.Table(w, color, []string{"CODE", "FILTER", "VALUES (COUNT)"}, rows)
}

func (a *App) searchFiltersCmd() *cobra.Command {
	var f searchFlags
	cmd := &cobra.Command{
		Use:   "filters [keywords...]",
		Short: "List the filters the site offers for a search or category",
		Long: `List the facets the site offers for a search or a category, with the values
and how many products each has. Pass CODE=VALUE to --filter.

Examples:
  obramat search filters cemento --format pretty
  obramat search filters --category /madera/tableros/tableros-y-tablas-de-madera/`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := a.listParams(cmd.Context(), cmd, args, f)
			if err != nil {
				return err
			}
			l, err := a.Client.List(cmd.Context(), p)
			if err != nil {
				return err
			}
			if l.Filters == nil {
				l.Filters = []api.Filter{}
			}
			return a.Printer.Print(filtersView(l.Filters))
		},
	}
	bindSearchFlags(cmd, &f)
	return cmd
}

type productView struct{ *api.ProductDetail }

func (v productView) Pretty(w io.Writer, color bool) {
	d := v.ProductDetail
	rows := [][]string{{"ref", d.Ref}, {"product", d.Title}}
	if d.Brand != "" {
		rows = append(rows, []string{"brand", d.Brand})
	}
	if d.Price != nil {
		pr := fmt.Sprintf("%.2f € %s / %s", d.Price.Amount, d.Price.Tax, d.Price.Unit)
		if d.Price.AmountExVAT != nil {
			pr += fmt.Sprintf("  (%.2f € without)", *d.Price.AmountExVAT)
		}
		rows = append(rows, []string{"price", pr})
		if m := d.Price.PerMeasure; m != nil {
			rows = append(rows, []string{"", fmt.Sprintf("%.2f € / %s", m.Amount, m.Unit)})
		}
	} else {
		rows = append(rows, []string{"price", "-"})
	}
	if d.Store != nil {
		rows = append(rows, []string{"store", fmt.Sprintf("%s (%d)", d.Store.Name, d.Store.ID)})
	} else {
		rows = append(rows, []string{"store", "none: web price, no stock (use --store)"})
	}
	if d.Stock != nil {
		rows = append(rows, []string{"stock", d.Stock.Label})
	}
	for _, del := range d.Delivery {
		rows = append(rows, []string{"delivery", del})
	}
	if d.EAN != "" {
		rows = append(rows, []string{"ean", d.EAN})
	}
	if d.Category != "" {
		rows = append(rows, []string{"category", d.Category})
	}
	rows = append(rows, []string{"url", d.URL})
	output.Table(w, color, nil, rows)
	if len(d.Attributes) > 0 {
		fmt.Fprintln(w)
		attrs := make([][]string, 0, len(d.Attributes))
		for _, at := range d.Attributes {
			attrs = append(attrs, []string{output.Dim(at.Name, color), output.Truncate(at.Value, 80)})
		}
		output.Table(w, color, nil, attrs)
	}
}

func (a *App) productCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "product",
		Aliases: []string{"item"},
		Short:   "Inspect one product",
	}
	var store string
	show := &cobra.Command{
		Use:   "show PRODUCT",
		Short: "A product's price, stock at the store, delivery options and characteristics",
		Long: `Show one product. PRODUCT is its reference (the "Ref." number, for example
25064668) or its URL. Price and stock are for the store in context.

Examples:
  obramat product show 25064668 --store 4 --format pretty
  obramat product show https://www.obramat.es/productos/tablero-osb3-250x125x1-5cm-25064668.html`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sref, err := a.storeContext(cmd.Context(), store)
			if err != nil {
				return err
			}
			d, err := a.Client.Product(cmd.Context(), args[0], sref)
			if err != nil {
				return err
			}
			return a.Printer.Print(productView{d})
		},
	}
	show.Flags().StringVar(&store, "store", "", storeFlagUsage)
	cmd.AddCommand(show)
	return cmd
}

type categoryList []api.Category

func (l categoryList) Pretty(w io.Writer, color bool) {
	rows := make([][]string, 0, len(l))
	for _, c := range l {
		sub := ""
		if c.Children > 0 {
			sub = strconv.Itoa(c.Children)
		}
		rows = append(rows, []string{c.Path, sub})
	}
	output.Table(w, color, []string{"CATEGORY", "SUBCATEGORIES"}, rows)
}

func (a *App) categoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "category", Short: "Browse the category tree"}
	var parent string
	var depth int
	list := &cobra.Command{
		Use:   "list",
		Short: "List category paths, for search --category",
		Long: `List the site's categories as paths. Browse one with
` + "`obramat search --category PATH`" + `. Top-level paths are landing pages; the product
lists start one or two levels down.

Examples:
  obramat category list --depth 1 --format pretty
  obramat category list --parent /madera/ --format pretty`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cats, err := a.Client.Categories(cmd.Context())
			if err != nil {
				return err
			}
			if parent != "" {
				if parent, err = api.CategoryPath(parent); err != nil {
					return err
				}
			}
			out := categoryList{}
			for _, c := range cats {
				if parent != "" && c.Parent != parent {
					continue
				}
				if depth > 0 && c.Depth > depth {
					continue
				}
				out = append(out, c)
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
			return a.Printer.Print(out)
		},
	}
	list.Flags().StringVar(&parent, "parent", "", "only the direct children of this path")
	list.Flags().IntVar(&depth, "depth", 0, "only paths this many levels deep or less")
	cmd.AddCommand(list)
	return cmd
}
