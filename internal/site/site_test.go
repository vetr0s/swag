package site

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mkdirs(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if filepath.Ext(p) != "" || filepath.Base(p) == "swag.conf" {
			os.MkdirAll(filepath.Dir(full), 0o755)
			if err := os.WriteFile(full, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindRoot(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	mkdirs(t, tmp, "a/swag.conf", "a/b/content/posts", "c/content/posts", "d")
	tests := []struct{ start, want string }{
		{"a/b/content/posts", "a"}, // swag.conf beats a nearer content/
		{"a", "a"},
		{"c/content/posts", "c"},
		{"c", "c"},
	}
	for _, tt := range tests {
		got, err := FindRoot(filepath.Join(tmp, tt.start))
		if err != nil || got != filepath.Join(tmp, tt.want) {
			t.Errorf("FindRoot(%s) = %s, %v; want %s", tt.start, got, err, tt.want)
		}
	}
	if _, err := FindRoot(filepath.Join(tmp, "d")); err == nil {
		t.Error("FindRoot(d): want error")
	}
}

func TestCheckOut(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(tmp, "site")
	mkdirs(t, root, "swag.conf", "content/a.md", "static", "layouts", ".git/HEAD", "docs/index.html", "empty", "notes/x.txt")
	mkdirs(t, tmp, "outside")
	os.Symlink(filepath.Join(root, "content"), filepath.Join(root, "public"))
	os.Symlink(tmp, filepath.Join(root, "up"))
	for _, out := range []string{"docs", "build/site", "empty", filepath.Join(root, "docs")} {
		if _, err := CheckOut(root, out); err != nil {
			t.Errorf("CheckOut(%s): %v", out, err)
		}
	}
	for _, out := range []string{
		".", "..", "/", tmp, "../outside", "docs/../..",
		"content", "content/out", "static/x", "layouts",
		"Content", "STATIC/x", // the same directories on a case-insensitive volume
		"public/site", "up/outside", // symlinked parents that lead into content/ or out of the root
		".git", "swag.conf", "notes", // existing things that are not earlier swag output
	} {
		if _, err := CheckOut(root, out); err == nil {
			t.Errorf("CheckOut(%s): want error", out)
		}
	}
}

func TestWalk(t *testing.T) {
	tmp := t.TempDir()
	mkdirs(t, tmp, "b.txt", "a/z.md", ".hidden/x.md", ".nojekyll.x", "a/.DS_Store", "a/x.md~", "a/.x.md.swp", "a/y.swp")
	got, err := Walk(tmp, false)
	if want := []string{"a/z.md", "b.txt"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("Walk(keepDot=false) = %q, %v; want %q", got, err, want)
	}
	got, _ = Walk(tmp, true)
	if want := []string{".hidden/x.md", ".nojekyll.x", "a/z.md", "b.txt"}; !slices.Equal(got, want) {
		t.Errorf("Walk(keepDot=true) = %q; want %q", got, want)
	}
	if got, err := Walk(filepath.Join(tmp, "missing"), false); err != nil || got != nil {
		t.Errorf("Walk(missing) = %q, %v", got, err)
	}
}
