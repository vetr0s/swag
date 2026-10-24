// Package layout loads the layouts and partials a page is wrapped in.
//
// The binary embeds a default set under defaults/. A file of the same name
// in the site's layouts/ directory replaces the embedded one; new files add
// to it. Every partial is parsed into one base template, and each layout is
// parsed into its own clone of that base, once per build.
package layout

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vetr0s/swag/internal/content"
	"github.com/vetr0s/swag/internal/diag"
	"github.com/vetr0s/swag/internal/site"
)

//go:embed defaults
var defaults embed.FS

type Site struct {
	Title       string
	URL         string // "" when unset
	Author      string
	Description string
	Language    string
	Params      map[string]string
	Posts       []*content.Page // newest first
	HasFeed     bool
}

type TemplateData struct {
	Site *Site
	Page *content.Page
}

type Set struct {
	layouts map[string]*template.Template
	files   map[string]file // template name -> source, for locating errors
}

type file struct {
	text    string
	display string // site path, or "embedded <name>"
	abs     string // "" when embedded
}

// Load parses every layout and partial. siteURL feeds the absURL function.
func Load(root, siteURL string) (*Set, []diag.Diagnostic) {
	files := map[string]file{}
	err := fs.WalkDir(defaults, "defaults", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := defaults.ReadFile(p)
		rel := strings.TrimPrefix(p, "defaults/")
		files[rel] = file{text: string(data), display: "embedded layout " + rel}
		return err
	})
	if err != nil {
		panic(err) // the embedded tree is fixed at compile time
	}
	dir := filepath.Join(root, "layouts")
	rels, err := site.Walk(dir, false)
	if err != nil {
		return nil, []diag.Diagnostic{diag.Errorf(dir, 0, "%v", err)}
	}
	for _, rel := range rels {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, []diag.Diagnostic{diag.Errorf(abs, 0, "%v", err)}
		}
		files[rel] = file{text: string(data), display: "layouts/" + rel, abs: abs}
	}

	funcs := template.FuncMap{
		"date": func(layout string, t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(layout)
		},
		"absURL": func(u string) string {
			if siteURL == "" {
				return u
			}
			return siteURL + "/" + strings.TrimPrefix(u, "/")
		},
	}
	var ds []diag.Diagnostic
	s := &Set{layouts: map[string]*template.Template{}, files: map[string]file{}}
	base := template.New("").Funcs(funcs)
	var layoutNames []string
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		f := files[rel]
		name, ok := strings.CutSuffix(rel, ".html")
		if !ok || strings.Count(name, "/") > 1 {
			continue
		}
		if partial, ok := strings.CutPrefix(name, "partials/"); ok {
			s.files[partial] = f
			// Drop the file's final newline so the include line supplies it.
			if _, err := base.New(partial).Parse(strings.TrimSuffix(f.text, "\n")); err != nil {
				ds = append(ds, s.locate(err))
			}
		} else if !strings.Contains(name, "/") {
			layoutNames = append(layoutNames, name)
		}
	}
	for _, name := range layoutNames {
		f := files[name+".html"]
		s.files[name] = f
		t := template.Must(base.Clone()).New(name)
		if _, err := t.Parse(f.text); err != nil {
			ds = append(ds, s.locate(err))
			continue
		}
		s.layouts[name] = t
	}
	diag.Sort(ds)
	return s, ds
}

func (s *Set) Has(name string) bool {
	_, ok := s.layouts[name]
	return ok
}

// Execute runs the named layout. A failure is located in the layout or
// partial that caused it.
func (s *Set) Execute(w io.Writer, name string, data TemplateData) []diag.Diagnostic {
	if err := s.layouts[name].Execute(w, data); err != nil {
		return []diag.Diagnostic{s.locate(err)}
	}
	return nil
}

var templateErrRE = regexp.MustCompile(`(?s)^(?:html/)?template: ?([^:]+):(\d+):(?:(\d+):)? (.*)$`)

// locate turns a template error such as "template: post:3:7: msg" or
// "html/template:post:3:7: msg" into a diagnostic at that file position.
func (s *Set) locate(err error) diag.Diagnostic {
	m := templateErrRE.FindStringSubmatch(err.Error())
	if m == nil {
		return diag.Diagnostic{Msg: err.Error()}
	}
	f, ok := s.files[m[1]]
	if !ok {
		return diag.Diagnostic{Msg: err.Error()}
	}
	if f.abs == "" {
		return diag.Diagnostic{Msg: fmt.Sprintf("%s:%s: %s", f.display, m[2], m[4])}
	}
	line, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	return diag.Diagnostic{Path: f.abs, Line: line, Col: col, Msg: m[4]}
}
