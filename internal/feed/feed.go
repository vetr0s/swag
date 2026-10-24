// Package feed encodes posts as an RSS 2.0 feed. Encoding goes through
// encoding/xml structs so escaping is correct by construction.
package feed

import (
	"cmp"
	"encoding/xml"
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
	out, err := xml.MarshalIndent(rss{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Channel: c}, "", "  ")
	if err != nil {
		panic(err) // only reachable through a bug in the structs above
	}
	return append(append([]byte(xml.Header), out...), '\n')
}
