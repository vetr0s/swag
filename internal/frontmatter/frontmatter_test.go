package frontmatter

import (
	"strconv"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		fields   map[string]string
		body     string
		bodyLine int
		errs     []string // "line: message substring"
	}{
		{name: "basic", src: "---\ntitle: Hi\ndate: 2026-01-02\n---\n\nBody\n", fields: map[string]string{"title": "Hi", "date": "2026-01-02"}, body: "Body\n", bodyLine: 6},
		{name: "first colon splits", src: "---\ntitle: Go: a tour\n---\nx", fields: map[string]string{"title": "Go: a tour"}, body: "x", bodyLine: 4},
		{name: "bom crlf and trailing space", src: "\xef\xbb\xbf--- \r\ntitle:  T  \r\n---\r\n\r\nB\r\n", fields: map[string]string{"title": "T"}, body: "B\r\n", bodyLine: 5},
		{name: "comments and blanks", src: "---\n# note\n\ntitle: T\n---\n", fields: map[string]string{"title": "T"}, body: "", bodyLine: 6},
		{name: "only one blank line dropped", src: "---\ntitle: T\n---\n\n\nB", fields: map[string]string{"title": "T"}, body: "\nB", bodyLine: 5},
		{name: "missing", src: "title: T\n", errs: []string{"1: missing front matter"}},
		{name: "empty file", src: "", errs: []string{"1: missing front matter"}},
		{name: "unterminated", src: "---\ntitle: T\n", errs: []string{"1: unterminated"}},
		{name: "no colon", src: "---\ntitle: T\nnope\n---\n", errs: []string{"3: expected key: value"}},
		{name: "bad key", src: "---\nTitle: T\n---\n", errs: []string{"2: invalid key"}},
		{name: "duplicate", src: "---\ntitle: A\ntitle: B\n---\n", errs: []string{"3: duplicate key"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, ds := Parse("f.md", []byte(tt.src))
			if len(ds) != len(tt.errs) {
				t.Fatalf("got %d diagnostics %v, want %v", len(ds), ds, tt.errs)
			}
			for i, want := range tt.errs {
				line, msg, _ := strings.Cut(want, ": ")
				if got := ds[i]; strconv.Itoa(got.Line) != line || !strings.Contains(got.Msg, msg) {
					t.Errorf("diagnostic %d = %d: %s, want %s", i, got.Line, got.Msg, want)
				}
			}
			if tt.errs != nil {
				return
			}
			for k, v := range tt.fields {
				if fm.Fields[k] != v {
					t.Errorf("field %s = %q, want %q", k, fm.Fields[k], v)
				}
			}
			if len(fm.Fields) != len(tt.fields) || string(fm.Body) != tt.body || fm.BodyLine != tt.bodyLine {
				t.Errorf("got fields %v body %q line %d, want %v %q %d", fm.Fields, fm.Body, fm.BodyLine, tt.fields, tt.body, tt.bodyLine)
			}
		})
	}
}
