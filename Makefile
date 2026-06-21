.PHONY: gen test build web dev clean

VERSION ?= dev

gen:
	@if command -v sqlc >/dev/null 2>&1; then sqlc generate; else go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate; fi

test:
	go test ./cmd/... ./internal/...
	cd web && npm run test

web:
	cd web && npm install && npm run build
	rm -rf internal/api/dist
	cp -r web/dist internal/api/dist

build: web
	CGO_ENABLED=0 go build -tags prod -ldflags "-X main.version=$(VERSION)" -o crate ./cmd/crate

dev:
	@echo "Run in two shells:"
	@echo "  1) cd web && npm run dev"
	@echo "  2) go run ./cmd/crate --dev"

clean:
	rm -rf crate web/dist internal/api/dist
