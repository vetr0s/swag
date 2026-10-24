package config

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	c, ds := Parse("swag.conf", nil, "mysite")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if c.Title != "mysite" || c.Language != "en" || c.Out != "docs" || c.Port != 8080 || c.Timeout != 30*time.Second {
		t.Errorf("defaults: %+v", c)
	}
	if !slices.Equal(c.Render["md"], []string{"pandoc", "--from=gfm", "--to=html5"}) {
		t.Errorf("default render.md = %q", c.Render["md"])
	}
}

func TestParse(t *testing.T) {
	src := `# comment
site.title = A = B
site.url = https://example.com/
  build.out=public
serve.port = 9000
render.timeout = 5s
render.md =
render.org = scripts/org "two words"
params.stylesheet = /style.css
`
	c, ds := Parse("swag.conf", []byte(src), "x")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if c.Title != "A = B" || c.URL != "https://example.com" || c.Out != "public" || c.Port != 9000 || c.Timeout != 5*time.Second {
		t.Errorf("parsed: %+v", c)
	}
	if argv, ok := c.Render["md"]; !ok || len(argv) != 0 {
		t.Errorf("empty render.md should disable md, got %q %v", argv, ok)
	}
	if !slices.Equal(c.Render["org"], []string{"scripts/org", "two words"}) {
		t.Errorf("render.org = %q", c.Render["org"])
	}
	if c.Params["stylesheet"] != "/style.css" {
		t.Errorf("params = %v", c.Params)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{"site.title", "1: expected key = value"},
		{"Site.Title = x", "1: invalid key"},
		{"site.title = a\nsite.title = b", "2: duplicate key"},
		{"site.titel = x", "1: unknown key \"site.titel\""},
		{"site.url = example.com", "1: site.url must start with"},
		{"serve.port = 0", "1: serve.port"},
		{"serve.port = http", "1: serve.port"},
		{"render.timeout = 5", "1: render.timeout"},
		{"render.html = cat", "1: render.html cannot be set"},
		{"render.md = pandoc \"open", "1: render.md: unterminated"},
		{"build.out =", "1: build.out must not be empty"},
		{"params. = x", "1: unknown key"},
	}
	for _, tt := range tests {
		_, ds := Parse("swag.conf", []byte(tt.src), "x")
		line, msg, _ := strings.Cut(tt.want, ": ")
		if len(ds) != 1 || ds[0].Path != "swag.conf" || strconv.Itoa(ds[0].Line) != line || !strings.Contains(ds[0].Msg, msg) {
			t.Errorf("Parse(%q) = %v, want %s", tt.src, ds, tt.want)
		}
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"pandoc --from=gfm   --to=html5", []string{"pandoc", "--from=gfm", "--to=html5"}},
		{`a "b c" d`, []string{"a", "b c", "d"}},
		{`"say \"hi\""`, []string{`say "hi"`}},
		{`x""`, []string{"x"}},
		{`""`, []string{""}},
		{`a\b`, []string{`a\b`}},
		{"a\tb", []string{"a", "b"}},
	}
	for _, tt := range tests {
		got, err := Tokenize(tt.in)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("Tokenize(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	if _, err := Tokenize(`a "b`); err == nil {
		t.Error("unterminated quote: want error")
	}
}
