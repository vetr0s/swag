package linkcheck

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLinks(t *testing.T) {
	doc := `<!DOCTYPE html><html><head><link rel=stylesheet href=/s.css></head>
<body><a href="/a.html?x=1&amp;y=2">a</a> <IMG SRC='b.png' alt="x > y"> <a class="x" href = "c d.html">
<!-- <a href="/commented.html"> -->
<code>&lt;a href="/escaped.html"&gt;</code>
<script src="/app.js">var s = '<a href="/in-script.html">';</SCRIPT>
<style>a[href="/in-style.html"] {}</style>
<script>var s = "</scripts>"; var t = '<a href="/after-prefix.html">';</script>
<p data-href="/not-href.html">text with a stray < sign</p><a href=/last.html>
<h2 id="intro">x</h2><a name=old></a><p name="not-an-anchor" data-id="nope">`
	links, ids := Scan(doc)
	want := []string{"/s.css", "/a.html?x=1&y=2", "b.png", "c d.html", "/app.js", "/last.html"}
	if !slices.Equal(links, want) {
		t.Errorf("links = %q\nwant  %q", links, want)
	}
	if want := []string{"intro", "old"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %q, want %q", ids, want)
	}
}

func TestTarget(t *testing.T) {
	const site = "https://example.com"
	tests := []struct {
		page, link, want, frag string
		ok                     bool
	}{
		{"index.html", "/posts/a.html", "posts/a.html", "", true},
		{"posts/a.html", "b.png", "posts/b.png", "", true},
		{"posts/a.html", "../style.css", "style.css", "", true},
		{"posts/a.html", "../../../x.html", "x.html", "", true},
		{"posts/a.html", "/", "index.html", "", true},
		{"posts/a.html", "/notes/", "notes/index.html", "", true},
		{"notes/index.html", "..", "index.html", "", true},
		{"index.html", "/a%20b.html#frag", "a b.html", "frag", true},
		{"index.html", "/a.html?q=1", "a.html", "", true},
		{"index.html", "https://example.com/posts/a.html", "posts/a.html", "", true},
		{"index.html", "https://example.com", "index.html", "", true},
		{"index.html", "HTTPS://example.com/x.html", "", "", false}, // prefix match is exact
		{"index.html", "https://example.community/x.html", "", "", false},
		{"index.html", "https://other.org/x.html", "", "", false},
		{"index.html", "mailto:me@example.com", "", "", false},
		{"index.html", "data:image/png;base64,xx", "", "", false},
		{"index.html", "//cdn.example.com/x.js", "", "", false},
		{"posts/a.html", "#top", "posts/a.html", "top", true},
		{"index.html", "#", "", "", false},
		{"index.html", "?page=2", "", "", false},
		{"index.html", "", "", "", false},
	}
	for _, tt := range tests {
		got, frag, ok := Target(tt.page, tt.link, site)
		if got != tt.want || frag != tt.frag || ok != tt.ok {
			t.Errorf("Target(%s, %q) = %q, %q, %v; want %q, %q, %v", tt.page, tt.link, got, frag, ok, tt.want, tt.frag, tt.ok)
		}
	}
	if _, _, ok := Target("index.html", "https://example.com/x.html", ""); ok {
		t.Error("absolute links are external when site.url is unset")
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"index.html":       `<a href="/posts/">p</a> <a href="/gone.html">x</a> <a href="/gone.html">again</a> <a href="notes">n</a>`,
		"posts/index.html": `<img src="missing.png"> <a href="../index.html">home</a>`,
		"notes/index.html": `<h2 id="a b">x</h2>`,
		"frag.html": `<a href="#here">ok</a> <a href="#gone">x</a> <a href="#TOP">top</a> <p id="here">` +
			`<a href="/notes/#a%20b">ok</a> <a href="notes#a b">ok</a> <a href="index.html#nope">x</a> <a href="/file.pdf#page=2">ok</a>`,
		"file.pdf": ``,
	}
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(body), 0o644)
	}
	got, err := Check(dir, "")
	want := []Broken{{"frag.html", "#gone"}, {"frag.html", "index.html#nope"}, {"index.html", "/gone.html"}, {"posts/index.html", "missing.png"}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("Check = %v, %v; want %v", got, err, want)
	}
}
