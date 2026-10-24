package content

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOutPathAndURL(t *testing.T) {
	tests := []struct{ rel, out, url string }{
		{"index.md", "index.html", "/"},
		{"about.md", "about.html", "/about.html"},
		{"posts/foo.md", "posts/foo.html", "/posts/foo.html"},
		{"notes/index.html", "notes/index.html", "/notes/"},
		{"posts/a.b.MD", "posts/a.b.html", "/posts/a.b.html"},
		{"myindex.md", "myindex.html", "/myindex.html"},
	}
	for _, tt := range tests {
		out := OutPath(tt.rel)
		if out != tt.out || URL(out) != tt.url {
			t.Errorf("%s -> %s %s, want %s %s", tt.rel, out, URL(out), tt.out, tt.url)
		}
	}
}

func TestClassify(t *testing.T) {
	render := map[string][]string{"md": {"pandoc"}, "org": {}}
	tests := []struct {
		rel       string
		isContent bool
		errs      bool
	}{
		{"a.html", true, false}, {"a.HTML", true, false}, {"a.md", true, false}, {"a.Md", true, false},
		{"a.png", false, false}, {"README", false, false}, {"a.org", false, true},
	}
	for _, tt := range tests {
		got, msg := Classify(tt.rel, render)
		if got != tt.isContent || (msg != "") != tt.errs {
			t.Errorf("Classify(%s) = %v %q", tt.rel, got, msg)
		}
	}
}

func TestLoad(t *testing.T) {
	p, ds := Load("/s/content/posts/x.md", "posts/x.md", []byte("---\ntitle: X\ndate: 2026-02-03\nupdated: 2026-03-04   Fixed a typo.\nupdated: 2026-05-06\nupdated: 2026-04-05 Added a section.\nseries: go\n---\nbody"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if !p.IsPost || p.Layout != "post" || p.Source != "content/posts/x.md" || p.URL != "/posts/x.html" ||
		!p.Date.Equal(time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)) || !slices.Equal(p.Updates, []Update{{day(2026, 5, 6), ""}, {day(2026, 4, 5), "Added a section."}, {day(2026, 3, 4), "Fixed a typo."}}) ||
		!p.Updated.Equal(day(2026, 5, 6)) ||
		p.Params["series"] != "go" || p.Params["title"] != "X" {
		t.Errorf("Load: %+v", p)
	}
	layouts := map[string]string{"index.md": "index", "about.md": "default", "notes/index.md": "default"}
	for rel, want := range layouts {
		p, _ := Load("/x", rel, []byte("---\ntitle: T\n---\n"))
		if p.Layout != want {
			t.Errorf("layout of %s = %s, want %s", rel, p.Layout, want)
		}
	}
	p, _ = Load("/x", "about.md", []byte("---\ntitle: T\nlayout: wide\ndraft: true\n---\n"))
	if p.Layout != "wide" || !p.IsDraft || !p.Date.IsZero() {
		t.Errorf("explicit layout/draft: %+v", p)
	}

	errs := []struct{ rel, src, want string }{
		{"a.md", "---\n---\n", "missing title"},
		{"a.md", "---\ntitle:\n---\n", "missing title"},
		{"posts/a.md", "---\ntitle: T\n---\n", "missing date"},
		{"a.md", "---\ntitle: T\ndate: 2026-1-2\n---\n", "invalid date"},
		{"a.md", "---\ntitle: T\ndate: 2026-01-02T10:00\n---\n", "invalid date"},
		{"a.md", "---\ntitle: T\ndraft: yes\n---\n", "invalid draft"},
		{"a.md", "---\ntitle: T\nupdated: soon\n---\n", "invalid updated date"},
		{"a.md", "---\ntitle: T\nupdated: 2026/01/01 Fixed.\n---\n", "invalid updated date"},
		{"a.md", "---\ntitle: T\ndate: 2026-02-03\nupdated: 2026-02-02\n---\n", "is before date"},
	}
	for _, tt := range errs {
		p, ds := Load("/x", tt.rel, []byte(tt.src))
		if p != nil || len(ds) != 1 || !strings.Contains(ds[0].Msg, tt.want) {
			t.Errorf("Load(%q) = %v, %v; want %s", tt.src, p, ds, tt.want)
		}
	}
}

func TestSortPosts(t *testing.T) {
	d := func(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }
	posts := []*Page{
		{Source: "content/posts/b.md", Date: d("2026-01-01")},
		{Source: "content/posts/c.md", Date: d("2026-03-01")},
		{Source: "content/posts/a.md", Date: d("2026-01-01")},
	}
	SortPosts(posts)
	var got []string
	for _, p := range posts {
		got = append(got, p.Source)
	}
	if want := "content/posts/c.md content/posts/a.md content/posts/b.md"; strings.Join(got, " ") != want {
		t.Errorf("order = %v", got)
	}
}

func day(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }
