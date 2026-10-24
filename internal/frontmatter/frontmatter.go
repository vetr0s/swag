// Package frontmatter splits a source file into its `key: value` front
// matter block and its body. It does no I/O.
package frontmatter

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/vetr0s/swag/internal/diag"
)

type FrontMatter struct {
	Fields map[string]string
	Lines  map[string]int // line each key was set on
	Body   []byte
}

var keyRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func Parse(path string, src []byte) (*FrontMatter, []diag.Diagnostic) {
	src = bytes.TrimPrefix(src, []byte("\ufeff"))
	fm := &FrontMatter{Fields: map[string]string{}, Lines: map[string]int{}}
	var ds []diag.Diagnostic
	rest, n := src, 0
	next := func() (string, bool) {
		if len(rest) == 0 {
			return "", false
		}
		line, after, _ := bytes.Cut(rest, []byte("\n"))
		rest, n = after, n+1
		return strings.TrimRight(string(line), " \t\r"), true
	}
	if first, _ := next(); first != "---" {
		return nil, []diag.Diagnostic{diag.Errorf(path, 1, "missing front matter: the file must start with a --- line")}
	}
	for {
		line, ok := next()
		if !ok {
			return nil, []diag.Diagnostic{diag.Errorf(path, 1, "unterminated front matter: no closing --- line")}
		}
		if line == "---" {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch {
		case !ok:
			ds = append(ds, diag.Errorf(path, n, "expected key: value"))
		case !keyRE.MatchString(key):
			ds = append(ds, diag.Errorf(path, n, "invalid key %q: keys are lowercase letters, digits, '_' and '-'", key))
		case fm.Lines[key] != 0:
			ds = append(ds, diag.Errorf(path, n, "duplicate key %q (first set on line %d)", key, fm.Lines[key]))
		default:
			fm.Fields[key], fm.Lines[key] = val, n
		}
	}
	if blank, after, _ := bytes.Cut(rest, []byte("\n")); len(rest) > 0 && len(bytes.TrimRight(blank, "\r")) == 0 {
		rest = after
	}
	fm.Body = rest
	return fm, ds
}
