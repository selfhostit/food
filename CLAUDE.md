# CLAUDE.md

`github.com/selfhostit/food` is a Go module shared by Troye and Shoppa: it reads Open Food
Facts (export and barcode lookups), the bundled USDA table and NEVO downloads, and has the
search helpers (normalizing, stemming, scoring). It has no database code; each app stores and
ranks foods itself.

- `gofmt`, `go vet ./...` and `go test ./...` must pass before every commit.
- Keep it dependency-light: the standard library and `golang.org/x/text` only.
- Changes reach the apps through a tag (`v0.x.y`) and a bump in their `go.mod`, so keep the
  API backwards compatible within a minor version.
- Every request to Open Food Facts sends the app's User-Agent (`Client.UserAgent`).

## Git and GitHub

- Never add AI co-author or attribution lines to commits, PR descriptions, or GitHub comments
  (no `Co-Authored-By: Claude ...`, `Claude-Session: ...`, "Generated with Claude Code", or similar).
