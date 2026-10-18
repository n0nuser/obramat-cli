package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Price is what one selling unit costs at one store (or nationally online
// when the product carries no store). Amount includes the sales tax named in
// Tax: IVA, or IGIC for Canary Islands prices; AmountExVAT excludes it.
type Price struct {
	Amount      float64       `json:"amount"`
	AmountExVAT *float64      `json:"amount_ex_vat,omitempty"`
	Tax         string        `json:"tax"`
	Unit        string        `json:"unit,omitempty"` // selling unit as the site words it: Unidad, cajas, Kg...
	Currency    string        `json:"currency"`
	PerMeasure  *MeasurePrice `json:"per_measure,omitempty"`
}

// MeasurePrice is the comparison price the site prints under the main one,
// for example per m² for a board sold by the unit.
type MeasurePrice struct {
	Amount      float64  `json:"amount"`
	AmountExVAT *float64 `json:"amount_ex_vat,omitempty"`
	Unit        string   `json:"unit"`
}

// Stock is a product's availability at the store in context.
type Stock struct {
	// Quantity is the number of units the site reports at the store; nil
	// when it shows none. Without a store the site never shows a number.
	Quantity  *int   `json:"quantity"`
	Available bool   `json:"available"`
	Label     string `json:"label,omitempty"`
}

// Product is one product as a listing card shows it. Price and Stock are for
// Store when it is set, and Stock is only present then.
type Product struct {
	Ref      string    `json:"ref"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	Image    string    `json:"image,omitempty"`
	Brand    string    `json:"brand,omitempty"`
	Price    *Price    `json:"price"`
	Store    *StoreRef `json:"store"`
	Stock    *Stock    `json:"stock,omitempty"`
	Badges   []string  `json:"badges,omitempty"`
	Delivery []string  `json:"delivery,omitempty"`
}

type FilterValue struct {
	Value   string `json:"value"`
	Count   int    `json:"count"`
	Applied bool   `json:"applied,omitempty"`
}

// Filter is one facet the site offers for a listing. Code is what goes into
// --filter CODE=VALUE; the price range is the "price" facet.
type Filter struct {
	Code    string        `json:"code"`
	Label   string        `json:"label"`
	Type    string        `json:"type"` // standard or range
	Unit    string        `json:"unit,omitempty"`
	Applied bool          `json:"applied,omitempty"`
	Values  []FilterValue `json:"values,omitempty"`
}

type Listing struct {
	Query    string    `json:"query,omitempty"`
	Category string    `json:"category,omitempty"`
	Store    *StoreRef `json:"store"`
	Page     int       `json:"page"`
	Pages    int       `json:"pages"`
	Total    int       `json:"total"`
	Sort     string    `json:"sort"`
	Products []Product `json:"products"`
	Filters  []Filter  `json:"-"`
}

// Sorts maps the CLI's sort names to the site's.
var Sorts = map[string]string{"relevance": "relevance", "price_asc": "price-asc", "price_desc": "price-desc"}

// ListParams is one search or category page. Exactly one of Query and
// Category is set; the site's category pages ignore a query.
type ListParams struct {
	Query    string
	Category string // path, for example /madera/tableros/
	Filters  map[string][]string
	MinPrice *float64
	MaxPrice *float64
	Sort     string // site value: relevance, price-asc, price-desc
	Page     int
	Store    StoreRef
}

// filtersParam encodes the site's `filters` query parameter: a JSON object of
// facet code to values joined with "#|", with the price range as
// range-price-min and range-price-max.
func (p ListParams) filtersParam() string {
	m := map[string]string{}
	for code, vals := range p.Filters {
		m[code] = strings.Join(vals, "#|")
	}
	if p.MinPrice != nil {
		m["range-price-min"] = strconv.FormatFloat(*p.MinPrice, 'f', -1, 64)
	}
	if p.MaxPrice != nil {
		m["range-price-max"] = strconv.FormatFloat(*p.MaxPrice, 'f', -1, 64)
	}
	if len(m) == 0 {
		return ""
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}

func (p ListParams) request() request {
	q := url.Values{}
	r := request{store: p.Store}
	if p.Category != "" {
		r.path, r.endpoint = p.Category, "GET /{category}/"
	} else {
		r.path, r.endpoint = "/search", "GET /search"
		q.Set("q", p.Query)
	}
	if f := p.filtersParam(); f != "" {
		q.Set("filters", f)
	}
	if p.Sort != "" && p.Sort != "relevance" {
		q.Set("sort", p.Sort)
	}
	if p.Page > 1 {
		q.Set("p", strconv.Itoa(p.Page))
	}
	r.query = q
	return r
}

// CategoryPath normalises a category argument (path or full URL) to the
// site path with leading and trailing slashes.
func CategoryPath(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "http") {
		u, err := url.Parse(s)
		if err != nil || !strings.HasSuffix(u.Hostname(), "obramat.es") {
			return "", usagef("%q is not an obramat.es category URL", s)
		}
		s = u.Path
	}
	s = "/" + strings.Trim(s, "/") + "/"
	if s == "//" || strings.HasPrefix(s, "/productos/") || strings.HasPrefix(s, "/almacenes/") {
		return "", usagef("%q is not a category path. See `obramat category list`", s)
	}
	return s, nil
}

// List fetches one search or category page, including the cards the site
// loads lazily, in the order the site ranks them.
func (c *Client) List(ctx context.Context, p ListParams) (*Listing, error) {
	r := p.request()
	resp, err := c.get(ctx, r)
	if err != nil {
		return nil, err
	}
	if resp.status == 404 {
		if p.Category != "" {
			return nil, notFoundf("no category at %s. See `obramat category list`", p.Category)
		}
		return nil, notFoundf("search page not found")
	}
	page := string(resp.body)
	l := &Listing{Query: p.Query, Category: p.Category, Page: max(p.Page, 1), Sort: p.Sort}
	if l.Sort == "" {
		l.Sort = "relevance"
	}
	if p.Store.ID > 0 {
		s := p.Store
		l.Store = &s
	}
	// A search for an exact product reference redirects to that product.
	if strings.HasPrefix(resp.url.Path, "/productos/") {
		d, err := parseProduct(page, resp.url.Path, p.Store)
		if err != nil {
			return nil, err
		}
		l.Page, l.Pages, l.Total, l.Products = 1, 1, 1, []Product{d.Product}
		return l, nil
	}
	if p.Category == "" && resp.url.Path != "/search" {
		l.Category = resp.url.Path // the site may send a query to its category
	}
	ssr, err := parseSSR(page, r.endpoint)
	if err != nil {
		if p.Category != "" && jsonIsland(page, "guidance-ssr-data") == "" {
			return nil, usagef("%s is a landing page with no product list; pick one of its subcategories (`obramat category list --parent %s`)", p.Category, p.Category)
		}
		if e, ok := err.(*Error); ok && e.Kind == KindNotFound && l.Page > 1 {
			e.Msg = fmt.Sprintf("there is no page %d (the site serves at most 10 pages per listing)", l.Page)
		}
		return nil, err
	}
	l.Pages, l.Total = ssr.Pagination.NumberOfPages, ssr.Products.Number
	l.Filters = ssr.filters()
	for _, s := range ssr.Sorts {
		if s.Applied {
			l.Sort = s.Code
		}
	}

	cards, lazy := parseCards(page, p.Store)
	var codes []string
	for _, g := range ssr.Products.Groups {
		codes = append(codes, g.Codes...)
	}
	for _, path := range sortedKeys(lazy) {
		missing := false
		for _, ref := range lazy[path] {
			if _, ok := cards[ref]; !ok {
				missing = true
			}
		}
		if !missing {
			continue
		}
		fr, err := c.get(ctx, request{path: path, store: p.Store, endpoint: "GET /lazy/ProductThumbnailFragment"})
		if err != nil {
			return nil, err
		}
		more, _ := parseCards(string(fr.body), p.Store)
		for ref, pr := range more {
			if _, ok := cards[ref]; !ok {
				cards[ref] = pr
			}
		}
	}
	l.Products = make([]Product, 0, len(codes))
	for _, ref := range codes {
		pr, ok := cards[ref]
		if !ok {
			pr = Product{Ref: ref}
			if l.Store != nil {
				pr.Store = l.Store
			}
		}
		l.Products = append(l.Products, pr)
	}
	return l, nil
}

// ssrData is the guidance-ssr-data island search and category pages carry.
type ssrData struct {
	SSRData struct {
		Type string   `json:"ssrDataType"`
		Body *ssrBody `json:"body"`
	} `json:"ssrData"`
}

type ssrBody struct {
	Pagination struct {
		CurrentPage   int `json:"currentPage"`
		NumberOfPages int `json:"numberOfPages"`
	} `json:"pagination"`
	Products struct {
		Groups []struct {
			ID    string   `json:"id"`
			Codes []string `json:"codes"`
			Lazy  bool     `json:"lazy"`
		} `json:"groups"`
		Number int `json:"number"`
	} `json:"products"`
	Sorts []struct {
		Code    string `json:"code"`
		Applied bool   `json:"applied"`
	} `json:"sorts"`
	Filters struct {
		StockAndDelivery []ssrFilter `json:"stockAndDeliveryFilters"`
		Product          []ssrFilter `json:"productFilters"`
	} `json:"filters"`
}

type ssrFilter struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Label   string `json:"label"`
	Unit    string `json:"unit"`
	Applied bool   `json:"applied"`
	Values  []struct {
		Value   string `json:"value"`
		Total   int    `json:"total"`
		Applied bool   `json:"applied"`
	} `json:"values"`
}

// parseSSR decodes the island and checks it is a product listing.
func parseSSR(page, endpoint string) (*ssrBody, error) {
	island := jsonIsland(page, "guidance-ssr-data")
	if island == "" {
		return nil, changed(endpoint, "no guidance-ssr-data island", nil)
	}
	var d ssrData
	if err := json.Unmarshal([]byte(island), &d); err != nil {
		return nil, changed(endpoint, "guidance-ssr-data: "+err.Error(), []byte(island))
	}
	switch d.SSRData.Type {
	case "browseProducts":
	case "dependencyError":
		// The site answers this past its last page (p=11 and up).
		return nil, notFoundf("obramat.es has no results for that page")
	default:
		return nil, changed(endpoint, "ssrDataType "+strconv.Quote(d.SSRData.Type), []byte(island))
	}
	if d.SSRData.Body == nil {
		return nil, changed(endpoint, "guidance-ssr-data without body", []byte(island))
	}
	return d.SSRData.Body, nil
}

func (b *ssrBody) filters() []Filter {
	var out []Filter
	for _, f := range append(append([]ssrFilter{}, b.Filters.StockAndDelivery...), b.Filters.Product...) {
		ff := Filter{Code: f.Code, Label: f.Label, Type: f.Type, Unit: f.Unit, Applied: f.Applied}
		for _, v := range f.Values {
			ff.Values = append(ff.Values, FilterValue{Value: v.Value, Count: v.Total, Applied: v.Applied})
		}
		out = append(out, ff)
	}
	return out
}

var (
	reCardStart   = regexp.MustCompile(`<li\s+class="product-thumbnail product-thumbnail-item`)
	reCardClass   = regexp.MustCompile(`^<li\s+class="([^"]*)"`)
	reCardRef     = regexp.MustCompile(`data-reflm="(\d+)"`)
	reLazyURL     = regexp.MustCompile(`data-lazy-url="([^"]+)"`)
	reDesignation = regexp.MustCompile(`<a href="([^"]+)" title="([^"]*)" class="a-designation`)
	reCardImage   = regexp.MustCompile(`<img src="([^"]+)"[^>]*class="a-illustration__img"`)
	reCardBrand   = regexp.MustCompile(`(?s)class="o-thumbnail__brand">\s*<img alt="([^"]*)"`)
	reFlag        = regexp.MustCompile(`<span class="a-flag__label">([^<]+)</span>`)
	reStatusLabel = regexp.MustCompile(`(?s)<span class="stock-status_label">(.*?)</span>\s*</p>`)
	reInStock     = regexp.MustCompile(`([\d.]+)\s+en stock en\s+(.+)`)
)

// parseCards reads the product cards in a page or lazy fragment. Placeholder
// cards carry only a ref and the URL of the fragment that renders their
// group; those come back in lazy, keyed by fragment path.
func parseCards(page string, store StoreRef) (map[string]Product, map[string][]string) {
	cards := map[string]Product{}
	lazy := map[string][]string{}
	idx := reCardStart.FindAllStringIndex(page, -1)
	for i, loc := range idx {
		end := len(page)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		card := page[loc[0]:end]
		ref := first(reCardRef, card)
		if ref == "" {
			continue
		}
		if strings.Contains(first(reCardClass, card), "placeholder") {
			if u := first(reLazyURL, card); u != "" {
				u = html.UnescapeString(u)
				lazy[u] = append(lazy[u], ref)
			}
			continue
		}
		if _, dup := cards[ref]; dup {
			continue
		}
		cards[ref] = parseCard(ref, card, store)
	}
	return cards, lazy
}

func parseCard(ref, card string, store StoreRef) Product {
	p := Product{Ref: ref}
	if m := reDesignation.FindStringSubmatch(card); m != nil {
		p.URL = abs(html.UnescapeString(m[1]))
		p.Title = text(m[2])
	}
	p.Image = stripQuery(first(reCardImage, card))
	p.Brand = text(first(reCardBrand, card))
	for _, m := range reFlag.FindAllStringSubmatch(card, -1) {
		p.Badges = appendUnique(p.Badges, text(m[1]))
	}
	p.Price = parsePrice(card)
	if store.ID > 0 {
		s := store
		p.Store = &s
		p.Stock = &Stock{}
	}
	for _, m := range reStatusLabel.FindAllStringSubmatch(card, -1) {
		label := text(m[1])
		if sm := reInStock.FindStringSubmatch(label); sm != nil && p.Stock != nil {
			setQuantity(p.Stock, sm[1], label)
			if p.Store.Name == "" || p.Store.Name == "Obramat" {
				p.Store.Name = strings.TrimSpace(sm[2])
			}
			continue
		}
		p.Delivery = appendUnique(p.Delivery, label)
	}
	if p.Stock != nil && p.Stock.Quantity == nil && p.Price == nil {
		p.Stock.Label = "not sold at this store"
	}
	return p
}

func setQuantity(s *Stock, raw, label string) {
	if n, ok := number(raw); ok {
		q := int(n)
		s.Quantity = &q
		s.Available = q > 0
	}
	s.Label = label
}

// rePriceLine finds the screen-reader text of each price line by its class.
var rePriceLine = map[string]*regexp.Regexp{}

func priceLine(page, class string) (amount float64, tax, unit string, ok bool) {
	t := text(first(rePriceLine[class], page))
	m := reSold.FindStringSubmatch(t)
	if m == nil {
		return 0, "", "", false
	}
	amount, ok = number(m[1])
	tax = "IVA"
	if strings.Contains(m[2], "IGIC") {
		tax = "IGIC"
	}
	return amount, tax, strings.TrimSpace(m[3]), ok
}

// reSold reads the screen-reader text of a price line, for example
// "Vendido 28 € IVA por cajas", "Vendido 1.596 € IVA" or, for Canary
// Islands prices, "Vendido 35 € IGIC incluido por Unidad".
var reSold = regexp.MustCompile(`Vendido\s+([\d.,]+)\s*€\s*(.*?)(?:\s+por\s+(.*))?$`)

func init() {
	for _, c := range []string{"-main", "-secondary", "-mainSub", "-secondarySub"} {
		rePriceLine[c] = regexp.MustCompile(`(?s)<p class="m-price ` + regexp.QuoteMeta(c) + `[ "].*?<span class="kl-hidden-accessibility">(.*?)</span>`)
	}
}

// parsePrice reads the first price block in s: the main line with VAT, its
// ex-VAT twin, and the per-measure comparison lines when present.
func parsePrice(s string) *Price {
	amt, tax, unit, ok := priceLine(s, "-main")
	if !ok {
		return nil
	}
	p := &Price{Amount: amt, Tax: tax, Unit: unit, Currency: "EUR"}
	if v, _, _, ok := priceLine(s, "-mainSub"); ok {
		p.AmountExVAT = &v
	}
	if v, _, u, ok := priceLine(s, "-secondary"); ok && u != "" {
		p.PerMeasure = &MeasurePrice{Amount: v, Unit: u}
		if w, _, _, ok := priceLine(s, "-secondarySub"); ok {
			p.PerMeasure.AmountExVAT = &w
		}
	}
	return p
}

func appendUnique(list []string, s string) []string {
	if s == "" {
		return list
	}
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// sortedKeys gives map iteration a stable order.
func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
