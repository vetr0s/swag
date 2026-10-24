// Package site finds the site root, checks that the output directory is safe
// to replace wholesale, and walks source trees in sorted order.
package site

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FindRoot walks up from start to the first directory holding swag.conf,
// then, failing that, to the first holding a content/ directory.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for _, marker := range []string{"swag.conf", "content"} {
		for d := dir; ; d = filepath.Dir(d) {
			info, err := os.Stat(filepath.Join(d, marker))
			if err == nil && info.IsDir() == (marker == "content") {
				return d, nil
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return "", fmt.Errorf("no site found: neither %s nor any parent directory contains swag.conf or a content/ directory", dir)
}

// CheckOut resolves out against root and refuses any directory whose
// replacement could destroy the site or its sources.
func CheckOut(root, out string) (string, error) {
	abs := out
	if !filepath.IsAbs(out) {
		abs = filepath.Join(root, out)
	}
	abs = filepath.Clean(abs)
	if !within(root, abs) || abs == root {
		return "", fmt.Errorf("unsafe output directory %q: it must be a subdirectory of the site root %s", out, root)
	}
	for _, src := range []string{"content", "static", "layouts"} {
		srcAbs := filepath.Join(root, src)
		if within(srcAbs, abs) || within(abs, srcAbs) {
			return "", fmt.Errorf("unsafe output directory %q: it overlaps %s/", out, src)
		}
	}
	return abs, nil
}

// within reports whether path is dir or lies inside it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Walk returns the slash-separated paths of the non-directories under dir,
// sorted, skipping editor junk. Dot-named entries are skipped unless keepDot.
// A missing dir yields no files.
func Walk(dir string, keepDot bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return err
		}
		if p == dir {
			return nil
		}
		if Ignored(d.Name(), keepDot) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, err
}

func Ignored(name string, keepDot bool) bool {
	if strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".swp") || name == ".DS_Store" {
		return true
	}
	return !keepDot && strings.HasPrefix(name, ".")
}
