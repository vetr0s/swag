// Package linkcheck finds internal links in generated HTML that point at
// files missing from the output tree, or at fragments missing from the page.
// The stdlib has no HTML parser, so a small scanner pulls href, src and id
// values out of tags, skipping comments and the bodies of <script> and
// <style>. No network requests are ever made.
package linkcheck

import (
	"html"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/vetr0s/swag/internal/site"
)

type Broken struct {
	Page string // output path of the page, e.g. "posts/foo.html"
	Link string // the link as written, entity-decoded
}

// Check scans every .html file under dir.
func Check(dir, siteURL string) ([]Broken, error) {
	files, err := site.Walk(dir, true)
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, f := range files {
		exists[f] = true
	}
	// Fragments can point at any page, so every page is scanned before any link is checked.
	var pages []string
	links := map[string][]string{}
	ids := map[string]map[string]bool{}
	for _, f := range files {
		if !strings.HasSuffix(f, ".html") {
			continue
		}
		doc, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		var pageIDs []string
		links[f], pageIDs = Scan(string(doc))
		ids[f] = map[string]bool{}
		for _, id := range pageIDs {
			ids[f][id] = true
		}
		pages = append(pages, f)
	}
	var broken []Broken
	for _, f := range pages {
		for _, link := range links[f] {
			t, frag, ok := Target(f, link, siteURL)
			if !ok {
				continue
			}
			if !exists[t] {
				t = path.Join(t, "index.html")
			}
			if !exists[t] || !anchored(ids[t], t, frag) {
				broken = append(broken, Broken{Page: f, Link: link})
			}
		}
	}
	slices.SortFunc(broken, func(a, b Broken) int {
		return strings.Compare(a.Page+"\x00"+a.Link, b.Page+"\x00"+b.Link)
	})
	return slices.Compact(broken), nil
}

// anchored reports whether frag names an element of page t. Fragments into
// non-HTML files and the special fragment "top" always pass, as in browsers.
func anchored(ids map[string]bool, t, frag string) bool {
	if frag == "" || !strings.HasSuffix(t, ".html") || strings.EqualFold(frag, "top") || ids[frag] {
		return true
	}
	decoded, err := url.PathUnescape(frag)
	return err == nil && ids[decoded]
}

// Scan returns the entity-decoded href and src values in doc, and the
// fragment targets it defines: every id, and name on <a>.
func Scan(doc string) (links, ids []string) {
	i := 0
	for {
		lt := strings.IndexByte(doc[i:], '<')
		if lt < 0 {
			return links, ids
		}
		i += lt + 1
		if strings.HasPrefix(doc[i:], "!--") {
			end := strings.Index(doc[i:], "-->")
			if end < 0 {
				return links, ids
			}
			i += end + 3
			continue
		}
		j := i
		for j < len(doc) && isAlnum(doc[j]) {
			j++
		}
		if j == i {
			continue // a closing tag, a doctype, or a stray '<'
		}
		tag := strings.ToLower(doc[i:j])
		i = j
		for i < len(doc) && doc[i] != '>' {
			if isSpace(doc[i]) || doc[i] == '/' {
				i++
				continue
			}
			j = i
			for j < len(doc) && !isSpace(doc[j]) && !strings.ContainsRune("=>/", rune(doc[j])) {
				j++
			}
			name := strings.ToLower(doc[i:j])
			i = max(j, i+1)
			for i < len(doc) && isSpace(doc[i]) {
				i++
			}
			if i >= len(doc) || doc[i] != '=' {
				continue
			}
			i++
			for i < len(doc) && isSpace(doc[i]) {
				i++
			}
			var val string
			if i < len(doc) && (doc[i] == '"' || doc[i] == '\'') {
				end := strings.IndexByte(doc[i+1:], doc[i])
				if end < 0 {
					return links, ids
				}
				val, i = doc[i+1:i+1+end], i+end+2
			} else {
				j = i
				for j < len(doc) && !isSpace(doc[j]) && doc[j] != '>' {
					j++
				}
				val, i = doc[i:j], j
			}
			switch {
			case name == "href" || name == "src":
				links = append(links, html.UnescapeString(val))
			case name == "id" || name == "name" && tag == "a":
				ids = append(ids, html.UnescapeString(val))
			}
		}
		if tag == "script" || tag == "style" {
			i = closeTag(doc, i, tag)
		}
	}
}

// closeTag returns the index of the </tag that closes raw text starting at i.
func closeTag(doc string, i int, tag string) int {
	for {
		m := strings.Index(doc[i:], "</")
		if m < 0 {
			return len(doc)
		}
		i += m
		// The tag name must end the match, so </scripts does not close <script>.
		end := i + 2 + len(tag)
		if end <= len(doc) && strings.EqualFold(doc[i+2:end], tag) && (end == len(doc) || isSpace(doc[end]) || doc[end] == '/' || doc[end] == '>') {
			return i
		}
		i += 2
	}
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

var schemeRE = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.-]*):`)

// Target resolves link, found on the page at output path page, to the output
// path and fragment it must point at. ok is false for links that are not checked.
func Target(page, link, siteURL string) (target, frag string, ok bool) {
	if link == "" || strings.HasPrefix(link, "//") {
		return "", "", false
	}
	if m := schemeRE.FindStringSubmatch(link); m != nil {
		scheme := strings.ToLower(m[1])
		rest, internal := strings.CutPrefix(link, siteURL)
		if siteURL == "" || !internal || scheme != "http" && scheme != "https" {
			return "", "", false
		}
		// https://example.com.evil shares the prefix but is another host.
		if rest != "" && !strings.ContainsRune("/?#", rune(rest[0])) {
			return "", "", false
		}
		link = "/" + strings.TrimPrefix(rest, "/")
	}
	link, frag, _ = strings.Cut(link, "#")
	if k := strings.IndexByte(link, '?'); k >= 0 {
		link = link[:k]
	}
	if link == "" && frag == "" {
		return "", "", false
	}
	if link == "" {
		return page, frag, true
	}
	if decoded, err := url.PathUnescape(link); err == nil {
		link = decoded
	}
	t := path.Join("/", path.Dir(page), link)
	if strings.HasPrefix(link, "/") {
		t = path.Clean(link)
	}
	if strings.HasSuffix(link, "/") || t == "/" {
		t = path.Join(t, "index.html")
	}
	return strings.TrimPrefix(t, "/"), frag, true
}
