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
// replacement could destroy the site or its sources. Paths are compared after
// resolving symlinks and folding case, because macOS volumes ignore case.
func CheckOut(root, out string) (string, error) {
	abs := out
	if !filepath.IsAbs(out) {
		abs = filepath.Join(root, out)
	}
	abs = filepath.Clean(abs)
	real, realRoot := resolve(abs), resolve(root)
	if !within(realRoot, real) || within(real, realRoot) {
		return "", fmt.Errorf("unsafe output directory %q: it must be a subdirectory of the site root %s", out, root)
	}
	for _, src := range []string{"content", "static", "layouts"} {
		srcReal := resolve(filepath.Join(root, src))
		if within(srcReal, real) || within(real, srcReal) {
			return "", fmt.Errorf("unsafe output directory %q: it overlaps %s/", out, src)
		}
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return abs, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("unsafe output directory %q: it exists and is not a directory", out)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil && len(entries) > 0 {
		return "", fmt.Errorf("unsafe output directory %q: it is not empty and has no index.html, so it is not earlier swag output", out)
	}
	return abs, nil
}

// resolve follows symlinks in the longest existing prefix of p.
func resolve(p string) string {
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// within reports whether path is dir or lies inside it, ignoring case.
func within(dir, path string) bool {
	rel, err := filepath.Rel(strings.ToLower(dir), strings.ToLower(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Walk returns the sorted, slash-separated non-directories under dir, or none
// when dir is missing. Editor junk is skipped, and dot-named entries unless keepDot.
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
