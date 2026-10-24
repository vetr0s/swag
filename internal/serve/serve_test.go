package serve

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vetr0s/swag/internal/diag"
)

func TestInject(t *testing.T) {
	if got := string(Inject([]byte("<p>a</p></BODY></html>"), 3)); !strings.HasPrefix(got, "<p>a</p><script>") ||
		!strings.Contains(got, `const b = "3"`) || !strings.HasSuffix(got, "</script></BODY></html>") {
		t.Errorf("Inject before </body> = %s", got)
	}
	if got := string(Inject([]byte("<p>fragment</p>"), 1)); !strings.HasPrefix(got, "<p>fragment</p><script>") || !strings.HasSuffix(got, "</script>") {
		t.Errorf("Inject without </body> = %s", got)
	}
}

func get(t *testing.T, s *Server, url string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Result(), string(body)
}

func TestHandler(t *testing.T) {
	s := &Server{}
	if res, body := get(t, s, "/"); res.StatusCode != 503 || !strings.Contains(body, "<script>") {
		t.Errorf("before first build: %d %s", res.StatusCode, body)
	}

	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html><body>home</body></html>\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "index.html"), []byte("notes"), 0o644)
	os.WriteFile(filepath.Join(dir, "style.css"), []byte("body{}"), 0o644)
	s.dir, s.build = dir, 7

	if res, body := get(t, s, "/__swag/build"); body != "7" || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("build endpoint: %q %v", body, res.Header)
	}
	res, body := get(t, s, "/")
	if res.StatusCode != 200 || !strings.Contains(body, `home<script>`) || !strings.Contains(body, `const b = "7"`) ||
		res.Header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("index: %d %v %s", res.StatusCode, res.Header, body)
	}
	if _, body := get(t, s, "/notes/"); !strings.HasPrefix(body, "notes<script>") {
		t.Errorf("directory index: %s", body)
	}
	if res, _ := get(t, s, "/notes"); res.StatusCode != 301 || res.Header.Get("Location") != "/notes/" {
		t.Errorf("directory redirect: %d %v", res.StatusCode, res.Header)
	}
	if _, body := get(t, s, "/style.css"); body != "body{}" {
		t.Errorf("static file was modified: %q", body)
	}
	if res, _ := get(t, s, "/missing.html"); res.StatusCode != 404 {
		t.Errorf("missing page: %d", res.StatusCode)
	}
	if res, _ := get(t, s, "/../../etc/passwd"); res.StatusCode == 200 {
		t.Errorf("path traversal: %d", res.StatusCode)
	}
	if built, _ := os.ReadFile(filepath.Join(dir, "index.html")); strings.Contains(string(built), "<script>") {
		t.Error("injection leaked into the built file")
	}
}

func TestFingerprint(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "content"), 0o755)
	post := filepath.Join(root, "content", "a.md")
	os.WriteFile(post, []byte("a"), 0o644)
	fp := Fingerprint(root)
	if Fingerprint(root) != fp {
		t.Fatal("fingerprint is not stable")
	}

	os.WriteFile(filepath.Join(root, "content", ".a.md.swp"), []byte("swap"), 0o644)
	os.WriteFile(filepath.Join(root, "content", "a.md~"), []byte("backup"), 0o644)
	os.MkdirAll(filepath.Join(root, "content", ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "content", ".git", "HEAD"), []byte("x"), 0o644)
	if Fingerprint(root) != fp {
		t.Error("editor junk changed the fingerprint")
	}

	steps := []func(){
		func() { os.WriteFile(post, []byte("ab"), 0o644) },
		func() { os.Chtimes(post, time.Now(), time.Now().Add(time.Hour)) },
		func() { os.WriteFile(filepath.Join(root, "swag.conf"), nil, 0o644) },
		func() {
			os.MkdirAll(filepath.Join(root, "layouts"), 0o755)
			os.WriteFile(filepath.Join(root, "layouts", "x.html"), nil, 0o644)
		},
		func() {
			os.MkdirAll(filepath.Join(root, "static"), 0o755)
			os.WriteFile(filepath.Join(root, "static", ".nojekyll"), nil, 0o644)
		},
		func() { os.Remove(post) },
	}
	for i, step := range steps {
		step()
		next := Fingerprint(root)
		if next == fp {
			t.Errorf("step %d did not change the fingerprint", i)
		}
		fp = next
	}
}

func TestRebuild(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "content"), 0o755)
	os.MkdirAll(filepath.Join(root, "layouts"), 0o755)
	os.WriteFile(filepath.Join(root, "swag.conf"), []byte("site.title = T\n"), 0o644)
	os.WriteFile(filepath.Join(root, "content", "index.html"), []byte("---\ntitle: Home\n---\n<p>home</p>"), 0o644)
	layout := filepath.Join(root, "layouts", "index.html")
	os.WriteFile(layout, []byte("{{.Page.Content}}"), 0o644)

	var log bytes.Buffer
	var reported int
	s := &Server{o: Options{Root: root, Stderr: &log, Report: func(ds []diag.Diagnostic) { reported += len(ds) }}, tmp: t.TempDir()}
	ctx := context.Background()
	s.rebuild(ctx)
	first, n := s.current()
	if n != 1 || first == "" {
		t.Fatalf("first build: dir %q, build %d, log %s", first, n, &log)
	}

	os.WriteFile(layout, []byte("{{.Page.Content"), 0o644)
	s.rebuild(ctx)
	if dir, n := s.current(); dir != first || n != 1 || reported == 0 || !strings.Contains(log.String(), "last good build") {
		t.Errorf("broken build replaced the good one: dir %q, build %d, reported %d", dir, n, reported)
	}

	os.WriteFile(layout, []byte("<main>{{.Page.Content}}</main>"), 0o644)
	s.rebuild(ctx)
	dir, n := s.current()
	if n != 2 || dir == first {
		t.Errorf("fixed build: dir %q, build %d", dir, n)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("old build dir was not removed: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(data) != "<main><p>home</p></main>\n" {
		t.Errorf("index.html = %q", data)
	}
}
