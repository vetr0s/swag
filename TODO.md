# TODO

Findings from the audit of 2026-10-25 at f79bcc7. The project is in good
shape. `go vet`, `gofmt` and `go test -race` are all clean. The default
layouts are covered by golden and determinism tests. The one urgent item is
1: `swag build` deletes a git clone that lives in the output directory.

Every item below was checked against the code. Items marked "reproduced"
were also run against a real binary.

## Fix first

1. **`swag build` deletes `.git` inside the output directory.** Reproduced.
   `internal/site/site.go:68` accepts any output directory that contains
   `index.html`. `internal/build/build.go:70-79` then moves it aside and
   removes it.
   - Repro: `git clone -b gh-pages <repo> docs`, then `swag build`.
     `docs/.git` and any unpushed commits are gone, and the build exits 0.
   - vetr0s.dev is not affected because its `docs/` is part of the main
     repo, not a clone.
   - Fix: make `CheckOut` refuse when `out/.git` exists. The spec already
     claims this protection in §10.1. Add a case to the safety tests.

2. **A failed restore is silent.** `internal/build/build.go:74` ignores the
   error from `os.Rename(old, o.Out)`. If both renames fail, the previous
   output is left in `<root>/.swag-build-NNN.old`. No diagnostic says where
   it is, and nothing ever cleans it up. Fix: report the restore error with
   the path of the `.old` directory.

3. **The output swap has no tests.** `internal/build/build.go:63-79` covers
   rename-aside, rollback and old-dir removal. It is the main data-safety
   path, and coverage never reaches it. Write a test that makes the second
   rename fail and asserts the old output is restored.

## Bugs

4. **Serve redirects off-site.** Reproduced. `internal/serve/serve.go:165`
   builds the redirect from the raw `r.URL.Path`, so
   `http://127.0.0.1:8080//posts` redirects to `http://posts/`. The redirect
   also drops the query string. Fix: redirect to the cleaned name plus `/`,
   and keep `RawQuery`.

5. **A symlinked directory in `static/` or `content/` fails the build.**
   Reproduced. `internal/site/site.go:100` uses `WalkDir`, which lists a
   symlinked directory as a file. `internal/build/build.go:230` then fails
   with `read ...: is a directory`, which does not name the cause. Decide
   between two fixes. Follow symlinked directories, which also needs
   `Fingerprint`. Or reject them with a diagnostic that says symlinked
   directories are not supported.

6. **The serve render cache misses files the renderer reads.**
   `internal/render/render.go:65-73` keys the cache on argv, source, drafts
   and body. A renderer like
   `pandoc --citeproc --bibliography=refs.bib` or `--lua-filter=x.lua` reads
   other files, and `internal/serve/serve.go:200-214` does not watch them.
   Editing `refs.bib` leaves every cached post stale until serve restarts.
   `swag build` is not affected. docs/spec.md:676 claims the key covers
   "everything a renderer sees". Two options: hash files that appear in argv
   into the key, or document the limit and drop the claim.

7. **`.Page.Params.updated` holds the wrong line.**
   `internal/frontmatter/frontmatter.go:68` keeps the last `updated` value
   in source order. When edits are written oldest first, a custom layout
   that reads `.Page.Params.updated` shows the oldest edit. docs/spec.md:287
   says known keys appear raw in Params, but it never says which value a
   repeated key gets. Fix: store the newest entry, or leave `updated` out of
   Params and point layouts at `.Page.Updates`.

8. **A failed `swag init` blocks the retry.**
   `internal/scaffold/scaffold.go:98-101` creates `content/` before it
   writes `swag.conf`. If the write fails, the retry refuses with "content
   already exists". `New` already cleans up on failure. Make `Init` remove
   the `content/` directory it created.

9. **Nested layout files are silently ignored.**
   `internal/layout/layout.go:109` and `:118` skip any
   `layouts/<dir>/x.html` outside `partials/`, and
   `layouts/partials/<dir>/x.html`. There is no diagnostic. README.md:152
   and docs/spec.md:407 imply every file is loaded. Fix: warn on skipped
   files, or document the flat layout rule.

10. **`--port` is not validated.** Reproduced. `swag serve --port 70000`
    exits 1 with a listen error. docs/spec.md:751 says bad flags exit 2.
    Check the 1 to 65535 range in `internal/cli/cli.go` and use
    `usageError`.

11. **Some `render.*` keys are silently ignored.**
    `internal/config/config.go:125-136` accepts `render.md.bak`, but
    `path.Ext` never yields `md.bak`, so the setting does nothing. Reject
    an extension that contains a dot.

12. **Serve can delete a build directory mid-request.**
    `internal/serve/serve.go:134-135` removes the old build while handlers
    that already called `current()` may still read from it. A CSS or image
    request that lands during a swap gets a transient 404. The page reloads
    anyway, so this is low priority. Fix: delay removal by one rebuild.

## Documentation drift

13. **The spec says `site.author` feeds RSS, but no code reads it.**
    docs/spec.md:166 says the author is used "for layouts and RSS", but
    `internal/feed/feed.go:55` never receives it and no default layout reads
    `.Site.Author`. Either add `<managingEditor>` or a meta tag, or reword
    the spec.

14. **The README leaves out a condition for rss.xml.** README.md:184 says
    rss.xml is written when `site.url` is set. `internal/build/build.go:121`
    also requires at least one post. docs/spec.md:494 has the right rule.

15. **The README and spec describe the wrong condition for the generated
    home page.** README.md:183 and docs/spec.md:485 say it is generated
    "when `content/index.*` does not exist". The real rule in
    `internal/build/build.go:110-116` is "when no published index page
    exists". A draft `index.md` without `--drafts` still gets the generated
    page.

16. **The spec describes a version fallback that cannot happen.**
    docs/spec.md:742 says a build without VCS info "falls back to the
    revision setting". `internal/cli/cli.go:134-148` only uses the revision
    when VCS info exists. Reword the spec or delete the dead branch.

17. **The spec's build step order differs from the code.**
    docs/spec.md:541-549 has layouts loading at step 8. In
    `internal/build/build.go:86-173` they load before the collision checks
    and before rendering. Only the order of error reports changes. Update
    the list.

18. **The scaffold package comment is out of date.**
    `internal/scaffold/scaffold.go:1` says the package is for `swag new`.
    It also holds `Init`.

## Code health

19. **`build.Generate` does five jobs.** `internal/build/build.go:84-254`
    runs to 171 lines: discover, claim outputs, render, execute and write,
    then link check. Four `HasErrors` gates separate the jobs. The next
    pipeline step will make it longer. Split it along those gates when the
    next feature touches it.

20. **The `updated` parsing should be its own function.**
    `internal/content/content.go:152-171` builds an `Update` at :159 and
    then overwrites it at :165, with nested `err` and `cerr`. Extract a
    `parseUpdate(v frontmatter.Value) (Update, []diag.Diagnostic)`.

21. **Duplicated rules that will drift.**
    - The source-dir list `content`, `static`, `layouts` appears in
      `internal/site/site.go:48` and `internal/serve/serve.go:203`.
      `keepDot` for `static` appears in `internal/serve/serve.go:204` and
      `internal/build/build.go:88`. Define both once in `site`.
    - The "no renderer" check differs between
      `internal/content/content.go:92-99` and
      `internal/scaffold/scaffold.go:41-43`. A missing key is an asset in
      one and an error in the other, and the messages differ.
    - The "built N pages" summary appears in `internal/cli/cli.go:240` and
      `internal/serve/serve.go:138`.
    - `internal/layout/defaults/post.html:13-17` duplicates a whole `<p>` to
      change the date format. A single `{{if .HasTime}}` around the
      `datetime` and format strings would remove it.

22. **`render` imports `content` for one helper.**
    `internal/render/render.go:122` calls `content.Ext`. `Job` could carry
    the extension instead.

23. **Output handling is split between `cli` and `serve`.**
    `serve.Options` has `Log`, `Quiet`, `Stderr` and `Report`
    (`internal/serve/serve.go:36-39`), and serve writes to `Stderr`
    directly at :71, :126 and :138. Any change to `-q` behavior has to be
    checked in both packages.

## Tests and tooling

24. **`internal/build` tests take 20s under `-race`.** Each fake-renderer
    child is a race-built re-exec that sleeps 1s at exit. Measured:
    `GORACE=atexit_sleep_ms=0 go test -race ./internal/build` runs in 1.1s.
    Set it in the helper process env in `internal/build/build_test.go`
    `options()`, and in `internal/render/render_test.go`.

25. **Untested paths.**
    - `serve.Run` at `internal/serve/serve.go:53-95`: port-in-use message,
      debounce loop and shutdown. The debounce could be pulled out and
      tested without timing.
    - `internal/cli/cli.go:256-278`: everything in `serve` after flag
      parsing.
    - The `-v` output at `internal/cli/cli.go:204` and
      `internal/build/build.go:181`.
    - `internal/cli/cli.go:137-152`: the `resolveVersion` revision and
      dirty branches.
    - `internal/content/content.go:55,59,68,77`: the missing-`content/`,
      walk-error and disabled-extension paths in `Discover`.
    - `internal/layout/layout.go:157-165`: the `locate` fallbacks.

26. **`go.mod` demands Go 1.27.3.** The newest API in use is
    `sync.WaitGroup.Go`, from Go 1.25. A user on 1.25 through 1.27.2 has to
    download a newer toolchain, and with `GOTOOLCHAIN=local` the install
    fails. Lower the directive to `go 1.25` and confirm with that toolchain.

27. **No release tags.** `go install ...@latest` resolves a pseudo-version.
    Tag `v0.1.0` when the items above are done.

28. **`.gitignore` misses coverage output and `.DS_Store`.** Add `*.out`
    and `.DS_Store`.

## Leave alone

- The temp-dir build with rename swap. It needs only item 1 and item 2.
- `site.CheckOut`'s symlink, case-folding and overlap guards.
- The `diag` format and its Vim compatibility.
- The render worker pool, the cache key design and `WaitDelay`.
- `feed` building XML with `encoding/xml` structs.
- The strict front-matter and config parsers. Every error carries a line
  number, and neither parser does I/O.
- Serve path handling. `path.Clean` and the `..` guard block traversal.
  Reload injection applies only to `.html` files.
- Zero dependencies, enforced by `main_test.go`.

Out of scope for this audit: `testdata/` golden output, `docs/audit-*.html`,
swag.nvim, and Windows behavior.
