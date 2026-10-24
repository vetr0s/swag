package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run("", args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestDispatch(t *testing.T) {
	tests := []struct {
		args   []string
		code   int
		stdout string
	}{
		{nil, exitUsage, ""},
		{[]string{"help"}, exitOK, ""},
		{[]string{"help", "build"}, exitOK, ""},
		{[]string{"help", "nope"}, exitUsage, ""},
		{[]string{"nope"}, exitUsage, ""},
		{[]string{"-Z", "build"}, exitUsage, ""},
		{[]string{"build", "--nope"}, exitUsage, ""},
		{[]string{"build", "-h"}, exitOK, ""},
		{[]string{"new"}, exitUsage, ""},
		{[]string{"-C", t.TempDir(), "build"}, exitUsage, ""}, // no site root
	}
	for _, tt := range tests {
		code, stdout, _ := run(t, tt.args...)
		if code != tt.code || stdout != tt.stdout {
			t.Errorf("swag %q = %d %q, want %d %q", tt.args, code, stdout, tt.code, tt.stdout)
		}
	}
	if code, stdout, _ := run(t, "version"); code != exitOK || !strings.HasPrefix(stdout, "swag ") {
		t.Errorf("version = %d %q", code, stdout)
	}
	var stdout bytes.Buffer
	if Run("v1.2.3", []string{"version"}, &stdout, &stdout); stdout.String() != "swag v1.2.3\n" {
		t.Errorf("version override = %q", stdout.String())
	}
}

func TestNewAndBuild(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "content", "posts"), 0o755)
	os.WriteFile(filepath.Join(root, "swag.conf"), []byte("site.title = T\nsite.url = https://example.com\n"), 0o644)
	t.Chdir(filepath.Join(root, "content", "posts"))

	code, stdout, stderr := run(t, "new", "--ext", "html", "Hello there")
	if code != exitOK || stdout != "hello-there.html\n" || stderr != "" {
		t.Fatalf("new = %d %q %q", code, stdout, stderr)
	}
	if code, _, _ := run(t, "new", "--ext", "html", "Hello there"); code != exitError {
		t.Errorf("new over an existing file = %d", code)
	}
	if code, stdout, _ := run(t, "new", "--page", "--ext=html", "About"); code != exitOK || stdout != filepath.FromSlash("../about.html\n") {
		t.Errorf("new --page = %d %q", code, stdout)
	}

	code, stdout, stderr = run(t, "build", "--drafts")
	if code != exitOK || stdout != "" || !strings.HasPrefix(stderr, "swag: built 2 pages, 1 posts, 1 files in ") ||
		!strings.HasSuffix(stderr, " -> "+filepath.FromSlash("../../docs/")+"\n") {
		t.Errorf("build = %d %q %q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "posts", "hello-there.html")); err != nil {
		t.Error(err)
	}
	if code, _, stderr := run(t, "build", "-q", "--out", "content/x"); code != exitUsage || !strings.Contains(stderr, "unsafe output directory") {
		t.Errorf("unsafe --out = %d %q", code, stderr)
	}

	os.WriteFile(filepath.Join(root, "swag.conf"), []byte("site.title = T\n"), 0o644) // no site.url: a warning
	if code, _, stderr := run(t, "build", "-q", "--drafts"); code != exitOK || stderr != "" {
		t.Errorf("-q printed a warning: %d %q", code, stderr)
	}

	os.WriteFile(filepath.Join(root, "content", "bad.html"), []byte("---\ntitle: x\ndraft: maybe\n---\n"), 0o644)
	code, _, stderr = run(t, "build", "-q")
	if want := filepath.FromSlash("../bad.html") + ":3: invalid draft \"maybe\": use true or false\n"; code != exitError || stderr != want {
		t.Errorf("bad front matter = %d %q, want %q", code, stderr, want)
	}
}
