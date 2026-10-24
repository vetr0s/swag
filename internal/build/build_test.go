package build

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vetr0s/swag/internal/config"
	"github.com/vetr0s/swag/internal/diag"
)

var update = flag.Bool("update", false, "rewrite the expected/ trees of the fixture sites")

// TestHelperProcess is the fake renderer: run as a child with "--" in its
// arguments, it wraps each blank-line-separated paragraph of stdin in <p>.
func TestHelperProcess(t *testing.T) {
	if !slices.Contains(os.Args, "--") {
		return
	}
	body, _ := io.ReadAll(os.Stdin)
	if bytes.Contains(body, []byte("RENDER_FAIL")) {
		fmt.Fprint(os.Stderr, "fake: cannot render\nsecond line\n")
		os.Exit(64)
	}
	for _, para := range strings.Split(strings.TrimSpace(string(body)), "\n\n") {
		if para != "" {
			fmt.Printf("<p>%s</p>\n", strings.TrimSpace(para))
		}
	}
	os.Exit(0)
}

func fixture(name string) string {
	root, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "sites", name))
	return root
}

// copyFixture copies a fixture site, minus its expected/ tree, into a temp dir.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name) // the directory name is the default site.title
	if err := os.CopyFS(root, os.DirFS(fixture(name))); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(root, "expected"))
	return root
}

func options(t *testing.T, root string) Options {
	t.Helper()
	cfg, ds := config.Load(root)
	if diag.HasErrors(ds) {
		t.Fatal(ds)
	}
	cfg.Render["md"] = []string{os.Args[0], "-test.run=^TestHelperProcess$", "--"}
	return Options{Root: root, Config: cfg, Out: filepath.Join(root, cfg.Out)}
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		tree[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func diffTrees(t *testing.T, want, got map[string]string) {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if g, ok := got[name]; !ok {
			t.Errorf("missing %s", name)
		} else if g != want[name] {
			t.Errorf("%s differs:\n--- want\n%s\n--- got\n%s", name, want[name], g)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected %s", name)
		}
	}
}

func formatted(root string, ds []diag.Diagnostic) []string {
	diag.Sort(ds)
	var out []string
	for _, d := range ds {
		out = append(out, diag.Format(d, root))
	}
	return out
}

func TestGolden(t *testing.T) {
	tests := map[string][]string{
		"basic":    nil,
		"htmlonly": {"swag.conf: warning: site.url is not set, so rss.xml is not generated"},
	}
	for name, wantDiags := range tests {
		t.Run(name, func(t *testing.T) {
			root := fixture(name)
			o := options(t, root)
			dir := t.TempDir()
			_, ds := Generate(context.Background(), dir, o)
			if got := formatted(root, ds); !slices.Equal(got, wantDiags) {
				t.Fatalf("diagnostics = %q, want %q", got, wantDiags)
			}
			got := readTree(t, dir)
			expected := filepath.Join(root, "expected")
			if *update {
				os.RemoveAll(expected)
				if err := os.CopyFS(expected, os.DirFS(dir)); err != nil {
					t.Fatal(err)
				}
			}
			diffTrees(t, readTree(t, expected), got)

			again := t.TempDir()
			Generate(context.Background(), again, o)
			diffTrees(t, got, readTree(t, again))
		})
	}
}

func TestBuildReplacesOutput(t *testing.T) {
	root := copyFixture(t, "htmlonly")
	o := options(t, root)
	os.MkdirAll(filepath.Join(o.Out, "stale"), 0o755)
	os.WriteFile(filepath.Join(o.Out, "stale", "old.html"), nil, 0o644)
	if _, ds := Build(context.Background(), o); diag.HasErrors(ds) {
		t.Fatal(ds)
	}
	if _, err := os.Stat(filepath.Join(o.Out, "stale")); !os.IsNotExist(err) {
		t.Error("stale output survived the rebuild")
	}
	if info, err := os.Stat(o.Out); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("output dir mode = %v, %v", info.Mode(), err)
	}
	diffTrees(t, readTree(t, filepath.Join(fixture("htmlonly"), "expected")), readTree(t, o.Out))
}

func TestFailedBuildLeavesOutput(t *testing.T) {
	breaks := map[string]map[string]string{
		"front matter": {"content/bad.md": "no front matter\n"},
		"renderer":     {"content/fail.md": "---\ntitle: F\n---\nRENDER_FAIL\n"},
		"broken link":  {"content/link.html": "---\ntitle: L\n---\n<a href=\"/missing.html\">x</a>\n"},
		"layout":       {"content/l.html": "---\ntitle: L\nlayout: nope\n---\n"},
		"partial":      {"layouts/post.html": "{{template \"nope\" .}}"},
		"collision":    {"static/about.html": "x"},
	}
	for name, files := range breaks {
		t.Run(name, func(t *testing.T) {
			root := copyFixture(t, "basic")
			o := options(t, root)
			if _, ds := Build(context.Background(), o); diag.HasErrors(ds) {
				t.Fatal(ds)
			}
			before := readTree(t, o.Out)
			for path, body := range files {
				os.WriteFile(filepath.Join(root, path), []byte(body), 0o644)
			}
			if _, ds := Build(context.Background(), o); !diag.HasErrors(ds) {
				t.Fatal("build succeeded, want failure")
			}
			diffTrees(t, before, readTree(t, o.Out))
			if left, _ := filepath.Glob(filepath.Join(root, ".swag-build-*")); len(left) > 0 {
				t.Errorf("temporary directories left behind: %v", left)
			}
		})
	}
}

func TestInterruptedBuildLeavesOutput(t *testing.T) {
	root := copyFixture(t, "basic")
	o := options(t, root)
	if _, ds := Build(context.Background(), o); diag.HasErrors(ds) {
		t.Fatal(ds)
	}
	before := readTree(t, o.Out)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ds := Build(ctx, o); !slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return d.Msg == "build interrupted" }) {
		t.Errorf("diagnostics = %v, want build interrupted", ds)
	}
	diffTrees(t, before, readTree(t, o.Out))
}

func TestDiagnostics(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name: "every renderer failure is reported",
			files: map[string]string{
				"content/posts/z.md": "---\ntitle: Z\ndate: 2026-01-01\n---\nRENDER_FAIL\n",
				"content/a.md":       "---\ntitle: A\n---\nRENDER_FAIL\n",
				"content/empty.md":   "---\ntitle: E\n---\n",
			},
			want: []string{
				`content/a.md: render failed (build.test exited 64): fake: cannot render / second line`,
				`content/empty.md: warning: build.test produced no output`,
				`content/posts/z.md: render failed (build.test exited 64): fake: cannot render / second line`,
			},
		},
		{
			name: "broken links map to sources",
			files: map[string]string{
				"content/posts/z.md": "---\ntitle: Z\ndate: 2026-01-01\n---\n<a href=\"/b.html\">b</a> <a href=\"/a.html\">a</a>\n",
				"static/extra.html":  "<img src=\"nope.png\">",
				"layouts/index.html": "<a href=\"/gone/\">x</a>",
			},
			want: []string{
				`content/index.md: broken link "/gone/"`,
				`content/posts/z.md: broken link "/a.html"`,
				`content/posts/z.md: broken link "/b.html"`,
				`static/extra.html: broken link "nope.png"`,
			},
		},
		{
			name: "collisions name both files",
			files: map[string]string{
				"content/about.html": "---\ntitle: A\n---\n",
				"static/rss.xml":     "x",
				"static/About.html":  "x",
				"static/sitemap.xml": "x",
			},
			want: []string{
				`content/about.md: writes docs/about.html, which content/about.html also writes`,
				`static/About.html: writes docs/About.html, which content/about.html also writes`,
				`static/rss.xml: writes docs/rss.xml, which swag also generates`,
				`static/sitemap.xml: writes docs/sitemap.xml, which swag also generates`,
			},
		},
		{
			name:  "layout parse errors point into the layout",
			files: map[string]string{"layouts/note.html": "<html>\n{{if}}\n"},
			want:  []string{`layouts/note.html:2: missing value for if`},
		},
		{
			name:  "layout execution errors point into the partial",
			files: map[string]string{"layouts/partials/footer.html": "<footer>\n{{.Page.Nope}}\n</footer>\n"},
			want: []string{
				`layouts/partials/footer.html:2:7: executing "footer" at <.Page.Nope>: can't evaluate field Nope in type *content.Page (building content/about.md)`,
				`layouts/partials/footer.html:2:7: executing "footer" at <.Page.Nope>: can't evaluate field Nope in type *content.Page (building content/index.md)`,
				`layouts/partials/footer.html:2:7: executing "footer" at <.Page.Nope>: can't evaluate field Nope in type *content.Page (building content/posts/first.md)`,
				`layouts/partials/footer.html:2:7: executing "footer" at <.Page.Nope>: can't evaluate field Nope in type *content.Page (building content/posts/second.html)`,
			},
		},
		{
			name:  "missing partial",
			files: map[string]string{"layouts/note.html": "<html>\n{{template \"nav\" .}}\n"},
			want:  []string{`layouts/note.html:2:11: no such template "nav" (building content/about.md)`},
		},
		{
			name:  "missing layout",
			files: map[string]string{"content/x.html": "---\ntitle: X\nlayout: wide\n---\n"},
			want:  []string{`content/x.html: layout "wide" not found in layouts/ or the embedded defaults`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t, "basic")
			for path, body := range tt.files {
				os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755)
				os.WriteFile(filepath.Join(root, path), []byte(body), 0o644)
			}
			_, ds := Generate(context.Background(), t.TempDir(), options(t, root))
			if got := formatted(root, ds); !slices.Equal(got, tt.want) {
				t.Errorf("diagnostics:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestRendererNotFound(t *testing.T) {
	root := copyFixture(t, "basic")
	o := options(t, root)
	o.Config.Render["md"] = []string{"swag-no-such-renderer"}
	_, ds := Generate(context.Background(), t.TempDir(), o)
	if len(ds) == 0 || !strings.Contains(ds[0].Msg, "swag-no-such-renderer not found on PATH; install it or change render.md") {
		t.Errorf("diagnostics = %v", ds)
	}
}

func TestDrafts(t *testing.T) {
	o := options(t, fixture("basic"))
	o.Drafts = true
	dir := t.TempDir()
	_, ds := Generate(context.Background(), dir, o)
	if got := formatted(o.Root, ds); !slices.Equal(got, []string{`content/posts/draft.md: broken link "/nowhere.html"`}) {
		t.Errorf("diagnostics = %q", got)
	}
	if !strings.Contains(readTree(t, dir)["index.html"], "Not yet") {
		t.Error("draft missing from the home page with --drafts")
	}
}

func TestPandoc(t *testing.T) {
	pandoc, err := exec.LookPath("pandoc")
	if err != nil {
		t.Skip("pandoc not on PATH")
	}
	o := options(t, fixture("basic"))
	o.Config.Render["md"] = []string{pandoc, "--from=gfm", "--to=html5"}
	dir := t.TempDir()
	if _, ds := Generate(context.Background(), dir, o); len(ds) != 0 {
		t.Fatal(ds)
	}
	if page := readTree(t, dir)["posts/first.html"]; !strings.Contains(page, `<img src="pic.png" alt="pic">`) {
		t.Errorf("pandoc output:\n%s", page)
	}
}
