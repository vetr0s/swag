// Package serve is the `swag serve` local preview. Each rebuild goes into a
// fresh directory under os.TempDir(), never into the site's output, and the
// handler switches to it only once it built cleanly, so a broken edit keeps
// the last good build on screen. A polling watcher triggers rebuilds, and
// HTML responses get a script that reloads the page after each one.
package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vetr0s/swag/internal/build"
	"github.com/vetr0s/swag/internal/config"
	"github.com/vetr0s/swag/internal/diag"
	"github.com/vetr0s/swag/internal/render"
	"github.com/vetr0s/swag/internal/site"
)

type Options struct {
	Root   string
	Drafts bool
	Addr   string    // host:port
	Log    io.Writer // receives the written-file list and renderer stderr; nil for none
	Quiet  bool
	Stderr io.Writer
	Report func([]diag.Diagnostic) // prints diagnostics
}

type Server struct {
	o     Options
	tmp   string
	cache *render.Cache // shortcut: never evicts, grows with every edit; add eviction if long sessions get heavy

	mu    sync.Mutex
	dir   string // current good build; "" before the first one
	build int    // count of good builds
}

// Run serves until ctx is cancelled.
func Run(ctx context.Context, o Options) error {
	ln, err := net.Listen("tcp", o.Addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %v (is another server using the port? pass --port)", o.Addr, err)
	}
	tmp, err := os.MkdirTemp("", "swag-serve-*")
	if err != nil {
		ln.Close()
		return err
	}
	defer os.RemoveAll(tmp)
	s := &Server{o: o, tmp: tmp, cache: render.NewCache()}
	// The baseline comes first so an edit made during the first build still triggers a rebuild.
	last, pending := Fingerprint(o.Root), false
	s.rebuild(ctx)
	srv := &http.Server{Handler: s}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	fmt.Fprintf(o.Stderr, "swag: serving http://%s (Ctrl-C to stop)\n", ln.Addr())

	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return srv.Shutdown(shutdown)
		case err := <-served:
			return err
		case <-tick.C:
		}
		// Rebuild once the tree has stopped changing for one tick, so an
		// editor's burst of writes causes a single rebuild.
		fp := Fingerprint(o.Root)
		if fp != last {
			last, pending = fp, true
		} else if pending {
			pending = false
			s.rebuild(ctx)
		}
	}
}

func (s *Server) rebuild(ctx context.Context) {
	start := time.Now()
	cfg, ds := config.Load(s.o.Root)
	if diag.HasErrors(ds) {
		s.o.Report(ds)
		return
	}
	dir, err := os.MkdirTemp(s.tmp, "build-*")
	if err != nil {
		s.o.Report([]diag.Diagnostic{diag.Errorf("", 0, "%v", err)})
		return
	}
	stats, bds := build.Generate(ctx, dir, build.Options{
		Root:         s.o.Root,
		Config:       cfg,
		Out:          filepath.Join(s.o.Root, cfg.Out),
		Drafts:       s.o.Drafts,
		Log:          s.o.Log,
		Cache:        s.cache,
		LinkWarnings: true,
	})
	if ctx.Err() != nil {
		os.RemoveAll(dir)
		return
	}
	ds = append(ds, bds...)
	s.o.Report(ds)
	if diag.HasErrors(ds) {
		os.RemoveAll(dir)
		fmt.Fprintln(s.o.Stderr, "swag: build failed; still serving the last good build")
		return
	}
	s.mu.Lock()
	old := s.dir
	s.dir = dir
	s.build++
	s.mu.Unlock()
	if old != "" {
		os.RemoveAll(old)
	}
	if !s.o.Quiet {
		fmt.Fprintf(s.o.Stderr, "swag: built %v in %v\n", stats, time.Since(start).Round(time.Millisecond))
	}
}

func (s *Server) current() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir, s.build
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	dir, n := s.current()
	if r.URL.Path == "/__swag/build" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, n)
		return
	}
	if dir == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write(Inject([]byte("<p>swag: no successful build yet. See the terminal for errors.</p>"), n))
		return
	}
	name := path.Clean("/" + r.URL.Path)
	if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil && info.IsDir() {
		if name != "/" && !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		name = path.Join(name, "index.html")
	}
	if path.Ext(name) != ".html" {
		http.ServeFile(w, r, filepath.Join(dir, filepath.FromSlash(name)))
		return
	}
	data, err := fs.ReadFile(os.DirFS(dir), name[1:])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(Inject(data, n))
}

const reload = `<script>(() => { const b = "%d"; setInterval(async () => { try { const r = await fetch("/__swag/build", {cache: "no-store"}); if (await r.text() !== b) location.reload(); } catch (e) {} }, 1000); })();</script>`

// Inject adds the live reload script, for build number n, before </body>.
func Inject(doc []byte, n int) []byte {
	script := []byte(fmt.Sprintf(reload, n))
	for i := len(doc); ; {
		i = bytes.LastIndex(doc[:i], []byte("</"))
		if i < 0 {
			return append(doc, script...)
		}
		if i+7 <= len(doc) && bytes.EqualFold(doc[i:i+7], []byte("</body>")) {
			return bytes.Join([][]byte{doc[:i], script, doc[i:]}, nil)
		}
	}
}

// Fingerprint summarises the path, size and mod time of every file a build reads.
func Fingerprint(root string) string {
	h := sha256.New()
	files := []string{"swag.conf"}
	for _, d := range []string{"content", "static", "layouts"} {
		rels, _ := site.Walk(filepath.Join(root, d), d == "static")
		for _, rel := range rels {
			files = append(files, path.Join(d, rel))
		}
	}
	for _, f := range files {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil {
			fmt.Fprintf(h, "%s\x00%d\x00%d\n", f, info.Size(), info.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
