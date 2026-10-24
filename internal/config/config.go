// Package config parses swag.conf, a strict `key = value` file, and the
// renderer command lines inside it.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vetr0s/swag/internal/diag"
)

type Config struct {
	Title       string
	URL         string // no trailing slash; "" when unset
	Author      string
	Description string
	Language    string
	Out         string
	Port        int
	Timeout     time.Duration
	Render      map[string][]string // lowercase ext -> argv; an empty argv disables the ext
	Params      map[string]string
}

var keyRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

// Load reads swag.conf in root. A missing file yields the defaults.
func Load(root string) (*Config, []diag.Diagnostic) {
	path := filepath.Join(root, "swag.conf")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = nil, nil
	}
	if err != nil {
		return nil, []diag.Diagnostic{diag.Errorf(path, 0, "%v", err)}
	}
	return Parse(path, data, filepath.Base(root))
}

// Parse parses the contents of the swag.conf at path. defaultTitle is used
// when site.title is absent.
func Parse(path string, data []byte, defaultTitle string) (*Config, []diag.Diagnostic) {
	c := &Config{
		Title:    defaultTitle,
		Language: "en",
		Out:      "docs",
		Port:     8080,
		Timeout:  30 * time.Second,
		Render:   map[string][]string{"md": {"pandoc", "--from=gfm", "--to=html5"}},
		Params:   map[string]string{},
	}
	var ds []diag.Diagnostic
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		switch {
		case !ok:
			ds = append(ds, diag.Errorf(path, n, "expected key = value"))
			continue
		case !keyRE.MatchString(key):
			ds = append(ds, diag.Errorf(path, n, "invalid key %q: keys are lowercase letters, digits, '_', '.' and '-'", key))
			continue
		case seen[key]:
			ds = append(ds, diag.Errorf(path, n, "duplicate key %q", key))
			continue
		}
		seen[key] = true
		if msg := c.set(key, val); msg != "" {
			ds = append(ds, diag.Errorf(path, n, "%s", msg))
		}
	}
	return c, ds
}

// set applies one setting and returns an error message, or "" on success.
func (c *Config) set(key, val string) string {
	switch key {
	case "site.title":
		c.Title = val
	case "site.url":
		val = strings.TrimSuffix(val, "/")
		if val != "" && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
			return "site.url must start with http:// or https://"
		}
		c.URL = val
	case "site.author":
		c.Author = val
	case "site.description":
		c.Description = val
	case "site.language":
		c.Language = val
	case "build.out":
		if val == "" {
			return "build.out must not be empty"
		}
		c.Out = val
	case "serve.port":
		p, err := strconv.Atoi(val)
		if err != nil || p < 1 || p > 65535 {
			return "serve.port must be a number from 1 to 65535"
		}
		c.Port = p
	case "render.timeout":
		d, err := time.ParseDuration(val)
		if err != nil || d <= 0 {
			return "render.timeout must be a positive Go duration such as 30s"
		}
		c.Timeout = d
	default:
		if name, ok := strings.CutPrefix(key, "params."); ok && name != "" {
			c.Params[name] = val
			return ""
		}
		ext, ok := strings.CutPrefix(key, "render.")
		if !ok || ext == "" {
			return "unknown key " + strconv.Quote(key)
		}
		if ext == "html" {
			return "render.html cannot be set: .html sources are always passed through"
		}
		argv, err := Tokenize(val)
		if err != nil {
			return key + ": " + err.Error()
		}
		c.Render[ext] = argv
	}
	return ""
}

// Tokenize splits a command line on whitespace. Double quotes group a token
// and \" inside quotes is a literal quote. Nothing else is special.
func Tokenize(s string) ([]string, error) {
	var argv []string
	var tok strings.Builder
	inTok, inQuote := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inQuote && ch == '\\' && i+1 < len(s) && s[i+1] == '"':
			tok.WriteByte('"')
			i++
		case ch == '"':
			inQuote, inTok = !inQuote, true
		case !inQuote && (ch == ' ' || ch == '\t'):
			if inTok {
				argv = append(argv, tok.String())
				tok.Reset()
				inTok = false
			}
		default:
			tok.WriteByte(ch)
			inTok = true
		}
	}
	if inQuote {
		return nil, errors.New("unterminated double quote")
	}
	if inTok {
		argv = append(argv, tok.String())
	}
	return argv, nil
}
