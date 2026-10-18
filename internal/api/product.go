package api

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

type Attribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Crumb struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ProductDetail is a product page. The embedded Product carries price and
// stock for the store in context, like a listing card.
type ProductDetail struct {
	Product
	EAN         string      `json:"ean,omitempty"`
	Description string      `json:"description,omitempty"`
	Category    string      `json:"category,omitempty"` // path of the deepest category
	Breadcrumb  []Crumb     `json:"breadcrumb,omitempty"`
	Attributes  []Attribute `json:"attributes,omitempty"`
	// Buyable is the site's own add-to-order flag for this store context.
	Buyable bool `json:"buyable"`
}

var reRef = regexp.MustCompile(`^\d{6,10}$`)

// IsRef reports whether s looks like a product reference (the "Ref." number).
func IsRef(s string) bool { return reRef.MatchString(s) }

// ProductPath resolves a product argument: a reference, a product URL or a
// /productos/... path. A bare reference needs one search round trip, since
// the site redirects an exact-reference search to the product page.
func (c *Client) ProductPath(ctx context.Context, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if IsRef(arg) {
		resp, err := c.get(ctx, request{path: "/search", query: url.Values{"q": {arg}}, endpoint: "GET /search"})
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(resp.url.Path, "/productos/") {
			return "", notFoundf("no product with reference %s", arg)
		}
		return resp.url.Path, nil
	}
	if strings.HasPrefix(arg, "http") {
		u, err := url.Parse(arg)
		if err != nil || !strings.HasSuffix(u.Hostname(), "obramat.es") {
			return "", usagef("%q is not an obramat.es product URL", arg)
		}
		arg = u.Path
	}
	if !strings.HasPrefix(arg, "/productos/") || !strings.HasSuffix(arg, ".html") {
		return "", usagef("%q is neither a product reference (digits, as in `Ref. 25064668`) nor a product URL", arg)
	}
	return arg, nil
}

// Product fetches one product page for the given store context.
func (c *Client) Product(ctx context.Context, arg string, store StoreRef) (*ProductDetail, error) {
	path, err := c.ProductPath(ctx, arg)
	if err != nil {
		return nil, err
	}
	resp, err := c.get(ctx, request{path: path, store: store, endpoint: "GET /productos/{slug}.html"})
	if err != nil {
		return nil, err
	}
	if resp.status == 404 {
		return nil, notFoundf("no product at %s", path)
	}
	return parseProduct(string(resp.body), resp.url.Path, store)
}

var (
	rePageRef     = regexp.MustCompile(`<span class="js-product-ref">(\d+)</span>`)
	reAttr        = regexp.MustCompile(`(?s)<th class="m-product-attr-row__name" scope="row">(.*?)</th>\s*<td class="m-product-attr-row__value">(.*?)</td>`)
	reTitleH1     = regexp.MustCompile(`l-product-detail-presentation__title`)
	reAddToCart   = regexp.MustCompile(`"add_to_cart_availability":(true|false)`)
	reAvailItem   = regexp.MustCompile(`(?s)data-cerberus="ELEM_availability-([a-z-]+)"\s*>(.*?)</ul>`)
	reUnavailable = regexp.MustCompile(`no está disponible`)
)

func parseProduct(page, path string, store StoreRef) (*ProductDetail, error) {
	const endpoint = "GET /productos/{slug}.html"
	var ld struct {
		Name        string `json:"name"`
		SKU         string `json:"sku"`
		GTIN        string `json:"gtin"`
		Image       string `json:"image"`
		Description string `json:"description"`
	}
	island := jsonIsland(page, "jsonld_PRODUCT")
	if island == "" {
		return nil, changed(endpoint, "no jsonld_PRODUCT", nil)
	}
	if err := json.Unmarshal([]byte(island), &ld); err != nil {
		return nil, changed(endpoint, "jsonld_PRODUCT: "+err.Error(), []byte(island))
	}
	d := &ProductDetail{EAN: ld.GTIN, Description: text(ld.Description)}
	d.Ref = first(rePageRef, page)
	if d.Ref == "" {
		d.Ref = ld.SKU
	}
	if d.Ref == "" || ld.Name == "" {
		return nil, changed(endpoint, "product without reference or name", []byte(island))
	}
	d.Title, d.URL, d.Image = text(ld.Name), abs(path), ld.Image

	var crumbs struct {
		Items []struct {
			Item struct {
				Name string `json:"name"`
				ID   string `json:"@id"`
			} `json:"item"`
		} `json:"itemListElement"`
	}
	if err := json.Unmarshal([]byte(jsonIsland(page, "jsonld_BREADCRUMB")), &crumbs); err == nil {
		for _, it := range crumbs.Items {
			d.Breadcrumb = append(d.Breadcrumb, Crumb{Name: text(it.Item.Name), URL: it.Item.ID})
		}
		// The last crumb is the product itself, the one before it its category.
		if n := len(d.Breadcrumb); n >= 3 {
			if u, err := url.Parse(d.Breadcrumb[n-2].URL); err == nil {
				d.Category = u.Path
			}
		}
	}
	for _, m := range reAttr.FindAllStringSubmatch(page, -1) {
		a := Attribute{Name: text(m[1]), Value: text(m[2])}
		d.Attributes = append(d.Attributes, a)
		if a.Name == "Marca" && d.Brand == "" {
			d.Brand = a.Value
		}
	}
	d.Buyable = first(reAddToCart, page) == "true"

	// Price and availability live between the title and the "how to buy"
	// block; recommendation carousels further down carry prices of their own.
	region := page
	if loc := reTitleH1.FindStringIndex(page); loc != nil {
		region = page[loc[0]:]
		if end := strings.Index(region, "mc-accordion"); end > 0 {
			region = region[:end]
		}
	}
	d.Price = parsePrice(region)
	if store.ID > 0 {
		s := store
		d.Store = &s
		d.Stock = &Stock{}
	}
	for _, m := range reAvailItem.FindAllStringSubmatch(region, -1) {
		label := text(m[2])
		label = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(label, "Compra en Almacén"), "Recogida"), "Envío"))
		if m[1] == "in-store" {
			if sm := reInStock.FindStringSubmatch(label); sm != nil && d.Stock != nil {
				setQuantity(d.Stock, sm[1], label)
				if d.Store.Name == "" || d.Store.Name == "Obramat" {
					d.Store.Name = strings.TrimSpace(sm[2])
				}
			}
			continue
		}
		d.Delivery = appendUnique(d.Delivery, label)
	}
	if d.Stock != nil && d.Stock.Quantity == nil && reUnavailable.MatchString(region) {
		zero := 0
		d.Stock.Quantity, d.Stock.Label = &zero, "no disponible en este Almacén"
		d.Price = nil // the page shows no price for the store
	}
	return d, nil
}
