package layout

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vetr0s/swag/internal/content"
)

func TestPartialRedefinitionIsDeterministic(t *testing.T) {
	root := t.TempDir()
	partials := filepath.Join(root, "layouts", "partials")
	os.MkdirAll(partials, 0o755)
	os.WriteFile(filepath.Join(partials, "a.html"), []byte(`{{define "x"}}A{{end}}`), 0o644)
	os.WriteFile(filepath.Join(partials, "b.html"), []byte(`{{define "x"}}B{{end}}`), 0o644)
	os.WriteFile(filepath.Join(root, "layouts", "note.html"), []byte(`{{template "x"}}`), 0o644)
	for range 20 {
		s, ds := Load(root, "")
		if ds != nil {
			t.Fatal(ds)
		}
		var buf bytes.Buffer
		if ds := s.Execute(&buf, "note", TemplateData{Site: &Site{}, Page: &content.Page{}}); ds != nil {
			t.Fatal(ds)
		}
		if buf.String() != "B" {
			t.Fatalf("partial x = %q, want the last file in name order", buf.String())
		}
	}
}
