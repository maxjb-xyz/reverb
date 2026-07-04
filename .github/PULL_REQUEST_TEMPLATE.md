## What & why

Describe the change and the motivation. Link any related issue (e.g. `Closes #123`).

## How it was tested

- [ ] `go test ./cmd/... ./internal/...`
- [ ] `cd web && npm run test`
- [ ] `cd web && npm run e2e` (if UI/behavior changed)
- [ ] Manually verified in dev mode (`go run ./cmd/reverb --dev`)

Describe anything reviewers should manually check.

## Checklist

- [ ] Commits follow [Conventional Commits](https://www.conventionalcommits.org/)
- [ ] New behavior is covered by tests (TDD)
- [ ] Go code is gofmt-clean
- [ ] OpenAPI (`/api/v1/openapi.yaml`) updated if the HTTP API changed
- [ ] No secrets committed
