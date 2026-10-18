package api

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// Category is one node of the site's category tree, taken from its category
// sitemap. The sitemap carries no display names, so Name is the last path
// segment made readable; the page title can differ in accents and case.
type Category struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Depth    int    `json:"depth"`
	Parent   string `json:"parent,omitempty"`
	Children int    `json:"children"`
}

var reCategoryLoc = regexp.MustCompile(`<loc>https?://[^/<]+(/[^<]*/)</loc>`)

// Categories lists the whole tree, sorted by path so parents precede children.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	resp, err := c.get(ctx, request{path: "/sitemap-category1.xml", endpoint: "GET /sitemap-category1.xml"})
	if err != nil {
		return nil, err
	}
	return parseCategorySitemap(resp.body)
}

func parseCategorySitemap(body []byte) ([]Category, error) {
	paths := map[string]bool{}
	for _, m := range reCategoryLoc.FindAllStringSubmatch(string(body), -1) {
		paths[m[1]] = true
	}
	if len(paths) == 0 {
		return nil, changed("GET /sitemap-category1.xml", "no category URLs", body)
	}
	children := map[string]int{}
	out := make([]Category, 0, len(paths))
	for p := range paths {
		segs := strings.Split(strings.Trim(p, "/"), "/")
		c := Category{Path: p, Depth: len(segs), Name: strings.ReplaceAll(segs[len(segs)-1], "-", " ")}
		if len(segs) > 1 {
			c.Parent = "/" + strings.Join(segs[:len(segs)-1], "/") + "/"
			children[c.Parent]++
		}
		out = append(out, c)
	}
	for i := range out {
		out[i].Children = children[out[i].Path]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
