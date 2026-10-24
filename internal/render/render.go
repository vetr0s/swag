// Package render runs renderer commands over page bodies on a worker pool.
// Each command gets the body on stdin and returns an HTML fragment on
// stdout. Results come back in job order, so parallelism never shows in
// the output. A Cache lets `swag serve` skip commands whose input is unchanged.
package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"swag/internal/diag"
)

type Job struct {
	Source string // root-relative, e.g. "content/posts/foo.md"
	Argv   []string
	Body   []byte
}

type Result struct {
	HTML   []byte
	Stderr []byte // non-empty stderr from a successful run
	Diags  []diag.Diagnostic
}

type Options struct {
	Root    string
	Drafts  bool
	Timeout time.Duration
	Cache   *Cache // nil disables caching
}

// Cache holds renderer output in memory, keyed by argv and body.
type Cache struct {
	mu sync.Mutex
	m  map[[32]byte][]byte
}

func NewCache() *Cache { return &Cache{m: map[[32]byte][]byte{}} }

func (c *Cache) get(k [32]byte) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *Cache) put(k [32]byte, v []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}

func key(argv []string, body []byte) [32]byte {
	h := sha256.New()
	for _, a := range argv {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	h.Write([]byte{0})
	h.Write(body)
	return [32]byte(h.Sum(nil))
}

// Run renders every job on runtime.NumCPU() workers.
func Run(jobs []Job, opts Options) []Result {
	results := make([]Result, len(jobs))
	idx := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(jobs)) {
		wg.Go(func() {
			for i := range idx {
				results[i] = one(jobs[i], opts)
			}
		})
	}
	for i := range jobs {
		idx <- i
	}
	close(idx)
	wg.Wait()
	return results
}

func one(j Job, opts Options) Result {
	k := key(j.Argv, j.Body)
	if opts.Cache != nil {
		if html, ok := opts.Cache.get(k); ok {
			return Result{HTML: html}
		}
	}
	path := filepath.Join(opts.Root, filepath.FromSlash(j.Source))
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, j.Argv[0], j.Argv[1:]...)
	cmd.Dir = opts.Root
	cmd.WaitDelay = time.Second // a stray grandchild holding stdout must not outlive the timeout
	drafts := "0"
	if opts.Drafts {
		drafts = "1"
	}
	cmd.Env = append(os.Environ(), "SWAG_ROOT="+opts.Root, "SWAG_SOURCE="+j.Source, "SWAG_DRAFTS="+drafts)
	cmd.Stdin = bytes.NewReader(j.Body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	name := filepath.Base(j.Argv[0])
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return fail(path, "render failed: %s not found on PATH; install it or change render.%s in swag.conf", name, extOf(j.Source))
	case ctx.Err() != nil:
		return fail(path, "render failed (%s timed out after %v)%s", name, opts.Timeout, firstLines(stderr.Bytes()))
	case errors.As(err, &exitErr):
		return fail(path, "render failed (%s exited %d)%s", name, exitErr.ExitCode(), firstLines(stderr.Bytes()))
	case err != nil:
		return fail(path, "render failed: %v", err)
	}
	r := Result{HTML: stdout.Bytes(), Stderr: stderr.Bytes()}
	if len(bytes.TrimSpace(r.HTML)) == 0 {
		r.Diags = []diag.Diagnostic{diag.Warnf(path, 0, "%s produced no output", name)}
	}
	if opts.Cache != nil {
		opts.Cache.put(k, r.HTML)
	}
	return r
}

func fail(path, format string, args ...any) Result {
	return Result{Diags: []diag.Diagnostic{diag.Errorf(path, 0, format, args...)}}
}

func extOf(source string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(source), "."))
}

// firstLines returns up to five lines of stderr as ": line1 / line2".
func firstLines(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	if lines[0] == "" {
		return ""
	}
	return ": " + strings.Join(lines[:min(len(lines), 5)], " / ")
}
