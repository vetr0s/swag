// Package diag holds the located error and warning values every other
// package returns, and formats them as <path>:<line>:<col>: <message> so
// that Vim's default errorformat can jump to them.
//
// Path is absolute. Formatting makes it relative to the working directory.
package diag

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
)

type Diagnostic struct {
	Path    string // absolute; "" for diagnostics about no particular file
	Line    int    // 1-based; 0 when unknown
	Col     int    // 1-based; 0 when unknown
	Warning bool
	Msg     string
}

func Errorf(path string, line int, format string, args ...any) Diagnostic {
	return Diagnostic{Path: path, Line: line, Msg: fmt.Sprintf(format, args...)}
}

func Warnf(path string, line int, format string, args ...any) Diagnostic {
	return Diagnostic{Path: path, Line: line, Warning: true, Msg: fmt.Sprintf(format, args...)}
}

func HasErrors(ds []Diagnostic) bool {
	return slices.ContainsFunc(ds, func(d Diagnostic) bool { return !d.Warning })
}

func Sort(ds []Diagnostic) {
	slices.SortStableFunc(ds, func(a, b Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col))
	})
}

// Format renders d with its path relative to cwd.
func Format(d Diagnostic, cwd string) string {
	loc := "swag"
	if d.Path != "" {
		loc = Rel(cwd, d.Path)
		if d.Line > 0 {
			loc += fmt.Sprintf(":%d", d.Line)
			if d.Col > 0 {
				loc += fmt.Sprintf(":%d", d.Col)
			}
		}
	}
	if d.Warning {
		return loc + ": warning: " + d.Msg
	}
	return loc + ": " + d.Msg
}

// Rel returns path relative to cwd, or path itself when that fails.
func Rel(cwd, path string) string {
	if rel, err := filepath.Rel(cwd, path); err == nil {
		return rel
	}
	return path
}
