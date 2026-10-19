package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Store is one Obramat warehouse. ID is the store code the site uses in its
// store cookie and stock cards (Usera is 12). The store API also has an
// internal id (Usera is 53); it is never exposed.
type Store struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Slug       string   `json:"slug,omitempty"`
	URL        string   `json:"url,omitempty"`
	Street     string   `json:"street,omitempty"`
	Postcode   string   `json:"postcode,omitempty"`
	City       string   `json:"city,omitempty"`
	Lat        float64  `json:"lat,omitempty"`
	Lng        float64  `json:"lng,omitempty"`
	Phone      string   `json:"phone,omitempty"`
	Email      string   `json:"email,omitempty"`
	Open       *bool    `json:"open,omitempty"`
	Hours      string   `json:"hours,omitempty"`
	DistanceKm *float64 `json:"distance_km,omitempty"`
}

// StoreRef identifies the store a price or stock figure is for.
func (s Store) Ref() StoreRef { return StoreRef{ID: s.ID, Name: s.Name} }

type nearbyResponse struct {
	Stores []struct {
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Opening struct {
			IsOpen *bool  `json:"isOpen"`
			Label  string `json:"label"`
		} `json:"opening"`
		Original struct {
			PoiID   int `json:"poiId"`
			Address struct {
				Line2       string `json:"addressLine2"`
				ZipCode     string `json:"zipCode"`
				City        string `json:"city"`
				Coordinates struct {
					Latitude  float64 `json:"latitude"`
					Longitude float64 `json:"longitude"`
				} `json:"coordinates"`
			} `json:"address"`
			Contacts []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"contacts"`
			WebSite struct {
				URL string `json:"url"`
			} `json:"webSite"`
			Distance *float64 `json:"distance"`
		} `json:"originalStore"`
	} `json:"stores"`
}

var reStoreSlug = regexp.MustCompile(`/almacenes/([a-z0-9-]+)\.html`)

// Nearby returns the stores closest to a point, nearest first (the site
// returns six).
func (c *Client) Nearby(ctx context.Context, lat, lng float64) ([]Store, error) {
	const endpoint = "GET /fox--microfront--store/api/v1/stores-nearby-by-distance"
	q := url.Values{
		"latitude":  {strconv.FormatFloat(lat, 'f', 4, 64)},
		"longitude": {strconv.FormatFloat(lng, 'f', 4, 64)},
		"isCcdp":    {"true"},
	}
	resp, err := c.get(ctx, request{path: "/fox--microfront--store/api/v1/stores-nearby-by-distance", query: q, json: true, endpoint: endpoint})
	if err != nil {
		return nil, err
	}
	return parseNearby(resp.body)
}

func parseNearby(body []byte) ([]Store, error) {
	const endpoint = "GET /fox--microfront--store/api/v1/stores-nearby-by-distance"
	var r nearbyResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, changed(endpoint, err.Error(), body)
	}
	out := make([]Store, 0, len(r.Stores))
	for _, s := range r.Stores {
		o := s.Original
		if o.PoiID == 0 || s.Name == "" {
			return nil, changed(endpoint, "store without poiId or name", body)
		}
		st := Store{
			ID: o.PoiID, Name: strings.TrimSpace(s.Name), Street: o.Address.Line2, Postcode: o.Address.ZipCode,
			City: strings.TrimSpace(o.Address.City), Lat: o.Address.Coordinates.Latitude, Lng: o.Address.Coordinates.Longitude,
			Phone: s.Phone, Open: s.Opening.IsOpen, Hours: s.Opening.Label, DistanceKm: o.Distance,
		}
		for _, ct := range o.Contacts {
			if ct.Type == "EMAIL" {
				st.Email = ct.Value
			}
		}
		// webSite.url is not always filled in (some stores carry the home
		// page or localhost), so fall back to the name, which is how the
		// sitemap slugs are built.
		st.Slug = first(reStoreSlug, o.WebSite.URL)
		if st.Slug == "" {
			st.Slug = Slugify(st.Name)
		}
		st.URL = abs("/almacenes/" + st.Slug + ".html")
		out = append(out, st)
	}
	return out, nil
}

// Slugify builds a store slug from its name: "Obramat Leganés" becomes
// "obramat-leganes".
func Slugify(s string) string {
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n", "à", "a", "è", "e", "ò", "o", "ç", "c")
	s = r.Replace(strings.ToLower(strings.TrimSpace(s)))
	var b strings.Builder
	dash := false
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// StoreSlugs lists every store page in the site's store sitemap.
func (c *Client) StoreSlugs(ctx context.Context) ([]string, error) {
	resp, err := c.get(ctx, request{path: "/sitemap-store1.xml", endpoint: "GET /sitemap-store1.xml"})
	if err != nil {
		return nil, err
	}
	return parseStoreSitemap(resp.body)
}

func parseStoreSitemap(body []byte) ([]string, error) {
	var slugs []string
	for _, m := range reStoreSlug.FindAllStringSubmatch(string(body), -1) {
		slugs = appendUnique(slugs, m[1])
	}
	if len(slugs) == 0 {
		return nil, changed("GET /sitemap-store1.xml", "no store URLs", body)
	}
	return slugs, nil
}

var (
	reStorePageID = regexp.MustCompile(`data-store-id="(\d+)"`)
)

// StorePage reads one store's own page: name, coordinates, phone, locality.
func (c *Client) StorePage(ctx context.Context, slug string) (*Store, error) {
	resp, err := c.get(ctx, request{path: "/almacenes/" + slug + ".html", endpoint: "GET /almacenes/{slug}.html"})
	if err != nil {
		return nil, err
	}
	if resp.status == 404 {
		return nil, notFoundf("no store page %q. See `obramat store list`", slug)
	}
	return parseStorePage(string(resp.body), slug)
}

func parseStorePage(page, slug string) (*Store, error) {
	const endpoint = "GET /almacenes/{slug}.html"
	var ld struct {
		Name string `json:"name"`
		Geo  struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"geo"`
		Address struct {
			Street   string `json:"streetAddress"`
			Locality string `json:"addressLocality"`
		} `json:"address"`
		Telephone string `json:"telephone"`
	}
	island := jsonIsland(page, "jsonld_STORE")
	if err := json.Unmarshal([]byte(island), &ld); err != nil || ld.Name == "" {
		return nil, changed(endpoint, "jsonld_STORE missing or unreadable", []byte(island))
	}
	id, _ := strconv.Atoi(first(reStorePageID, page))
	if id == 0 {
		return nil, changed(endpoint, "no data-store-id", nil)
	}
	return &Store{ID: id, Name: strings.TrimSpace(ld.Name), Slug: slug, URL: abs("/almacenes/" + slug + ".html"),
		Street: ld.Address.Street, City: strings.TrimSpace(ld.Address.Locality), Lat: ld.Geo.Latitude, Lng: ld.Geo.Longitude,
		Phone: ld.Telephone}, nil
}

// sweepSeeds are points spread over Spain. Asking for the stores near each
// covers almost every store in a dozen requests; AllStores reads the pages of
// any the sweep missed.
var sweepSeeds = [][2]float64{
	{40.42, -3.70}, {41.39, 2.17}, {39.47, -0.38}, {37.39, -5.98}, {36.72, -4.42}, {43.26, -2.93},
	{41.65, -0.88}, {41.65, -4.72}, {43.36, -5.85}, {42.88, -8.54}, {39.57, 2.65}, {28.10, -15.42},
	{38.00, -1.13}, {36.84, -2.46}, {41.98, 2.82},
}

// AllStores lists every store with its code and coordinates, sorted by name.
// It costs one sitemap read, about fifteen nearby lookups and one page read
// per store the lookups missed.
func (c *Client) AllStores(ctx context.Context) ([]Store, error) {
	defer c.sweep()()
	slugs, err := c.StoreSlugs(ctx)
	if err != nil {
		return nil, err
	}
	bySlug := map[string]Store{}
	byID := map[int]bool{}
	for _, p := range sweepSeeds {
		near, err := c.Nearby(ctx, p[0], p[1])
		if err != nil {
			return nil, err
		}
		for _, s := range near {
			s.DistanceKm, s.Open, s.Hours = nil, nil, ""
			if !byID[s.ID] {
				byID[s.ID] = true
				bySlug[s.Slug] = s
			}
		}
	}
	for _, slug := range slugs {
		if _, ok := bySlug[slug]; ok {
			continue
		}
		s, err := c.StorePage(ctx, slug)
		if err != nil {
			return nil, fmt.Errorf("store %s: %w", slug, err)
		}
		if !byID[s.ID] {
			byID[s.ID] = true
			bySlug[slug] = *s
		}
	}
	out := make([]Store, 0, len(bySlug))
	for _, s := range bySlug {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// StoreStock is one store's stock of a product, as the site's "availability
// by store" drawer shows it.
type StoreStock struct {
	Store      StoreRef `json:"store"`
	Slug       string   `json:"slug,omitempty"`
	DistanceKm *float64 `json:"distance_km,omitempty"`
	Stock      Stock    `json:"stock"`
}

var (
	reStockCard     = regexp.MustCompile(`(?s)<article class="m-store-search-card".*?</article>`)
	reStockCardName = regexp.MustCompile(`m-store-info-header--title" role="heading" aria-level="3">([^<]+)<`)
	reStockCardDist = regexp.MustCompile(`m-store-info-header__store-distance">([^<]+)<`)
	reStockCardText = regexp.MustCompile(`(?s)<div class="stock-status_text">(.*?)</div>`)
	reStockCardQty  = regexp.MustCompile(`(?i)([\d.]+)\s+en stock`)
	reStockCardID   = regexp.MustCompile(`data-store-id="(\d+)"`)
	reStockBadge    = regexp.MustCompile(`stock-status_badge--(\w+)`)
)

// StockNear returns a product's stock at the six stores nearest a point.
func (c *Client) StockNear(ctx context.Context, ref string, lat, lng float64) ([]StoreStock, error) {
	q := url.Values{
		"latitude":        {strconv.FormatFloat(lat, 'f', 4, 64)},
		"longitude":       {strconv.FormatFloat(lng, 'f', 4, 64)},
		"productRef":      {ref},
		"storeSearchType": {"STOCK"},
	}
	resp, err := c.get(ctx, request{path: "/store-header-module/services/contextlayer/store-search-result", query: q,
		endpoint: "GET /store-header-module/services/contextlayer/store-search-result"})
	if err != nil {
		return nil, err
	}
	return parseStockCards(resp.body)
}

func parseStockCards(body []byte) ([]StoreStock, error) {
	page := string(body)
	if !strings.Contains(page, "m-store-search-result") {
		return nil, changed("GET /store-header-module/services/contextlayer/store-search-result", "no store-search-result section", body)
	}
	var out []StoreStock
	for _, card := range reStockCard.FindAllString(page, -1) {
		id, _ := strconv.Atoi(first(reStockCardID, card))
		ss := StoreStock{Store: StoreRef{ID: id, Name: text(first(reStockCardName, card))}, Slug: first(reStoreSlug, card)}
		if d, ok := number(strings.TrimSuffix(text(first(reStockCardDist, card)), " km")); ok {
			ss.DistanceKm = &d
		}
		label := text(first(reStockCardText, card))
		ss.Stock.Label = label
		if m := reStockCardQty.FindStringSubmatch(label); m != nil {
			setQuantity(&ss.Stock, m[1], label)
		} else if strings.Contains(strings.ToLower(label), "no dispon") {
			zero := 0
			ss.Stock.Quantity = &zero
		} else {
			// No number: trust the badge colour the site draws next to it.
			ss.Stock.Available = first(reStockBadge, card) == "green"
		}
		if id == 0 || ss.Store.Name == "" {
			return nil, changed("GET /store-header-module/services/contextlayer/store-search-result", "store card without id or name", []byte(card))
		}
		out = append(out, ss)
	}
	return out, nil
}

// StockEverywhere asks for a product's stock around each store until every
// store in stores has an answer. Each request answers for six stores, so a
// full sweep is about a third as many requests as there are stores.
func (c *Client) StockEverywhere(ctx context.Context, ref string, stores []Store) ([]StoreStock, error) {
	defer c.sweep()()
	got := map[int]StoreStock{}
	for _, s := range stores {
		if _, ok := got[s.ID]; ok || (s.Lat == 0 && s.Lng == 0) {
			continue
		}
		near, err := c.StockNear(ctx, ref, s.Lat, s.Lng)
		if err != nil {
			return nil, err
		}
		for _, ss := range near {
			if _, ok := got[ss.Store.ID]; !ok {
				ss.DistanceKm = nil // relative to a sweep point, meaningless here
				got[ss.Store.ID] = ss
			}
		}
	}
	out := make([]StoreStock, 0, len(got))
	for _, ss := range got {
		out = append(out, ss)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Store.Name < out[j].Store.Name })
	return out, nil
}
