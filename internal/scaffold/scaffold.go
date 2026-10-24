// Package scaffold creates new post and page sources for `swag new`.
package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Slug lowercases title, keeps ASCII letters and digits, and turns every run
// of anything else into a single '-'.
func Slug(title string) (string, error) {
	var b strings.Builder
	dash := false
	for _, r := range title {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A' // strings.ToLower would also map İ and the Kelvin sign to ASCII
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("cannot make a file name from %q: the title needs at least one ASCII letter or digit", title)
	}
	return b.String(), nil
}

// New creates the source file for title and returns its absolute path. ext
// must be html or have a renderer in render.
func New(root string, render map[string][]string, title, ext string, page bool, today time.Time) (string, error) {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext != "html" && len(render[ext]) == 0 {
		return "", fmt.Errorf("no renderer for .%s files: use --ext html or set render.%s in swag.conf", ext, ext)
	}
	if strings.ContainsAny(title, "\r\n") {
		return "", errors.New("the title must be a single line")
	}
	slug, err := Slug(title)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "content", "posts")
	front := "---\ntitle: " + title + "\ndate: " + today.Format("2006-01-02") + "\ndraft: true\n---\n\n"
	if page {
		dir = filepath.Join(root, "content")
		front = "---\ntitle: " + title + "\ndraft: true\n---\n\n"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, slug+"."+ext)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("%s already exists", path)
	}
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(front)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path) // a half-written draft would block the retry with "already exists"
		return "", err
	}
	return path, nil
}

const conf = `site.title       = %s
# site.url         = https://example.com
# site.author      =
# site.description =
`

// Init makes dir a site by creating swag.conf and content/, and creates dir
// first if needed. It refuses when either already exists.
func Init(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, name := range []string{"swag.conf", "content"} {
		if _, err := os.Lstat(filepath.Join(abs, name)); err == nil {
			return fmt.Errorf("%s already exists", filepath.Join(dir, name))
		}
	}
	if err := os.MkdirAll(filepath.Join(abs, "content"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(abs, "swag.conf"), fmt.Appendf(nil, conf, filepath.Base(abs)), 0o644)
}
