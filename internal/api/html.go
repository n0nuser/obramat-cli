package api

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// The site is server-rendered HTML with a few JSON islands. Pages are parsed
// with targeted regular expressions anchored on the site's own class names and
// data attributes, which have proven more stable than document structure.

var (
	reTags   = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces = regexp.MustCompile(`\s+`)
)

// text strips tags, decodes entities and collapses whitespace.
func text(s string) string {
	s = reTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// first returns submatch 1 of re in s, or "".
func first(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// number parses a Spanish-formatted amount: "1.596", "8,96", "2.127".
func number(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// jsonIsland returns the body of <script type="application/json" id="ID">.
// The pattern depends only on the id, so it is compiled once per id rather than
// once per page: every page read asks for at least one island.
func jsonIsland(page, id string) string {
	re, ok := islandRes.Load(id)
	if !ok {
		re, _ = islandRes.LoadOrStore(id, regexp.MustCompile(`(?s)<script type="application/(?:ld\+)?json" id="`+regexp.QuoteMeta(id)+`">(.*?)</script>`))
	}
	return first(re.(*regexp.Regexp), page)
}

var islandRes sync.Map // id -> *regexp.Regexp

// stripQuery drops ?width=... from image URLs so they point at the original.
func stripQuery(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}
