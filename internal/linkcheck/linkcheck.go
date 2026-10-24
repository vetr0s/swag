// Package linkcheck finds internal links in generated HTML that point at
// files missing from the output tree. The stdlib has no HTML parser, so a
// small scanner pulls href and src values out of tags, skipping comments and
// the bodies of <script> and <style>. No network requests are ever made.
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
	var broken []Broken
	for _, f := range files {
		if !strings.HasSuffix(f, ".html") {
			continue
		}
		doc, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		for _, link := range Links(string(doc)) {
			t, ok := Target(f, link, siteURL)
			if ok && !exists[t] && !exists[path.Join(t, "index.html")] {
				broken = append(broken, Broken{Page: f, Link: link})
			}
		}
	}
	slices.SortFunc(broken, func(a, b Broken) int {
		return strings.Compare(a.Page+"\x00"+a.Link, b.Page+"\x00"+b.Link)
	})
	return slices.Compact(broken), nil
}

// Links returns the entity-decoded href and src attribute values in doc.
func Links(doc string) []string {
	var links []string
	i := 0
	for {
		lt := strings.IndexByte(doc[i:], '<')
		if lt < 0 {
			return links
		}
		i += lt + 1
		if strings.HasPrefix(doc[i:], "!--") {
			end := strings.Index(doc[i:], "-->")
			if end < 0 {
				return links
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
					return links
				}
				val, i = doc[i+1:i+1+end], i+end+2
			} else {
				j = i
				for j < len(doc) && !isSpace(doc[j]) && doc[j] != '>' {
					j++
				}
				val, i = doc[i:j], j
			}
			if name == "href" || name == "src" {
				links = append(links, html.UnescapeString(val))
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
		if end := i + 2 + len(tag); end <= len(doc) && strings.EqualFold(doc[i+2:end], tag) {
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
// path it must point at. ok is false for links that are not checked.
func Target(page, link, siteURL string) (target string, ok bool) {
	if link == "" || strings.HasPrefix(link, "#") || strings.HasPrefix(link, "//") {
		return "", false
	}
	if m := schemeRE.FindStringSubmatch(link); m != nil {
		rest, internal := strings.CutPrefix(link, siteURL)
		scheme := strings.ToLower(m[1])
		if siteURL == "" || !internal || scheme != "http" && scheme != "https" || rest != "" && !strings.ContainsRune("/?#", rune(rest[0])) {
			return "", false
		}
		link = "/" + strings.TrimPrefix(rest, "/")
	}
	if k := strings.IndexAny(link, "?#"); k >= 0 {
		link = link[:k]
	}
	if link == "" {
		return "", false
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
	return strings.TrimPrefix(t, "/"), true
}
