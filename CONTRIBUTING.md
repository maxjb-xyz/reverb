# Contributing to Reverb

Thanks for your interest in Reverb! This guide covers building from source,
running the test suites, and the conventions the project follows.

Please open an issue to discuss substantial changes before you start, so we can
align on approach.

## Prerequisites

- **Go 1.23+** (see `go.mod`)
- **Node.js 22+** and npm (for the `web/` SPA)
- **Docker** (optional — only needed to build/run the container image)

## Repository layout

Reverb is a **modular monolith**: a single Go binary organized around clean
adapter seams — `library` (Subsonic/Navidrome), `search` (Spotify), and
`downloader` (spotDL) — each registered explicitly at the composition root (no
`init()` side-effects). The frontend lives in `web/` and is embedded into the
binary at build time under the `prod` build tag.

## Building from source

The `Makefile` has the common targets:

```bash
make gen     # regenerate sqlc code (queries → Go)
make web     # build the SPA and copy it into internal/api/dist
make build   # build the SPA + the production binary (-tags prod) -> ./reverb
make test    # run backend + frontend tests
make clean   # remove build artifacts
```

`make build` produces a static `./reverb` binary with the SPA embedded:

```bash
CGO_ENABLED=0 go build -tags prod \
  -ldflags "-X main.version=$(git describe --tags --always)" \
  -o reverb ./cmd/reverb
```

## Running locally (dev mode)

Dev mode runs the Vite dev server for the SPA and has the Go server proxy it, so
you get hot-reloading. Run in two shells:

```bash
# shell 1: Vite dev server
cd web && npm install && npm run dev

# shell 2: Go server proxying Vite
go run ./cmd/reverb --dev
```

Then open the URL the Go server prints (default http://localhost:8090).

## Running tests

**Backend** — scope tests to the app packages. Do **not** use `./...`:
`web/node_modules` contains vendored Go that will break a bare `go test ./...`.

```bash
go test ./cmd/... ./internal/...
```

**Frontend** (from `web/`):

```bash
cd web
npm install
npm run test          # unit/component tests
npm run e2e           # end-to-end (hermetic, mocked)
```

`make test` runs the backend tests and the frontend unit tests together.

## Conventions

- **Test-driven development.** New behavior lands with tests. The git history
  shows the pattern (a `test(...)` "RED phase" commit followed by the
  implementation). Keep the suites green.
- **Conventional Commits.** Commit messages follow
  [Conventional Commits](https://www.conventionalcommits.org/), e.g.
  `feat(scope): …`, `fix(scope): …`, `test(scope): …`, `refactor(scope): …`,
  `docs(scope): …`, `style: …`, `a11y(scope): …`.
- **Adapter/seam pattern.** New adapters (library / search / downloader) register
  explicitly at the composition root and ship with tests. Avoid `init()`
  side-effects.
- **gofmt.** Go code is gofmt-clean; run `gofmt -w` (or your editor's formatter)
  before committing.
- **API is documented in OpenAPI**, served live at `/api/v1/openapi.yaml`. Keep it
  in sync when you change the HTTP API.

## Pull requests

- Keep PRs focused; describe the change and how you tested it.
- Include tests for new behavior and keep existing tests passing.

By contributing you agree that your contributions are licensed under the
project's [AGPL-3.0-only](LICENSE) license.
