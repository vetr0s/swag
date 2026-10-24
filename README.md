# Static Website Assembler and Generator (a.k.a swag)

TODO: The contents of this readme are temporary for input to start development
process. It is a working specification, not end-user documentation. A proper
public-facing README will be written later.

## Getting Started

Read my blog post [here](https://vetr0s.dev/posts/zero-dependency-static-site-generator.html)

The idea with the project is to build our own tool to replace the emacs functionality.

The workflow is going to move entirely away from emacs into a minimal terminal based utility
with targeted interop with neovim being the editor that you are authoring this website in, possible
chance to create a companion neovim plugin perhaps? However the tool at it's core should just need
a working directory and terminal. It also shouldn't be designed as a service.

A key idea here is also not over-engineering. I don't want something too complicated especially
since I will be maintaining it for my own uses.

## Implementation details

Overall, the core CLI tool is a single Go binary, built with the Go standard
library only, that turns a directory of Markdown/HTML sources into a static
site. Markdown rendering is delegated to an external program (pandoc by
default) through a configurable hook, so swag never contains a Markdown parser.

The rest of this document is the full specification: goals, design decisions,
file formats, the build pipeline, the CLI, code layout, testing, and an ordered
implementation plan. Where a choice could go several ways, a decision has been
made and stated so development does not block. Anything marked **(future)** is
explicitly out of scope for v1.

---

## 1. Goals and non-goals

### 1.1 Goals

- **One binary, stdlib only.** `go.mod` has no `require` lines. Anyone can read
  the whole codebase in an afternoon.
- **Replace the current Emacs/Org Publish setup** for vetr0s.dev without
  breaking any existing URLs.
- **Author in Markdown from neovim (or any editor).** The tool only needs a
  working directory and a terminal.
- **Conventions over configuration.** A site with the standard layout builds
  with no config file beyond the site's basic metadata.
- **Deterministic output.** The same inputs (and the same pandoc version)
  produce byte-identical output, so `git diff docs/` shows only real changes.
- **Fast enough to never think about.** Full rebuilds every time; no
  incremental build cache on disk.
- **Clear errors.** Every content error names a file and (where possible) a
  line, in a format editors can jump to.

### 1.2 Non-goals (deliberately excluded)

These are the features that make other generators complicated. Saying no to
them is a design decision, not a TODO list.

- No built-in Markdown, Org, or any other markup parser.
- No plugin system, hooks beyond the renderer hook, or scripting.
- No themes or theme registry. Layouts are just files in the site.
- No layout inheritance (`extends`/blocks). Layouts include partials; that's all.
- No taxonomies (tags, categories), pagination, archives by year, or series.
- No asset pipeline (Sass, bundling, minification, image resizing, fingerprinting).
- No i18n / multilingual sites.
- No full YAML/TOML front matter.
- No on-disk incremental build cache.
- No daemon or long-running service. `swag serve` is a foreground,
  local-only preview that exits on Ctrl-C.
- No deployment commands. Deployment stays `git push` (GitHub Pages serves
  `docs/`).

---

## 2. Runtime and toolchain requirements

- **Language:** Go, targeting the current stable release at project
  initialisation (set the `go` directive in `go.mod` accordingly).
- **Dependencies:** standard library only. CI (or a test) must assert that
  `go list -m all` prints exactly one line (the module itself).
- **External runtime tool:** `pandoc` (3.x recommended) is required only when a
  site contains `.md` files and uses the default renderer. Sites that use only
  `.html` sources need nothing but the `swag` binary.
- **Platforms:** Linux and macOS are primary. Windows should compile and work
  for `build`, but is not actively tested.
- **Stdlib packages expected to be used:** `flag`, `os`, `os/exec`,
  `os/signal`, `io/fs`, `path/filepath`, `embed`, `html/template`,
  `encoding/xml`, `net/http`, `time`, `sort`/`slices`, `strings`, `bytes`,
  `bufio`, `crypto/sha256`, `sync`, `context`, `runtime`, `runtime/debug`,
  `errors`, `fmt`.

---

## 3. Site layout (conventions)

A swag site is a directory with this structure:

```
mysite/
  swag.conf          site configuration; also marks the site root
  content/           pages and posts (Markdown or HTML fragments)
    index.md         optional: body shown above the post list on the home page
    about.md         a page -> /about.html
    posts/           everything in here is a post
      my-post.md     -> /posts/my-post.html
      my-post.png    non-content files are copied as-is (co-located assets)
  static/            copied verbatim to the output root (CSS, images, CNAME, .nojekyll)
  layouts/           optional overrides of the embedded default layouts
    default.html
    post.html
    index.html
    partials/
      head.html
      footer.html
  docs/              generated output (GitHub Pages serves this); never edited by hand
```

Only `content/` is required. `static/`, `layouts/` and `swag.conf` are
optional, though `swag.conf` is strongly recommended (it provides site
metadata and is the root marker).

### 3.1 Finding the site root

Every command first locates the site root, so swag works from any
subdirectory (e.g. when neovim's working directory is `content/posts/`):

1. If `-C <dir>` is given, start from `<dir>`; otherwise from the current
   working directory.
2. Walk up the directory tree; the first directory containing `swag.conf` is
   the root.
3. If none is found, walk up again; the first directory containing a
   `content/` directory is the root.
4. If neither is found, exit with a usage error explaining what swag looks for.

All paths in config and in output are resolved relative to the site root,
never to the current working directory.

---

## 4. Configuration: `swag.conf`

### 4.1 Format

A plain `key = value` file. Deliberately not TOML/YAML/INI.

```
# vetr0s.dev site configuration
site.title       = vetr0s.dev
site.url         = https://vetr0s.dev
site.author      = Nick Tebbs
site.description = Notes on software and tools.
site.language    = en

render.md        = pandoc --from=gfm --to=html5

params.stylesheet = /style.css
```

Rules:

- One setting per line. Blank lines are ignored. Lines whose first
  non-whitespace character is `#` are comments.
- A line is split at the **first** `=`. Key and value are trimmed of
  surrounding whitespace. Values are taken literally (no quoting, no escapes,
  no interpolation, no environment variables).
- Keys are lowercase, matching `^[a-z][a-z0-9_.-]*$`.
- Duplicate keys are an error.
- **Unknown keys are an error** (to catch typos), except keys under the
  `render.` and `params.` prefixes.
- Errors are reported as `swag.conf:<line>: <message>`.

### 4.2 Keys

| Key | Default | Meaning |
|---|---|---|
| `site.title` | name of the root directory | Site title, used in layouts and RSS |
| `site.url` | *(none)* | Absolute base URL, no trailing slash. Required for RSS and absolute links |
| `site.author` | *(none)* | Author name for layouts and RSS |
| `site.description` | *(none)* | Site description for the home page meta tag and RSS channel |
| `site.language` | `en` | `<html lang>` and RSS `<language>` |
| `build.out` | `docs` | Output directory, relative to the root |
| `serve.port` | `8080` | Default port for `swag serve` |
| `render.timeout` | `30s` | Per-file renderer timeout (Go duration syntax) |
| `render.<ext>` | `render.md = pandoc --from=gfm --to=html5` | Command that renders files with extension `<ext>` |
| `params.<name>` | *(none)* | Free-form string values exposed to layouts as `.Site.Params.<name>` |

`site.url` is normalised by stripping a trailing `/` and must start with
`http://` or `https://`.

### 4.3 Renderer commands

`render.<ext>` values are parsed into an argv list with a minimal tokenizer:
split on whitespace, with double quotes grouping a token (`"a b"` is one
argument) and `\"` inside quotes producing a literal quote. **Commands are not
run through a shell.** No pipes, globbing, or variable expansion; if someone
needs a pipeline, they write a script and point `render.<ext>` at it.

The built-in default `render.md = pandoc --from=gfm --to=html5` applies when
`swag.conf` doesn't set `render.md`. Setting `render.md =` (empty value)
removes the default, making `.md` files an error.

`render.html` cannot be set (`.html` sources are always passed through);
setting it is an error.

---

## 5. Content model

### 5.1 What counts as a content file

While walking `content/` (in sorted, deterministic order):

- Files and directories whose names start with `.` are ignored entirely
  (editor swap files, `.DS_Store`).
- Files ending in `~` or `.swp` are ignored.
- `*.html` files are **content files** rendered by passthrough.
- Files whose extension has a configured `render.<ext>` are **content files**
  rendered by that command.
- Everything else is an **asset**, copied verbatim to the same relative path
  in the output (so images can live next to the post that uses them).

Extension matching is case-insensitive; the extension key in config is
lowercase without the dot.

### 5.2 Pages vs posts

- A **post** is any content file under `content/posts/` (at any depth).
- A **page** is any other content file.
- Posts appear in `.Site.Posts`, the home page list, and RSS. Pages don't.

### 5.3 Output paths and URLs

The output path keeps the source's relative path and swaps the extension for
`.html`:

| Source | Output | `.Page.URL` |
|---|---|---|
| `content/index.md` | `docs/index.html` | `/` |
| `content/about.md` | `docs/about.html` | `/about.html` |
| `content/posts/foo.md` | `docs/posts/foo.html` | `/posts/foo.html` |
| `content/notes/index.html` | `docs/notes/index.html` | `/notes/` |

- URLs are root-relative and use `/` separators on every OS.
- `index.html` outputs get directory-style URLs (`/`, `/notes/`).
- `.html` extensions are kept on purpose: existing vetr0s.dev URLs end in
  `.html` and must not break. No "pretty URL" mode.
- Two sources mapping to the same output (`foo.md` and `foo.html`) is an
  error naming both files.
- An asset or static file colliding with a generated output is an error
  naming both.

### 5.4 Front matter

Every content file **must** begin with a front matter block:

```markdown
---
title: A zero-dependency static website generator
date: 2026-09-16
description: How this site gets built.
layout: post
---

Body starts here.
```

Grammar:

- Line 1 must be exactly `---` (trailing whitespace tolerated; a UTF-8 BOM
  before it is stripped). If not, the file is an error ("missing front
  matter").
- The block ends at the next line that is exactly `---`. Reaching end-of-file
  first is an error pointing at line 1.
- Inside, each non-blank, non-`#`-comment line is `key: value`, split at the
  **first** `:`. Key and value are trimmed. Values are literal strings: no
  quoting, no multi-line values, no lists. (`title: Go: a tour` has the title
  `Go: a tour`.)
- Keys are lowercase, matching `^[a-z][a-z0-9_-]*$`.
- Duplicate keys are an error.
- Line numbers in errors refer to the real line in the source file.
- The body is everything after the closing `---` line. One leading blank line
  is dropped. The body's starting line number is recorded so renderer errors
  can be offset correctly where possible.

Known keys:

| Key | Required | Type / format | Effect |
|---|---|---|---|
| `title` | always | non-empty string | `<title>`, `<h1>`, index list, RSS item |
| `date` | for posts | `YYYY-MM-DD` | Post sorting, display, RSS `pubDate`. Optional for pages. Any other format is an error |
| `description` | no | string | `<meta name="description">`, RSS fallback |
| `layout` | no | layout name without `.html` | Selects `layouts/<name>.html` |
| `draft` | no | `true` or `false` exactly | `true` excludes the file unless `--drafts` |

All other keys are passed through untouched in `.Page.Params` (string to
string). Unknown keys are never an error, so new fields like `updated:` need
no code changes. Known keys also appear in `.Page.Params` with their raw
string values.

Dates are calendar dates with no time zone. Internally they're a `time.Time`
at 00:00 UTC.

### 5.5 Drafts

- `draft: true` sources are skipped entirely (not rendered, not in
  `.Site.Posts`, not in RSS) unless `--drafts` is passed.
- Because drafts are skipped, a link from a published page to a draft fails
  link checking. That's intended.
- Assets are always copied whether or not their neighbouring post is a draft
  (keeping it simple; an asset can't know which post it belongs to).

---

## 6. Rendering

### 6.1 Passthrough (`.html`)

The body (after front matter) is used as-is as the page's HTML content.

### 6.2 External renderer hook (`render.<ext>`)

For each content file whose extension has a renderer:

1. Start the command (argv from §4.3) with `os/exec`, working directory set
   to the site root.
2. Write the **body only** (front matter removed) to stdin, then close stdin.
3. Read stdout as the rendered HTML fragment. Capture stderr.
4. Environment: inherit the parent environment, plus:
   - `SWAG_ROOT`: absolute site root
   - `SWAG_SOURCE`: source path relative to the root (e.g. `content/posts/foo.md`)
   - `SWAG_DRAFTS`: `1` when building with drafts, else `0`
5. Apply `render.timeout` via `context.WithTimeout`; kill the process on timeout.
6. A non-zero exit, a timeout, or a failure to start is an error:
   `content/posts/foo.md: render failed (pandoc exited 64): <first lines of stderr>`.
   If the executable isn't on `PATH`, say so explicitly and suggest installing
   pandoc or changing `render.<ext>`.
7. Empty stdout is a warning, not an error.

Non-zero renderer stderr output on success is shown only in verbose mode.

### 6.3 Parallelism

Renderer invocations run on a worker pool of `runtime.NumCPU()` goroutines.
Results are collected and then processed in sorted source-path order, so
parallelism never affects output or error ordering. When several files fail,
**all** errors are reported (sorted by path), not just the first.

### 6.4 Pandoc notes

- Default flags: `--from=gfm --to=html5`. pandoc emits a fragment (not a
  standalone document) unless `--standalone` is given; the default deliberately
  omits it.
- **Syntax highlighting is pandoc's job.** pandoc marks up fenced code blocks
  with classes; the site supplies matching CSS in `static/`. swag does nothing
  special for code. Disabling or styling highlighting is done through pandoc
  flags in `render.md` (check pandoc's documentation for the flag names in the
  installed version, as they have changed across releases).
- Other input dialects (`--from=markdown`, `--from=commonmark_x`) are a
  config change, not a code change.
- Determinism holds for a given pandoc version. A pandoc upgrade may change
  output; that's expected and acceptable.

---

## 7. Layouts

### 7.1 Concept

There is one concept exposed to users: **layouts**. A layout is a file
written in Go `html/template` syntax that produces a full HTML page around
rendered content. Partials are named fragments that layouts include.

### 7.2 Embedded defaults and overrides

The binary embeds (via `embed`) a minimal default set:

```
default.html
post.html
index.html
partials/head.html
partials/footer.html
```

Resolution is per file: if `layouts/<file>` exists in the site, it replaces
the embedded file of the same name; otherwise the embedded one is used. Users
may add new layouts and partials freely.

Default layouts must be minimal, semantic HTML with no CSS framework and no
JavaScript. `partials/head.html` includes:

- `<meta charset>`, `<meta name="viewport" content="width=device-width, initial-scale=1">`
- `<title>`: page title, plus ` - Site Title` when the page isn't the home page
- `<meta name="description">` when a description exists
- `<meta name="generator" content="swag">`
- `<link rel="stylesheet" href="...">` only when `params.stylesheet` is set
- `<link rel="alternate" type="application/rss+xml" href="/rss.xml">` only when RSS is generated

The defaults should roughly mirror the current vetr0s.dev structure: a home
link at the top, the title as `<h1>`, the date under it on posts, the body,
and a footer with a link back to all posts and the RSS link.

### 7.3 Layout selection

For each page, in order:

1. The front matter `layout` value, if set. If no such layout exists
   (site or embedded), that's an error naming the file and the layout.
2. `post` for posts.
3. `index` for `content/index.*`.
4. `default` for everything else.

### 7.4 Partials

Every file in `partials/` (site and embedded, after override resolution) is
parsed into every layout's template set as a named template. The name is the
file's base name without `.html`: `partials/nav.html` is included with
`{{template "nav" .}}`. Partials can include other partials. A layout
referencing a missing partial is a build error with the layout's filename.

Implementation note: build one base `*template.Template` containing all
partials and the function map, then `Clone()` it per layout and parse the
layout into the clone. Parse all layouts once per build, not per page.

### 7.5 Template data

Each layout executes with a single value of this shape:

```go
type TemplateData struct {
    Site *Site
    Page *Page
}

type Site struct {
    Title       string
    URL         string            // "" when unset
    Author      string
    Description string
    Language    string
    Params      map[string]string // from params.* in swag.conf
    Posts       []*Page           // published posts (drafts only with --drafts), newest first
    HasFeed     bool              // true when rss.xml is generated
}

type Page struct {
    Title       string
    Date        time.Time     // zero when unset
    Description string
    URL         string        // root-relative, e.g. "/posts/foo.html"
    Source      string        // e.g. "content/posts/foo.md"
    Content     template.HTML // rendered body, trusted, not escaped
    IsPost      bool
    IsDraft     bool
    Layout      string
    Params      map[string]string
}
```

Escaping: `Content` is `template.HTML`, so it is inserted as-is. Every other
string goes through `html/template`'s contextual auto-escaping. There are no
"safe" helper functions that bypass escaping.

**Determinism:** no build timestamp, hostname, or random value is ever
exposed to templates.

### 7.6 Template functions

Keep the function map tiny:

| Function | Example | Result |
|---|---|---|
| `date` | `{{date "2006-01-02" .Page.Date}}` | Formats a time with a Go layout string; returns `""` for the zero time |
| `absURL` | `{{absURL .Page.URL}}` | Joins `site.url` and a root-relative URL; returns the input unchanged when `site.url` is unset |

Go's built-ins (`if`, `range`, `with`, `len`, `index`, `eq`, etc.) cover the
rest.

---

## 8. Generated pages (the "generator" pass)

### 8.1 Post ordering

`.Site.Posts` is sorted by `Date` descending, ties broken by `Source`
ascending. The same order is used everywhere (home page, RSS).

### 8.2 Home page

- If `content/index.*` exists, it is rendered like any page with the `index`
  layout (unless overridden). Its body appears above the post list.
- If not, swag still generates `docs/index.html` using the `index` layout,
  with `.Page.Title` set to `site.title` and empty `Content`.
- The default `index.html` layout lists `.Site.Posts` as date + linked title.
- The home page is not added to `.Site.Posts`.

### 8.3 RSS feed

- Generated at `docs/rss.xml` when `site.url` is set **and** there is at
  least one post. If posts exist but `site.url` is unset, print a warning and
  skip the feed (`.Site.HasFeed = false`).
- RSS 2.0, built with `encoding/xml` structs (not a text template), so
  escaping is correct by construction.
- Channel: `title`, `link` (= `site.url` + `/`), `description`,
  `language`, and `lastBuildDate` = the newest post's date (never "now", for
  determinism). Include an `atom:link rel="self"` to the feed URL.
- One `<item>` per post, in post order, with no item limit:
  - `title`
  - `link` and `guid isPermaLink="true"`: absolute post URL
  - `pubDate`: post date at 00:00 UTC in RFC 1123Z format
  - `description`: the post's full rendered HTML (XML-escaped by the encoder).
    Relative URLs inside it are left as-is in v1.
- Fixed, stable indentation in output.

### 8.4 Sitemap **(future)**

`sitemap.xml` is a candidate for later and is not part of v1.

---

## 9. Static files

- Everything under `static/` is copied to the output root, keeping relative
  paths, including dotfiles that matter for hosting (`CNAME`, `.nojekyll`).
  Exception: `.DS_Store` and editor swap/backup files are skipped.
- Files are copied byte-for-byte. Output files are created `0644`,
  directories `0755`. Modification times are not preserved.
- Collisions with generated pages, content assets, `index.html` or
  `rss.xml` are errors.

---

## 10. Build pipeline (`swag build`)

Ordered steps. Any error stops before the output directory is touched.

1. **Locate root** (§3.1) and **load config** (§4). Validate every key.
2. **Validate the output directory** (§10.1).
3. **Discover** files in `content/`, `static/`, `layouts/` with sorted
   walks. Classify content files vs assets (§5.1).
4. **Parse front matter** for every content file. Collect all errors across
   all files before stopping.
5. **Filter drafts** (unless `--drafts`).
6. **Compute output paths** and detect collisions (§5.3, §9).
7. **Render bodies**: passthrough or renderer hook, in parallel (§6).
8. **Load layouts** (embedded + overrides) and partials; parse once (§7).
9. **Assemble** each page: select layout, execute with `TemplateData`.
10. **Generate** the home page (if no `content/index.*`) and `rss.xml` (§8).
11. **Write** everything (pages, generated files, assets, static files) into
    a fresh temporary directory inside the site root (`.swag-build-*`).
12. **Link check** the temporary directory (§11). Failures stop the build.
13. **Swap**: remove the old output directory and rename the temporary
    directory to it. On any failure in steps 1–12 the temporary directory is
    removed and the existing output is left exactly as it was.
14. **Report**: one summary line on stderr, e.g.
    `swag: built 14 pages, 9 posts, 23 files in 180ms -> docs/`.

### 10.1 Output directory safety

Because the output directory is replaced wholesale, refuse to build (usage
error) if `build.out` resolves to:

- the site root itself or any ancestor of it;
- anything outside the site root;
- `content/`, `static/`, `layouts/`, or any path inside them, or any of them
  being inside it.

Everything that should survive a rebuild (`CNAME`, `.nojekyll`, favicons)
must live in `static/`.

### 10.2 Determinism checklist

- Sorted directory walks everywhere (`fs.WalkDir` is lexical; don't rely on
  map iteration order anywhere output is produced).
- Parallel rendering never changes ordering (§6.3).
- No timestamps, hostnames, absolute paths, or random values in output.
- RSS `lastBuildDate` comes from content, not the clock.
- Generated files end with exactly one trailing newline.
- A test builds the same site twice and compares the trees byte-for-byte.

---

## 11. Internal link checking

Run over every generated `.html` file in the temporary output directory.

- stdlib has no HTML parser, so use a small **attribute scanner**: find tags,
  extract `href` and `src` attribute values (double-quoted, single-quoted, or
  unquoted), and decode HTML entities in them (`html.UnescapeString`). Text
  inside code blocks is already escaped (`&lt;a href...`) so it isn't seen as
  a tag. Also skip the contents of `<script>` and `<style>` elements.
- **Ignored:** URLs with a scheme other than http/https (`mailto:`, `tel:`,
  `data:`), protocol-relative `//host` URLs, and pure `#fragment` links.
- **Absolute http(s) URLs** are checked only if they start with `site.url`
  (they're internal links written absolutely); everything else external is
  ignored. swag never makes network requests.
- **Checked:** root-relative (`/posts/foo.html`) and document-relative
  (`../style.css`, `foo.png`) URLs. Strip the query string and fragment,
  percent-decode, and resolve against the page's location. A path ending in
  `/` resolves to `index.html` in that directory. The target must exist in the
  output tree.
- Fragment targets (`#section`) are not validated in v1.
- Report every broken link, sorted, as
  `docs/posts/foo.html: broken link "/posts/old.html"`, mapped back to the
  source file when known: `content/posts/foo.md: broken link "/posts/old.html"`.
- Link check failures fail `build`. In `serve` they are printed as warnings.

---

## 12. CLI

### 12.1 Commands

```
swag build [--drafts] [--out <dir>] [-v] [-q]
swag serve [--drafts] [--port <n>] [--host <addr>] [-v] [-q]
swag new   [--page] [--ext <ext>] "<title>"
swag version
swag help [command]
```

Global flag, accepted before the command: `-C <dir>` (start root discovery
from `<dir>`).

Implementation: stdlib `flag` with one `flag.FlagSet` per subcommand and a
hand-written dispatcher. No CLI framework.

### 12.2 `swag build`

Runs the pipeline in §10.

- `--drafts`: include drafts.
- `--out <dir>`: override `build.out` (same safety rules).
- `-v`: verbose. List each file written and show renderer stderr.
- `-q`: quiet. Print errors only, no summary line.

### 12.3 `swag serve`

A foreground local preview. Not a service.

- Builds into a temporary directory under `os.TempDir()`, **never** into
  `build.out`, so the committed output is never polluted with preview-only
  changes. The temp directory is removed on exit (handle SIGINT/SIGTERM).
- Serves it with `net/http` on `--host` (default `127.0.0.1`, local only)
  and `--port` (default `serve.port`, i.e. 8080). If the port is taken, fail
  with a clear message instead of picking another port.
- Prints `swag: serving http://127.0.0.1:8080 (Ctrl-C to stop)`.
- **Watching:** every 500ms, walk `content/`, `static/`, `layouts/` and stat
  `swag.conf`. Build a fingerprint from (path, size, mod time) and rebuild
  when it changes. Use polling only; don't add a filesystem notification
  dependency. Debounce: rebuild after two consecutive identical fingerprints
  following a change, so a burst of writes from an editor causes one rebuild.
- **Render cache:** within the serve process only, cache renderer output in
  memory keyed by `sha256(renderer argv + body)`. A rebuild after editing
  one post re-runs pandoc for that post only. The cache is never written to
  disk. Every rebuild still runs the full pipeline; only the renderer calls
  are cached.
- **On build failure:** keep serving the last good build, print the errors,
  and keep watching.
- **Live reload:** when serving an HTML response, the handler injects a
  small inline `<script>` just before `</body>` (or at the end if there is
  none). Injection happens in the HTTP handler only; built files never
  contain it. The script polls `GET /__swag/build` every second. That endpoint
  returns the current build number as plain text (it increments on each
  successful rebuild). When the number changes, the script calls
  `location.reload()`. Serve responses use `Cache-Control: no-store`.
- `--drafts` defaults to **off** in `serve` too, for consistency; pass it to
  preview drafts.
- Link check runs on each rebuild, reporting warnings only.

### 12.4 `swag new`

Creates a new source file with front matter, and prints its path.

- Default: a post at `content/posts/<slug>.<ext>`.
- `--page`: a page at `content/<slug>.<ext>`.
- `--ext`: source extension, default `md`. Must be `html` or have a
  configured renderer.
- Generated file:

  ```markdown
  ---
  title: <title exactly as given>
  date: <today, local time, YYYY-MM-DD>
  draft: true
  ---

  ```

  Pages get `title` and `draft` only (no `date`).
- **Slug rules:** lowercase; ASCII letters and digits kept; every run of
  other characters becomes a single `-`; leading/trailing `-` trimmed;
  non-ASCII characters are treated as separators (no transliteration). An
  empty slug is an error asking for a title with letters or digits.
- Refuse to overwrite an existing file.
- **stdout** receives only the created path relative to the current working
  directory, followed by a newline, so editors and scripts can consume it
  (e.g. `nvim "$(swag new 'My post')"`). Nothing else goes to stdout.

### 12.5 `swag version`

Prints `swag <version>`. The version comes from
`runtime/debug.ReadBuildInfo()` (module version, or VCS revision for local
builds), overridable with `-ldflags "-X main.version=..."` for releases.

### 12.6 Output streams and exit codes

- **stdout:** machine-readable results only (the path from `swag new`, the
  version string). Everything else goes to **stderr**.
- **Exit codes:** `0` success; `1` build or content error; `2` usage error
  (bad flags, unknown command, root not found, unsafe output directory).
- **Diagnostic format:** `<path>:<line>:<col>: <message>` with line and
  column omitted when unknown (`<path>:<line>: <message>` or
  `<path>: <message>`). Paths are relative to the current working directory,
  which is what editors expect. Warnings are prefixed `warning: ` after the
  location. Multiple diagnostics are sorted by path, then line.

---

## 13. Neovim interop

The core tool must work from any terminal; neovim support comes from good
CLI behaviour, not special code.

- The diagnostic format matches Vim's default `errorformat`, so
  `:set makeprg=swag\ build` plus `:make` fills the quickfix list and jumps to
  front matter errors.
- `swag new` prints only the path on stdout, so it composes with `:e` and
  shell substitution.
- Root discovery (§3.1) means commands work from inside `content/posts/`.
- `swag serve` runs in a separate terminal or a `:terminal` split; live
  reload keeps the browser in sync on `:w`.
- **(future)** A companion neovim plugin could wrap `new`/`build`/`serve`.
  If it needs structured output, add a `--format=json` flag for diagnostics
  then, not now.

---

## 14. Source code layout

```
swag/
  go.mod                      module path TBD; no require lines
  main.go                     thin: calls cli.Run(os.Args[1:], os.Stdout, os.Stderr) and exits with its code
  internal/
    cli/                      argument parsing, subcommand dispatch, exit codes, diagnostic printing
    site/                     root discovery, path helpers, output-dir safety checks
    config/                   swag.conf parser and validation; renderer argv tokenizer
    frontmatter/              front matter splitting and parsing (pure, no I/O)
    content/                  discovery/classification, Page model, output path + URL mapping, sorting
    render/                   passthrough and exec renderer, worker pool, in-memory cache (used by serve)
    layout/                   embedded defaults (embed.FS), override resolution, template parsing, func map
      defaults/               default.html, post.html, index.html, partials/*.html
    feed/                     RSS 2.0 structs and encoding
    linkcheck/                attribute scanner and link resolution
    build/                    pipeline orchestration, temp dir + atomic swap
    serve/                    HTTP server, polling watcher, live reload injection
    scaffold/                 `swag new`: slugging and file creation
    diag/                     Diagnostic type (path, line, col, severity, message), sorting, formatting
  testdata/
    sites/                    small fixture sites used by tests, each with an expected/ output tree
```

Guidelines:

- Packages communicate through plain structs and functions. No interfaces
  unless a test genuinely needs a seam (the renderer is the one expected seam).
- Pure logic (front matter, config, slugging, URL mapping, link resolution)
  has no file system access and is unit-tested directly.
- Errors carry location info as `diag.Diagnostic` values; packages return
  slices of diagnostics where several errors can occur.
- Passing `io.Writer`s for stdout/stderr into `cli.Run` keeps the CLI testable.
- `gofmt` and `go vet` clean. No global mutable state.

---

## 15. Testing strategy

- **Unit tests** for `frontmatter`, `config` (including the tokenizer),
  `scaffold` slugs, `content` URL mapping, `linkcheck` resolution, and `feed`
  output. Table-driven, covering every error case in this spec.
- **Golden-file tests:** each fixture under `testdata/sites/<name>/` contains
  a site and an `expected/` tree. The test builds into a temp dir and diffs
  against `expected/`. An `-update` test flag regenerates the golden files.
- **No pandoc in tests.** Fixtures configure a fake renderer: the test binary
  re-executes itself in a "helper process" mode (the standard
  `TestHelperProcess` / `os.Args[0]` pattern) that turns stdin into
  predictable HTML (e.g. wraps lines in `<p>`). One optional integration test
  runs real pandoc and is skipped when pandoc isn't on `PATH`.
- **Determinism test:** build each fixture twice and compare byte-for-byte.
- **Failure safety test:** an existing output directory is left untouched
  when a build fails (bad front matter, renderer failure, broken link).
- **Serve tests:** test the reload-injection handler and the build-number
  endpoint with `net/http/httptest`; test the watcher's fingerprinting
  separately. No timing-dependent end-to-end serve test.
- **Dependency test:** assert `go.mod` has no `require` directives.
- `go test ./...` must pass with nothing installed but Go.

---

## 16. Implementation plan

Build in this order. Each milestone ends with passing tests and a working
binary.

**M0: Skeleton**
- `go mod init`, `main.go`, `internal/cli` dispatcher, `help`, `version`,
  exit codes, `diag` package.
- Done when: `swag help` and `swag version` work; unknown commands exit 2.

**M1: Config, root discovery, front matter**
- `site` root discovery and `-C`; `config` parser and validation;
  argv tokenizer; `frontmatter` parser.
- Done when: unit tests cover every rule in §3.1, §4 and §5.4.

**M2: Minimal build (HTML only)**
- Content discovery and classification, URL mapping, collisions, drafts,
  embedded default layouts with overrides and partials, template data and
  functions, temp dir + safe swap, output dir safety checks, summary line.
- Done when: an `.html`-only fixture builds to its golden output, and a
  failed build leaves the previous output intact.

**M3: Renderer hook**
- `exec` renderer with stdin/stdout, env vars, timeout, error reporting,
  worker pool with ordered results, fake renderer helper for tests.
- Done when: a Markdown fixture builds using the fake renderer; renderer
  failures produce the documented diagnostics.

**M4: Generator pass**
- Post sorting, home page (with and without `content/index.*`), RSS feed.
- Done when: golden tests include `index.html` and `rss.xml`; the feed
  validates as RSS 2.0; determinism test passes.

**M5: Static files and link checking**
- `static/` and asset copying, collision detection, link checker.
- Done when: fixtures with broken links fail with sorted diagnostics mapped
  to source files.

**M6: `swag new`**
- Slugging, post/page creation, refusal to overwrite, stdout contract.

**M7: `swag serve`**
- Temp build dir, HTTP server, polling watcher with debounce, render cache,
  last-good-build behaviour, live reload injection, clean shutdown.

**M8: Polish and migration**
- Review all error messages against §12.6, `-v`/`-q`, cross-compile check.
- Migrate vetr0s.dev (§17) as the real acceptance test.

---

## 17. Migrating vetr0s.dev from Org Publish

The existing site lives in `content/` (Org), `static/`, and generated `docs/`.

1. Convert each `.org` file to Markdown with pandoc
   (`pandoc --from=org --to=gfm`), then replace the Org keywords (`#+TITLE`,
   `#+DATE`, description) with swag front matter. A small throwaway script is
   fine; it doesn't belong in swag.
2. Keep every file's relative path so output URLs stay identical
   (`content/posts/zero-dependency-static-site-generator.md` →
   `/posts/zero-dependency-static-site-generator.html`).
3. The current pages load Org's baseline stylesheet followed by the site
   stylesheet. Fold whatever is still needed into one file in `static/` and
   point `params.stylesheet` at it. Add CSS for pandoc's code highlighting
   classes.
4. Move `CNAME` (and add `.nojekyll` if needed) into `static/`.
5. Override layouts/partials as needed to match the current header, footer
   (home link, email, GitHub, LinkedIn, RSS) and date placement.
6. Build, then compare the new `docs/` URL list against the old one; every
   old URL must still exist. Check `rss.xml` in a feed reader.
7. Remove `publish.el` and `.org-timestamps/` once the new build is committed.

---

## 18. Decisions log

Kept here so future changes can see why things are the way they are.

| Decision | Choice | Why |
|---|---|---|
| Language | Go, stdlib only | Single static binary; stdlib covers templating, HTTP, XML, embed, exec |
| Markup | External renderer hook, pandoc by default | Avoids owning a Markdown parser and the CommonMark-compatibility burden |
| Front matter | `key: value` lines, not YAML | Covers every needed field without a parser dependency |
| Config | `key = value` file, strict keys | Same reasoning; strictness catches typos |
| Templating | `html/template`, layouts + partials, no inheritance | Auto-escaping for free; inheritance is where complexity grows |
| URLs | Keep `.html` | Preserves existing vetr0s.dev URLs |
| Builds | Full rebuild, atomic swap | Simple, no stale cache, failed builds can't damage `docs/` |
| Output | Deterministic | `docs/` is committed and reviewed via `git diff` |
| Highlighting | Delegated to pandoc | No lexer code in swag |
| Watching | Polling | No fsnotify dependency |
| Serve | Separate temp output, localhost only | Never pollutes `docs/`; not a service |
| Link checking | Built in, fails the build | Cheap and catches real mistakes |

## 19. Open items (defaults already chosen)

These have a working default; revisit only if they cause friction.

- **RSS content:** full post HTML in `<description>`. Could switch to
  `description` front matter only if feeds get too large.
- **Relative URLs in feed items** are left relative; feed readers usually
  handle this, but rewriting to absolute URLs could be added later.
- **`swag init`** (scaffold a new site) is not in v1; creating `content/` by
  hand is enough for one site.
- **Sitemap**, **fragment link checking**, **JSON diagnostics**, and the
  **neovim plugin** are all deferred.
- **License** and **module path**: to be decided before the first commit is
  pushed.
