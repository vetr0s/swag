package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
