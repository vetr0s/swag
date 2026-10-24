// Package cli parses arguments, dispatches subcommands, prints diagnostics
// and maps outcomes to exit codes: 0 success, 1 build or content error,
// 2 usage error. stdout carries only machine-readable results.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/vetr0s/swag/internal/build"
	"github.com/vetr0s/swag/internal/config"
	"github.com/vetr0s/swag/internal/diag"
	"github.com/vetr0s/swag/internal/scaffold"
	"github.com/vetr0s/swag/internal/serve"
	"github.com/vetr0s/swag/internal/site"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

var usages = map[string]string{
	"build":   "swag build [--drafts] [--out <dir>] [-v] [-q]\n\nBuild the site into build.out (default docs/). --out is relative to the site root.",
	"serve":   "swag serve [--drafts] [--port <n>] [--host <addr>] [-v] [-q]\n\nBuild into a temporary directory, serve it locally and rebuild on changes.",
	"new":     "swag new [--page] [--ext <ext>] \"<title>\"\n\nCreate a draft post (or page) and print its path.",
	"version": "swag version\n\nPrint the swag version.",
	"help":    "swag help [command]\n\nShow help for swag or for one command.",
}

const usage = `usage: swag [-C <dir>] <command> [flags]

commands:
  build     build the site into build.out
  serve     preview the site locally with live reload
  new       create a new post or page
  version   print the version
  help      show help for a command

-C <dir> starts the search for the site root from <dir>.
`

type env struct {
	stdout, stderr io.Writer
	cwd            string
	start          string // where root discovery starts
	quiet          bool   // print errors only
}

// Run executes the command line args and returns the exit code. version
// overrides the build-info version when non-empty.
func Run(version string, args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("swag", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	start := global.String("C", "", "")
	if err := global.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stderr, usage)
		return exitOK
	} else if err != nil {
		fmt.Fprintf(stderr, "swag: %v\n%s", err, usage)
		return exitUsage
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "swag: %v\n", err)
		return exitError
	}
	e := &env{stdout: stdout, stderr: stderr, cwd: cwd, start: cwd}
	if *start != "" {
		if info, err := os.Stat(*start); err != nil || !info.IsDir() {
			fmt.Fprintf(stderr, "swag: -C %s: not a directory\n", *start)
			return exitUsage
		}
		e.start = *start
	}
	args = global.Args()
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "build":
		return e.build(args)
	case "serve":
		return e.serve(args)
	case "new":
		return e.new(args)
	case "version":
		if len(args) > 0 {
			return e.usageError("version", "unexpected argument %q", args[0])
		}
		fmt.Fprintln(stdout, "swag", resolveVersion(version))
		return exitOK
	case "help":
		if len(args) == 0 {
			fmt.Fprint(stderr, usage)
			return exitOK
		}
		if u, ok := usages[args[0]]; ok {
			fmt.Fprintln(stderr, u)
			return exitOK
		}
		cmd = args[0]
	}
	fmt.Fprintf(stderr, "swag: unknown command %q\n%s", cmd, usage)
	return exitUsage
}

func resolveVersion(v string) string {
	if v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value[:min(12, len(s.Value))]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "devel"
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

// flags parses a subcommand's flags and allows at most nargs arguments. ok is
// false after a usage error or -h, with code set to the exit code.
func (e *env) flags(fs *flag.FlagSet, args []string, nargs int) (code int, ok bool) {
	fs.SetOutput(io.Discard)
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprintln(e.stderr, usages[fs.Name()])
		return exitOK, false
	case err != nil:
		return e.usageError(fs.Name(), "%v", err), false
	case fs.NArg() > nargs:
		return e.usageError(fs.Name(), "unexpected argument %q", fs.Arg(nargs)), false
	}
	return 0, true
}

func (e *env) usageError(cmd, format string, args ...any) int {
	fmt.Fprintf(e.stderr, "swag %s: %s\nusage: %s\n", cmd, fmt.Sprintf(format, args...), usages[cmd])
	return exitUsage
}

// load finds the root and reads swag.conf. ok is false after printing errors.
func (e *env) load() (root string, cfg *config.Config, code int, ok bool) {
	root, err := site.FindRoot(e.start)
	if err != nil {
		fmt.Fprintf(e.stderr, "swag: %v\n", err)
		return "", nil, exitUsage, false
	}
	cfg, ds := config.Load(root)
	e.report(ds)
	if diag.HasErrors(ds) {
		return "", nil, exitError, false
	}
	return root, cfg, 0, true
}

func (e *env) report(ds []diag.Diagnostic) {
	diag.Sort(ds)
	for _, d := range ds {
		if e.quiet && d.Warning {
			continue
		}
		fmt.Fprintln(e.stderr, diag.Format(d, e.cwd))
	}
}

func (e *env) log(verbose bool) io.Writer {
	if verbose {
		return e.stderr
	}
	return nil
}

func (e *env) build(args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	drafts := fs.Bool("drafts", false, "")
	out := fs.String("out", "", "")
	verbose := fs.Bool("v", false, "")
	fs.BoolVar(&e.quiet, "q", false, "")
	if code, ok := e.flags(fs, args, 0); !ok {
		return code
	}
	start := time.Now()
	root, cfg, code, ok := e.load()
	if !ok {
		return code
	}
	if *out == "" {
		*out = cfg.Out
	}
	outAbs, err := site.CheckOut(root, *out)
	if err != nil {
		fmt.Fprintf(e.stderr, "swag: %v\n", err)
		return exitUsage
	}
	o := build.Options{Root: root, Config: cfg, Out: outAbs, Drafts: *drafts, Log: e.log(*verbose)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stats, ds := build.Build(ctx, o)
	e.report(ds)
	if diag.HasErrors(ds) {
		return exitError
	}
	if !e.quiet {
		fmt.Fprintf(e.stderr, "swag: built %v in %v -> %s%c\n",
			stats, time.Since(start).Round(time.Millisecond), diag.Rel(e.cwd, outAbs), filepath.Separator)
	}
	return exitOK
}

func (e *env) serve(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	drafts := fs.Bool("drafts", false, "")
	port := fs.Int("port", 0, "")
	host := fs.String("host", "127.0.0.1", "")
	verbose := fs.Bool("v", false, "")
	fs.BoolVar(&e.quiet, "q", false, "")
	if code, ok := e.flags(fs, args, 0); !ok {
		return code
	}
	root, cfg, code, ok := e.load()
	if !ok {
		return code
	}
	if *port == 0 {
		*port = cfg.Port
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := serve.Run(ctx, serve.Options{
		Root:   root,
		Drafts: *drafts,
		Addr:   net.JoinHostPort(*host, strconv.Itoa(*port)),
		Log:    e.log(*verbose),
		Quiet:  e.quiet,
		Stderr: e.stderr,
		Report: e.report,
	})
	if err != nil {
		fmt.Fprintf(e.stderr, "swag: %v\n", err)
		return exitError
	}
	return exitOK
}

func (e *env) new(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	page := fs.Bool("page", false, "")
	ext := fs.String("ext", "md", "")
	if code, ok := e.flags(fs, args, 1); !ok {
		return code
	}
	if fs.NArg() != 1 {
		return e.usageError("new", "expected one title argument")
	}
	root, cfg, code, ok := e.load()
	if !ok {
		return code
	}
	path, err := scaffold.New(root, cfg.Render, fs.Arg(0), *ext, *page, time.Now())
	if err != nil {
		fmt.Fprintf(e.stderr, "swag new: %v\n", err)
		return exitError
	}
	fmt.Fprintln(e.stdout, diag.Rel(e.cwd, path))
	return exitOK
}
