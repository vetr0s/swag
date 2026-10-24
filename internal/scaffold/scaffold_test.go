package scaffold

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"A zero-dependency static website generator": "a-zero-dependency-static-website-generator",
		"  Go: a tour!! ":  "go-a-tour",
		"C++ & Rust, 2026": "c-rust-2026",
		"café olé":         "caf-ol",
		"__x__":            "x",
		"Über":             "ber",
	}
	for in, want := range tests {
		if got, err := Slug(in); err != nil || got != want {
			t.Errorf("Slug(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "!!!", "日本語"} {
		if _, err := Slug(in); err == nil {
			t.Errorf("Slug(%q): want error", in)
		}
	}
}

func TestNew(t *testing.T) {
	root := t.TempDir()
	render := map[string][]string{"md": {"pandoc"}, "org": {}}
	today := time.Date(2026, 10, 23, 15, 0, 0, 0, time.Local)

	path, err := New(root, render, "Hello, World", "md", false, today)
	if err != nil || path != filepath.Join(root, "content/posts/hello-world.md") {
		t.Fatalf("New post = %s, %v", path, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "---\ntitle: Hello, World\ndate: 2026-10-23\ndraft: true\n---\n\n" {
		t.Errorf("post contents %q", data)
	}
	if _, err := New(root, render, "Hello world", "md", false, today); err == nil {
		t.Error("overwrite: want error")
	}

	path, err = New(root, render, "About me", "html", true, today)
	if err != nil || path != filepath.Join(root, "content/about-me.html") {
		t.Fatalf("New page = %s, %v", path, err)
	}
	if data, _ := os.ReadFile(path); string(data) != "---\ntitle: About me\ndraft: true\n---\n\n" {
		t.Errorf("page contents %q", data)
	}
	for _, ext := range []string{"org", "txt"} {
		if _, err := New(root, render, "x", ext, false, today); err == nil {
			t.Errorf("ext %s: want error", ext)
		}
	}
}
