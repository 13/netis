# Development

Building netis, the make targets, the test suites, the design system and the generated icons and screenshots.

[← Back to README](../README.md)

- [Building](#building)
- [Make targets](#make-targets)
- [Tests](#tests)
- [Visual regression tests](#visual-regression-tests)
- [Screenshots for the docs](#screenshots-for-the-docs)
- [Design system and styleguide](#design-system-and-styleguide)
- [App icon and favicons](#app-icon-and-favicons)
- [Releases](#releases)

## Building

```sh
go tool templ generate && CGO_ENABLED=0 go build -o netis ./cmd/netis
```

(`make build` does the same; templ is pinned in `go.mod` as a tool, so nothing
needs installing first.) After editing a `.templ` file, run
`go tool templ generate` and commit the generated `_templ.go` file with it; CI
fails on drift.

## Make targets

| Target | Does |
| --- | --- |
| `make generate` | `go tool templ generate` |
| `make build` | generate, then build a static `./netis` |
| `make run` | build and run it |
| `make test` / `make race` | generate, then `go test ./...` (with `-race`) |
| `make pg` / `make pg-stop` | start / remove a throwaway Postgres on port 55432 |
| `make test-pg` | the tests against that Postgres as well as SQLite |
| `make lint` | `go vet` and staticcheck |
| `make vuln` | govulncheck |
| `make docker` | `docker build -t netis .` |
| `make e2e` | visual regression and accessibility tests in Docker (see [below](#visual-regression-tests)) |
| `make e2e-update` | regenerate the screenshot baselines after an intended UI change |
| `make screenshots` | retake the README and docs screenshots in `docs/images` (see [below](#screenshots-for-the-docs)) |

## Tests

`make test` runs the Go suite against SQLite. The store suite also runs
against Postgres when `NETIS_TEST_PG_DSN` points at a server: `make pg` starts
a throwaway one and `make test-pg` runs the suite against it as well as
SQLite. The links and images in `README.md` and `docs/` are checked by
`go test` too (`docs_test.go` at the repository root), so a renamed heading or
a moved file fails the suite.

## Visual regression tests

`e2e/` holds a Playwright suite that screenshots the dashboard, devices,
a device, a subnet grid, events, the network settings and the sign-in page
at 1440×900 and 390×844 in light and dark, and compares them with the
baselines in `e2e/__screenshots__`. The same pages get an axe accessibility
check that fails on serious or critical findings. The pages are served by
the web package's test binary (`TestE2EServe`) from fixed data
(`internal/web/testdata/e2e_seed.sql`) with the clock frozen, so relative
times and availability bars render identically on every run.

`make e2e` needs Docker only: it builds the fixture and runs the suite in the
pinned `mcr.microsoft.com/playwright` image that CI uses, so fonts and
rendering match. When a UI change is intended, run `make e2e-update`, look at
the changed images and commit them with the change. On a CI failure the
`e2e-report` artifact has the expected, actual and diff image of each page.

## Screenshots for the docs

The images in `docs/images` come from the same fixture and image as the visual
regression suite, so the data in them is fictional and they come out the same
on every run. `make screenshots` runs `e2e/screenshots/docs.spec.ts` (with
`e2e/screenshots.config.ts`) and then shrinks the PNGs to a 256-colour palette
with `pngquant`, or ImageMagick's `magick` when `pngquant` is missing. Retake
them after a UI change that shows in the README and commit them.

## Design system and styleguide

The CSS lives in `internal/web/static/app.css`: design tokens first, then
components built only from tokens, in cascade layers
(`reset, tokens, base, components, utilities, pages`). Page-specific rules sit
in `internal/web/static/pages/<page>.css` inside `@layer pages`. The design
brief is `docs/superpowers/specs/2026-09-29-netis-ui-redesign-design.md`.

`/styleguide` (signed in) renders the living style tile: palette, type, icons
and every component in the current theme. It also lists the icon picker's
choices that a device's `icon` field accepts.

## App icon and favicons

The app mark, favicons, home-screen icons and the web app manifest's icons
are drawn once in `internal/web/gen_favicons.go` and committed. After changing
the drawing, run `go run gen_favicons.go` from `internal/web` (it needs
`rsvg-convert` and ImageMagick's `magick`, at build time only). The manifest
lets a phone or desktop browser install netis as a standalone app.
`docs/images/logo.svg` is a copy of `internal/web/static/icon.svg` for the
README; copy it again after changing the mark.

## Releases

Every `v*` tag publishes a static Linux amd64 binary with a `SHA256SUMS` file
to the GitHub release, and a `linux/amd64` image to GHCR
(`ghcr.io/13/netis`).

Publishing needs this repository's Actions token to be allowed to write
packages (Settings → Actions → General → Workflow permissions → "Read and
write permissions"). Without it the release workflow builds the image and then
fails the push with `denied: permission_denied: write_package`.
