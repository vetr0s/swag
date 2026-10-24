package feed

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"swag/internal/content"
)

func TestRSS(t *testing.T) {
	posts := []*content.Page{
		{Title: "B & <b>", URL: "/posts/b.html", Date: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), Content: "<p>b</p>"},
		{Title: "A", URL: "/posts/a.html", Date: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Description: "only a description"},
	}
	out := RSS("Site", "https://example.com", "Desc", "en", posts)
	if !bytes.HasPrefix(out, []byte(`<?xml version="1.0" encoding="UTF-8"?>`+"\n<rss version=\"2.0\" xmlns:atom=\"http://www.w3.org/2005/Atom\">")) {
		t.Errorf("header:\n%s", out)
	}
	if !bytes.HasSuffix(out, []byte("</rss>\n")) || bytes.HasSuffix(out, []byte("\n\n")) {
		t.Error("want exactly one trailing newline")
	}
	for _, want := range []string{
		"<lastBuildDate>Wed, 04 Mar 2026 00:00:00 +0000</lastBuildDate>",
		`<atom:link href="https://example.com/rss.xml" rel="self" type="application/rss+xml">`,
		"<title>B &amp; &lt;b&gt;</title>",
		`<guid isPermaLink="true">https://example.com/posts/b.html</guid>`,
		"<pubDate>Fri, 02 Jan 2026 00:00:00 +0000</pubDate>",
		"<description>&lt;p&gt;b&lt;/p&gt;</description>",
		"<description>only a description</description>",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	var parsed struct {
		Items []struct {
			Title       string `xml:"title"`
			Description string `xml:"description"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Items) != 2 || parsed.Items[0].Title != "B & <b>" || parsed.Items[0].Description != "<p>b</p>" {
		t.Errorf("round trip: %+v", parsed)
	}
	if strings.Count(string(out), "<item>") != 2 {
		t.Error("want two items")
	}
}
