// Package feed encodes posts as an RSS 2.0 feed and pages as a sitemap.
// Encoding goes through encoding/xml structs so escaping is correct by
// construction.
package feed

import (
	"cmp"
	"encoding/xml"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/vetr0s/swag/internal/content"
)

type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Atom    string   `xml:"xmlns:atom,attr"`
	Channel channel  `xml:"channel"`
}

type channel struct {
	Title         string   `xml:"title"`
	Link          string   `xml:"link"`
	Description   string   `xml:"description"`
	Language      string   `xml:"language,omitempty"`
	LastBuildDate string   `xml:"lastBuildDate"`
	AtomLink      atomLink `xml:"atom:link"`
	Items         []item   `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        guid   `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Description string `xml:"description"`
}

type guid struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	URL         string `xml:",chardata"`
}

// RSS encodes posts, which must be non-empty and sorted newest first.
// siteURL has no trailing slash.
func RSS(title, siteURL, description, language string, posts []*content.Page) []byte {
	c := channel{
		Title:         title,
		Link:          siteURL + "/",
		Description:   description,
		Language:      language,
		LastBuildDate: posts[0].Date.Format(time.RFC1123Z),
		AtomLink:      atomLink{Href: siteURL + "/rss.xml", Rel: "self", Type: "application/rss+xml"},
	}
	for _, p := range posts {
		u := siteURL + p.URL
		c.Items = append(c.Items, item{
			Title:       p.Title,
			Link:        u,
			GUID:        guid{IsPermaLink: "true", URL: u},
			PubDate:     p.Date.Format(time.RFC1123Z),
			Description: cmp.Or(string(p.Content), p.Description),
		})
	}
	return encode(rss{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Channel: c})
}

type urlset struct {
	XMLName xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

// Sitemap lists pages in URL order, with their date as lastmod when set.
func Sitemap(siteURL string, pages []*content.Page) []byte {
	pages = slices.SortedFunc(slices.Values(pages), func(a, b *content.Page) int { return strings.Compare(a.URL, b.URL) })
	var s urlset
	for _, p := range pages {
		u := sitemapURL{Loc: siteURL + (&url.URL{Path: p.URL}).EscapedPath()}
		if !p.Date.IsZero() {
			u.LastMod = p.Date.Format("2006-01-02")
		}
		s.URLs = append(s.URLs, u)
	}
	return encode(s)
}

func encode(v any) []byte {
	out, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err) // only reachable through a bug in the structs above
	}
	return append(append([]byte(xml.Header), out...), '\n')
}
