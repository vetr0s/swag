// Package content discovers the files under content/, splits them into
// content files and assets, and turns content files into Pages with their
// output paths and URLs.
package content

import (
	"cmp"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/vetr0s/swag/internal/diag"
	"github.com/vetr0s/swag/internal/frontmatter"
	"github.com/vetr0s/swag/internal/site"
)

type Page struct {
	Title       string
	Date        time.Time // zero when unset
	Updated     time.Time // newest entry of Updates; zero when there are none
	Updates     []Update  // newest first
	Description string
	URL         string // root-relative, e.g. "/posts/foo.html"
	Source      string // e.g. "content/posts/foo.md"
	Content     template.HTML
	IsPost      bool
	IsDraft     bool
	Layout      string
	Params      map[string]string

	Out  string // output path relative to the output root, e.g. "posts/foo.html"
	Ext  string // lowercase source extension without the dot
	Body []byte // source body after the front matter, before rendering
}

// Update is one `updated: YYYY-MM-DD [HH:MM] [note]` line of front matter.
type Update struct {
	Date    time.Time // wall-clock time, stored as UTC like every swag date
	Note    string    // what the edit changed; "" when unset
	HasTime bool      // the line gave HH:MM; otherwise Date is midnight
}

var clockRE = regexp.MustCompile(`^\d\d:\d\d$`)

// Discover walks root/content and returns its pages and its asset paths
// (relative to content/). Drafts are dropped unless drafts is set.
func Discover(root string, render map[string][]string, drafts bool) ([]*Page, []string, []diag.Diagnostic) {
	dir := filepath.Join(root, "content")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, nil, []diag.Diagnostic{diag.Errorf(dir, 0, "content directory not found")}
	}
	files, err := site.Walk(dir, false)
	if err != nil {
		return nil, nil, []diag.Diagnostic{diag.Errorf(dir, 0, "%v", err)}
	}
	var pages []*Page
	var assets []string
	var ds []diag.Diagnostic
	for _, rel := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		isContent, msg := Classify(rel, render)
		if msg != "" {
			ds = append(ds, diag.Errorf(abs, 0, "%s", msg))
			continue
		}
		if !isContent {
			assets = append(assets, rel)
			continue
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			ds = append(ds, diag.Errorf(abs, 0, "%v", err))
			continue
		}
		p, pds := Load(abs, rel, src)
		ds = append(ds, pds...)
		if p != nil && (drafts || !p.IsDraft) {
			pages = append(pages, p)
		}
	}
	return pages, assets, ds
}

// Classify reports whether the content/ file rel is a content file. msg is
// non-empty when the file's extension has been explicitly disabled.
func Classify(rel string, render map[string][]string) (isContent bool, msg string) {
	ext := Ext(rel)
	if ext == "html" {
		return true, ""
	}
	argv, ok := render[ext]
	if ok && len(argv) == 0 {
		return false, "no renderer for ." + ext + " files: render." + ext + " is empty in swag.conf"
	}
	return ok, ""
}

func Ext(rel string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(rel), "."))
}

// OutPath maps a content-relative source path to its output path.
func OutPath(rel string) string {
	return strings.TrimSuffix(rel, path.Ext(rel)) + ".html"
}

// URL maps an output path to its root-relative URL. index.html outputs get
// directory-style URLs.
func URL(out string) string {
	if out == "index.html" || strings.HasSuffix(out, "/index.html") {
		return "/" + strings.TrimSuffix(out, "index.html")
	}
	return "/" + out
}

// Load builds a Page from the source at content-relative path rel. abs is
// used for diagnostics only.
func Load(abs, rel string, src []byte) (*Page, []diag.Diagnostic) {
	fm, ds := frontmatter.Parse(abs, src)
	if fm == nil {
		return nil, ds
	}
	f := fm.Fields
	p := &Page{
		Title:       f["title"],
		Description: f["description"],
		Source:      "content/" + rel,
		IsPost:      strings.HasPrefix(rel, "posts/"),
		Params:      f,
		Out:         OutPath(rel),
		Ext:         Ext(rel),
		Body:        fm.Body,
	}
	p.URL = URL(p.Out)
	if p.Title == "" {
		ds = append(ds, diag.Errorf(abs, cmp.Or(fm.Lines["title"], 1), "missing title"))
	}
	if d, ok := f["date"]; ok {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			ds = append(ds, diag.Errorf(abs, fm.Lines["date"], "invalid date %q: use YYYY-MM-DD", d))
		}
		p.Date = t
	} else if p.IsPost {
		ds = append(ds, diag.Errorf(abs, 1, "missing date: posts need date: YYYY-MM-DD"))
	}
	for _, v := range fm.Repeated["updated"] {
		d, note, _ := strings.Cut(v.Text, " ")
		note = strings.TrimSpace(note)
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			ds = append(ds, diag.Errorf(abs, v.Line, "invalid updated date %q: use updated: YYYY-MM-DD, an optional HH:MM and an optional note", d))
		}
		u := Update{Date: t, Note: note}
		if clock, rest, _ := strings.Cut(note, " "); clockRE.MatchString(clock) && err == nil {
			at, cerr := time.Parse("2006-01-02 15:04", d+" "+clock)
			if cerr != nil {
				ds = append(ds, diag.Errorf(abs, v.Line, "invalid updated time %q: use HH:MM, 00:00 to 23:59", clock))
			}
			u = Update{Date: at, Note: strings.TrimSpace(rest), HasTime: true}
		}
		if err == nil && t.Before(p.Date) {
			ds = append(ds, diag.Errorf(abs, v.Line, "updated date %s is before date %s", d, f["date"]))
		}
		p.Updates = append(p.Updates, u)
	}
	slices.SortStableFunc(p.Updates, func(a, b Update) int { return b.Date.Compare(a.Date) })
	if len(p.Updates) > 0 {
		p.Updated = p.Updates[0].Date
	}
	switch f["draft"] {
	case "", "false":
	case "true":
		p.IsDraft = true
	default:
		ds = append(ds, diag.Errorf(abs, fm.Lines["draft"], "invalid draft %q: use true or false", f["draft"]))
	}
	switch {
	case f["layout"] != "":
		p.Layout = f["layout"]
	case p.IsPost:
		p.Layout = "post"
	case p.Out == "index.html":
		p.Layout = "index"
	default:
		p.Layout = "default"
	}
	if diag.HasErrors(ds) {
		return nil, ds
	}
	return p, ds
}

// SortPosts orders posts newest first, ties broken by source path.
func SortPosts(posts []*Page) {
	slices.SortFunc(posts, func(a, b *Page) int {
		return cmp.Or(b.Date.Compare(a.Date), cmp.Compare(a.Source, b.Source))
	})
}
