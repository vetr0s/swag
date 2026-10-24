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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/vetr0s/swag/internal/content"
	"github.com/vetr0s/swag/internal/diag"
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

// Cache holds successful results in memory, keyed by everything a renderer sees.
type Cache struct {
	mu sync.Mutex
	m  map[[32]byte]Result
}

func NewCache() *Cache { return &Cache{m: map[[32]byte]Result{}} }

func (c *Cache) get(k [32]byte) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *Cache) put(k [32]byte, v Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}

func key(j Job, drafts string) [32]byte {
	h := sha256.New()
	for _, a := range append(slices.Clip(j.Argv), "", j.Source, drafts) {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	h.Write(j.Body)
	return [32]byte(h.Sum(nil))
}

// Run renders every job on runtime.NumCPU() workers.
func Run(ctx context.Context, jobs []Job, opts Options) []Result {
	results := make([]Result, len(jobs))
	idx := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.NumCPU(), len(jobs)) {
		wg.Go(func() {
			for i := range idx {
				results[i] = one(ctx, jobs[i], opts)
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

func one(parent context.Context, j Job, opts Options) Result {
	drafts := "0"
	if opts.Drafts {
		drafts = "1"
	}
	k := key(j, drafts)
	if opts.Cache != nil {
		if r, ok := opts.Cache.get(k); ok {
			return r
		}
	}
	path := filepath.Join(opts.Root, filepath.FromSlash(j.Source))
	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, j.Argv[0], j.Argv[1:]...)
	cmd.Dir = opts.Root
	cmd.WaitDelay = time.Second // a stray grandchild holding stdout must not outlive the timeout
	cmd.Env = append(os.Environ(), "SWAG_ROOT="+opts.Root, "SWAG_SOURCE="+j.Source, "SWAG_DRAFTS="+drafts)
	cmd.Stdin = bytes.NewReader(j.Body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	name := filepath.Base(j.Argv[0])
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.Is(err, exec.ErrNotFound):
		return fail(path, "render failed: %s not found on PATH; install it or change render.%s in swag.conf", name, content.Ext(j.Source))
	case parent.Err() != nil:
		return fail(path, "render failed (%s interrupted)", name)
	case ctx.Err() != nil:
		return fail(path, "render failed (%s timed out after %v)%s", name, opts.Timeout, firstLines(stderr.Bytes()))
	case errors.As(err, &exitErr):
		return fail(path, "render failed (%s exited %d)%s", name, exitErr.ExitCode(), firstLines(stderr.Bytes()))
	default:
		return fail(path, "render failed: %v", err)
	}
	r := Result{HTML: stdout.Bytes(), Stderr: stderr.Bytes()}
	if len(bytes.TrimSpace(r.HTML)) == 0 {
		r.Diags = []diag.Diagnostic{diag.Warnf(path, 0, "%s produced no output", name)}
	}
	if opts.Cache != nil {
		opts.Cache.put(k, r)
	}
	return r
}

func fail(path, format string, args ...any) Result {
	return Result{Diags: []diag.Diagnostic{diag.Errorf(path, 0, format, args...)}}
}

// firstLines returns up to five lines of stderr as ": line1 / line2".
func firstLines(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	if lines[0] == "" {
		return ""
	}
	return ": " + strings.Join(lines[:min(len(lines), 5)], " / ")
}
