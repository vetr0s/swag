// Package build runs the build pipeline. Generate turns a site into a
// complete output tree in a given directory. Build wraps it for `swag build`.
// It generates into a temporary directory inside the site root, then swaps
// that directory into place only when every step passed, so a failed build
// never touches the existing output.
package build

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vetr0s/swag/internal/config"
	"github.com/vetr0s/swag/internal/content"
	"github.com/vetr0s/swag/internal/diag"
	"github.com/vetr0s/swag/internal/feed"
	"github.com/vetr0s/swag/internal/layout"
	"github.com/vetr0s/swag/internal/linkcheck"
	"github.com/vetr0s/swag/internal/render"
	"github.com/vetr0s/swag/internal/site"
)

type Options struct {
	Root         string
	Config       *config.Config
	Out          string // absolute output directory; also names files in diagnostics
	Drafts       bool
	Log          io.Writer     // receives the written-file list and renderer stderr; nil for none
	Cache        *render.Cache // nil disables render caching
	LinkWarnings bool          // report broken links as warnings instead of errors
}

type Stats struct {
	Pages int // non-post HTML pages, including a generated home page
	Posts int
	Files int // assets, static files and rss.xml
}

func (s Stats) String() string {
	return fmt.Sprintf("%d pages, %d posts, %d files", s.Pages, s.Posts, s.Files)
}

// Build generates the site and replaces o.Out with the result. The old output
// is renamed aside first, so a failed swap can put it back.
func Build(ctx context.Context, o Options) (Stats, []diag.Diagnostic) {
	tmp, err := os.MkdirTemp(o.Root, ".swag-build-*")
	if err != nil {
		return Stats{}, []diag.Diagnostic{diag.Errorf("", 0, "%v", err)}
	}
	defer os.RemoveAll(tmp)
	stats, ds := Generate(ctx, tmp, o)
	if diag.HasErrors(ds) {
		return stats, ds
	}
	fail := func(err error) (Stats, []diag.Diagnostic) {
		return stats, append(ds, diag.Errorf(o.Out, 0, "replacing output directory: %v", err))
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(o.Out), 0o755); err != nil {
		return fail(err)
	}
	old := tmp + ".old"
	if err := os.Rename(o.Out, old); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if err := os.Rename(tmp, o.Out); err != nil {
		os.Rename(old, o.Out)
		return fail(err)
	}
	if err := os.RemoveAll(old); err != nil {
		ds = append(ds, diag.Warnf(old, 0, "removing the previous output: %v", err))
	}
	return stats, ds
}

// Generate writes the whole site into dir, which must be an empty directory.
func Generate(ctx context.Context, dir string, o Options) (Stats, []diag.Diagnostic) {
	cfg := o.Config
	pages, assets, ds := content.Discover(o.Root, cfg.Render, o.Drafts)
	staticDir := filepath.Join(o.Root, "static")
	statics, err := site.Walk(staticDir, true)
	if err != nil {
		ds = append(ds, diag.Errorf(staticDir, 0, "%v", err))
	}
	layouts, lds := layout.Load(o.Root, cfg.URL)
	ds = append(ds, lds...)
	if diag.HasErrors(ds) {
		return Stats{}, ds
	}

	var stats Stats
	var posts []*content.Page
	var home *content.Page
	for _, p := range pages {
		if !layouts.Has(p.Layout) {
			ds = append(ds, diag.Errorf(o.abs(p.Source), 0, "layout %q not found in layouts/ or the embedded defaults", p.Layout))
		}
		if p.IsPost {
			posts = append(posts, p)
		} else {
			stats.Pages++
		}
		if p.Out == "index.html" {
			home = p
		}
	}
	stats.Posts = len(posts)
	content.SortPosts(posts)
	if home == nil {
		home = &content.Page{Title: cfg.Title, URL: "/", Layout: "index", Out: "index.html", Params: map[string]string{}}
		pages = append(pages, home)
		stats.Pages++
	}
	hasFeed := cfg.URL != "" && len(posts) > 0
	if cfg.URL == "" && len(posts) > 0 {
		ds = append(ds, diag.Warnf(o.abs("swag.conf"), 0, "site.url is not set, so rss.xml is not generated"))
	}

	// Map every output path to the file that produces it. Collisions compare
	// case-folded paths, since a case-insensitive volume holds only one of them.
	owner := map[string]string{}
	folded := map[string]string{}
	claim := func(out, src string) {
		if prev, ok := folded[strings.ToLower(out)]; ok {
			by := o.rel(prev) + " also writes"
			if prev == o.outFile(out) {
				by = "swag also generates"
			}
			ds = append(ds, diag.Errorf(src, 0, "writes %s, which %s", o.display(out), by))
			return
		}
		owner[out], folded[strings.ToLower(out)] = src, src
	}
	for _, p := range pages {
		claim(p.Out, o.source(p))
	}
	if hasFeed {
		claim("rss.xml", o.outFile("rss.xml"))
	}
	for _, a := range assets {
		claim(a, filepath.Join(o.Root, "content", filepath.FromSlash(a)))
	}
	for _, s := range statics {
		claim(s, filepath.Join(staticDir, filepath.FromSlash(s)))
	}
	if diag.HasErrors(ds) {
		return stats, ds
	}

	var jobs []render.Job
	var rendered []*content.Page
	for _, p := range pages {
		if p.Source == "" {
			continue
		}
		if p.Ext == "html" {
			p.Content = trimmedHTML(p.Body)
			continue
		}
		jobs = append(jobs, render.Job{Source: p.Source, Argv: cfg.Render[p.Ext], Body: p.Body})
		rendered = append(rendered, p)
	}
	results := render.Run(ctx, jobs, render.Options{Root: o.Root, Drafts: o.Drafts, Timeout: cfg.Timeout, Cache: o.Cache})
	if ctx.Err() != nil {
		return stats, append(ds, diag.Errorf("", 0, "build interrupted"))
	}
	for i, r := range results {
		rendered[i].Content = trimmedHTML(r.HTML)
		ds = append(ds, r.Diags...)
		if len(r.Stderr) > 0 && o.Log != nil {
			fmt.Fprintf(o.Log, "%s: renderer stderr:\n%s", rendered[i].Source, r.Stderr)
		}
	}
	if diag.HasErrors(ds) {
		return stats, ds
	}

	s := &layout.Site{
		Title:       cfg.Title,
		URL:         cfg.URL,
		Author:      cfg.Author,
		Description: cfg.Description,
		Language:    cfg.Language,
		Params:      cfg.Params,
		Posts:       posts,
		HasFeed:     hasFeed,
	}
	write := func(out string, data []byte) {
		path := filepath.Join(dir, filepath.FromSlash(out))
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err == nil {
			err = os.WriteFile(path, data, 0o644)
		}
		if err != nil {
			ds = append(ds, diag.Errorf(o.outFile(out), 0, "%v", err))
		} else if o.Log != nil {
			fmt.Fprintf(o.Log, "wrote %s\n", o.display(out))
		}
	}
	for _, p := range pages {
		var buf bytes.Buffer
		if lds := layouts.Execute(&buf, p.Layout, layout.TemplateData{Site: s, Page: p}); lds != nil {
			for _, d := range lds {
				d.Msg += " (building " + o.rel(o.source(p)) + ")"
				ds = append(ds, d)
			}
			continue
		}
		write(p.Out, append(bytes.TrimRight(buf.Bytes(), "\n"), '\n'))
	}
	if hasFeed {
		write("rss.xml", feed.RSS(cfg.Title, cfg.URL, cfg.Description, cfg.Language, posts))
		stats.Files++
	}
	for _, out := range append(assets, statics...) {
		data, err := os.ReadFile(owner[out])
		if err != nil {
			ds = append(ds, diag.Errorf(owner[out], 0, "%v", err))
			continue
		}
		write(out, data)
		stats.Files++
	}
	if diag.HasErrors(ds) {
		return stats, ds
	}

	broken, err := linkcheck.Check(dir, cfg.URL)
	if err != nil {
		return stats, append(ds, diag.Errorf("", 0, "link check: %v", err))
	}
	for _, b := range broken {
		path := o.outFile(b.Page)
		if src := owner[b.Page]; src != "" {
			path = src
		}
		ds = append(ds, diag.Diagnostic{Path: path, Warning: o.LinkWarnings, Msg: fmt.Sprintf("broken link %q", b.Link)})
	}
	return stats, ds
}

func trimmedHTML(b []byte) template.HTML {
	return template.HTML(bytes.TrimRight(b, "\n"))
}

// source is the absolute source path of p, or its output path when generated.
func (o Options) source(p *content.Page) string {
	if p.Source == "" {
		return o.outFile(p.Out)
	}
	return o.abs(p.Source)
}

// outFile is the absolute path an output file has in the final output directory.
func (o Options) outFile(out string) string {
	return filepath.Join(o.Out, filepath.FromSlash(out))
}

// display names an output path as it appears under the output directory.
func (o Options) display(out string) string {
	return filepath.Join(o.rel(o.Out), filepath.FromSlash(out))
}

func (o Options) abs(rootRel string) string {
	return filepath.Join(o.Root, filepath.FromSlash(rootRel))
}

func (o Options) rel(abs string) string {
	return diag.Rel(o.Root, abs)
}
