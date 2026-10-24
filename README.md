# swag

swag is the Static Website Assembler and Generator. It turns a directory of
Markdown and HTML files into a static site. It is one Go binary built from the
standard library only. Markdown goes through an external command, which is
pandoc by default, so swag has no Markdown parser of its own.

It was written to build [vetr0s.dev](https://vetr0s.dev), and that site is
built with it. The post
[Adding swag to my zero-dependency static website generator](https://vetr0s.dev/posts/adding-swag-to-my-zero-dependency-static-website-generator.html)
explains why it exists.

## Install

With Go installed:

```sh
go install github.com/vetr0s/swag@latest
```

You also need [pandoc](https://pandoc.org) to build `.md` files with the
default renderer. A site made only of `.html` files needs nothing but the
`swag` binary.

## Quick start

```sh
swag init mysite && cd mysite      # creates swag.conf and content/
nvim "$(swag new 'Hello world')"   # creates content/posts/hello-world.md as a draft
swag serve --drafts                # preview at http://127.0.0.1:8080
swag build                         # write the site to docs/
```

`swag new` prints only the path of the file it creates. That makes it work
with `$(...)` and with editors. Set `site.url` in `swag.conf` to get the RSS
feed and sitemap.

## Site layout

```
mysite/
  swag.conf          site settings; also marks the site root
  content/           pages and posts
    index.md         optional text above the post list on the home page
    about.md         a page, built to /about.html
    posts/           every content file in here is a post
      my-post.md     built to /posts/my-post.html
      my-post.png    other files are copied next to the post
  static/            copied as is to the output root (CSS, images, CNAME)
  layouts/           optional templates that replace the built-in ones
  docs/              the generated site
```

Only `content/` is required. swag finds the site root by walking up from the
current directory to the first `swag.conf`, then to the first `content/`. It
works from any subdirectory, and `-C <dir>` starts the search somewhere else.

Output paths keep the source path and end in `.html`. `content/notes/index.md`
becomes `/notes/`. Two files that produce the same output path fail the build.

## Configuration

`swag.conf` is a `key = value` file. Lines starting with `#` are comments.
Values are literal, with no quoting or variables. An unknown key is an error.

```
site.title        = vetr0s.dev
site.url          = https://vetr0s.dev
site.author       = Nathan Tebbs
site.description  = Notes on software and tools.
render.md         = pandoc --from=gfm --to=html5
params.stylesheet = /style.css
```

| Key | Default | Meaning |
|---|---|---|
| `site.title` | root directory name | Site title |
| `site.url` | none | Base URL. Needed for the RSS feed and `absURL` |
| `site.author` | none | Author name |
| `site.description` | none | Home page description and RSS channel description |
| `site.language` | `en` | `<html lang>` and RSS `<language>` |
| `build.out` | `docs` | Output directory, relative to the site root |
| `serve.port` | `8080` | Port for `swag serve` |
| `render.timeout` | `30s` | Time limit for one renderer run |
| `render.<ext>` | `render.md = pandoc --from=gfm --to=html5` | Command that renders `.<ext>` files |
| `params.<name>` | none | Any string, available to layouts as `.Site.Params.<name>` |

## Front matter

Every content file starts with a block of `key: value` lines. It is not YAML.
Each line splits at the first colon, and values are plain strings.

```markdown
---
title: Go: a tour
date: 2026-10-23
description: Shown in the meta tag and the feed.
---

The body starts here.
```

| Key | Required | Effect |
|---|---|---|
| `title` | always | Page title |
| `date` | for posts | `YYYY-MM-DD`. Orders posts and dates feed items |
| `description` | no | `<meta name="description">` |
| `layout` | no | Selects `layouts/<name>.html` |
| `draft` | no | `true` leaves the file out unless you pass `--drafts` |

Any other key is passed to layouts as `.Page.Params.<key>`.

## Rendering

An `.html` file's body is used as is. Any other extension needs a
`render.<ext>` command. swag writes the body to the command's stdin and reads
an HTML fragment from its stdout. The command runs from the site root, without
a shell, and gets three extra environment variables:

| Variable | Value |
|---|---|
| `SWAG_ROOT` | Absolute path of the site root |
| `SWAG_SOURCE` | Source path, such as `content/posts/foo.md` |
| `SWAG_DRAFTS` | `1` with `--drafts`, else `0` |

To use a pipeline or another tool, point `render.<ext>` at a script:

```
render.org = ./bin/render-org
```

Renderers run in parallel. If several files fail, swag reports all of them.
Pandoc also does syntax highlighting, and the site's CSS styles its classes.

## Layouts

Layouts are Go [`html/template`](https://pkg.go.dev/html/template) files.
swag embeds `default.html`, `post.html`, `index.html`, `partials/head.html` and
`partials/footer.html`. A file of the same name in `layouts/` replaces the
built-in one. New files add layouts and partials. A partial such as
`layouts/partials/nav.html` is included with `{{template "nav" .}}`.

A page uses its `layout` key if it has one. Otherwise posts use `post`, the
home page uses `index`, and every other page uses `default`.

Each layout receives `.Site` and `.Page`:

```
.Site.Title  .Site.URL  .Site.Author  .Site.Description  .Site.Language
.Site.Params  .Site.Posts (newest first)  .Site.HasFeed
.Page.Title  .Page.Date  .Page.Description  .Page.URL  .Page.Source
.Page.Content  .Page.IsPost  .Page.IsDraft  .Page.Layout  .Page.Params
```

Two functions are added to the built-ins:

```
{{date "January 2, 2006" .Page.Date}}   formats a date, "" when unset
{{absURL .Page.URL}}                    prefixes site.url
```

## Build

`swag build` runs the whole pipeline into a temporary directory. It replaces
the output directory only after every step passes, so a failed build leaves
the previous site in place. The output is byte-identical for the same input
and the same pandoc version, so `git diff docs/` shows only real changes.

The build also generates:

- `index.html` with the post list, when `content/index.*` does not exist.
- `rss.xml`, an RSS 2.0 feed of every post, when `site.url` is set.
- `sitemap.xml`, listing every page, when `site.url` is set.

Every `href` and `src` in the output that points inside the site must
resolve, and so must its `#fragment`, to an `id` on the target page. A broken
internal link fails the build. External links are never fetched.

swag refuses an output directory that could destroy data. That includes the
site root, anything outside it, anything overlapping `content/`, `static/` or
`layouts/`, and a non-empty directory that does not look like earlier swag
output.

## Commands

```
swag build [--drafts] [--out <dir>] [-v] [-q]
swag serve [--drafts] [--port <n>] [--host <addr>] [-v] [-q]
swag new   [--page] [--ext <ext>] "<title>"
swag init  [dir]
swag version
swag help [command]
```

`swag serve` builds into a temporary directory, never into `docs/`. It
serves on `127.0.0.1` and rebuilds about a second after a saved change. Open
pages reload by themselves. A failed rebuild prints its errors and keeps the
last good build on screen. Broken links are warnings in `serve`.

`-v` lists every file written and shows renderer stderr. `-q` prints errors
only.

Exit codes are `0` for success, `1` for a build or content error, and `2` for
a usage error. Errors go to stderr as `path:line:col: message`, with paths
relative to the current directory. Warnings carry a `warning:` prefix.

## Editor integration

The error format matches Vim's default `errorformat`, so in neovim:

```vim
:set makeprg=swag\ build
:make
```

A bad date or a broken link lands in the quickfix list:

```
content/posts/about-me.md:3: invalid date "2026-13-01": use YYYY-MM-DD
content/posts/colophon.md: broken link "/posts/old.html"
```

## Not included

swag has no plugins, themes, tags, pagination, asset pipeline, i18n or
deploy command. It has no YAML and no build cache on disk. Deployment is
committing `docs/` and pushing it, for example to GitHub Pages.

## Development

```sh
go test ./...                          # needs only Go; a fake renderer stands in for pandoc
go test ./internal/build -update       # rewrite the expected output of the fixture sites
```

The full design is in [docs/spec.md](docs/spec.md).

## License

MIT. See [LICENSE](LICENSE).
