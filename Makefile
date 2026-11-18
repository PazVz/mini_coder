SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO ?= go
VERSION ?= dev
SQLC_VERSION := v1.31.1
SQLC := $(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

.PHONY: help run web-dev web-build web-check test-browser fmt fmt-check test test-integration vet build check dist db-up db-down migrate-up migrate-down migrate-status generate generate-check

help:
	@printf '%s\n' \
	  'make run        Start the HTTP server' \
	  'make web-dev    Start Vite on 127.0.0.1:5173 (API proxy to :8080)' \
	  'make web-build  Build Vue assets for Go embedding' \
	  'make web-check  Type-check, build, and test the Vue frontend' \
	  'make test-browser  Browser auth flow with isolated test database' \
	  'make fmt        Format Go source files' \
	  'make check      Check formatting, generated code, vet, tests, and build' \
	  'make test       Run tests with the race detector' \
	  'make test-integration  Test against MINICODER_TEST_DATABASE_URL' \
	  'make db-up      Start local PostgreSQL and wait until ready' \
	  'make db-down    Stop local PostgreSQL, preserving its volume' \
	  'make migrate-up Apply pending database migrations' \
	  'make migrate-status  Show database migration status' \
	  'make migrate-down  Roll back the last migration (may delete data)' \
	  'make generate   Generate Go queries with pinned sqlc' \
	  'make build      Build server and migration binaries in bin/' \
	  'make dist VERSION=v0.1.0  Package Linux release binaries'

run: web-build
	$(GO) run ./cmd/server

web/node_modules/.package-lock.json: web/package.json web/package-lock.json
	npm --prefix web ci
	@touch $@

web-dev: web/node_modules/.package-lock.json
	npm --prefix web run dev

web-build: web/node_modules/.package-lock.json
	npm --prefix web run build

web-check: web-build
	npm --prefix web run format-check
	npm --prefix web test

test-browser: web-build
	@test -n "$${MINICODER_TEST_DATABASE_URL:-}" || { echo 'Set MINICODER_TEST_DATABASE_URL to a test PostgreSQL database.' >&2; exit 1; }
	$(GO) test -tags=integration,browser -race -count=1 -timeout=3m -run '^TestBrowserAuthFlow$$' -v ./internal/httpapi

fmt:
	gofmt -w cmd internal web/embed.go

fmt-check:
	@unformatted="$$(gofmt -l cmd internal web/embed.go)"; \
	if [ -n "$$unformatted" ]; then \
	  printf 'Run make fmt to format these files:\n%s\n' "$$unformatted"; \
	  exit 1; \
	fi

test: web-build
	$(GO) test -race -count=1 -cover ./...

test-integration: web-build
	@test -n "$${MINICODER_TEST_DATABASE_URL:-}" || { echo 'Set MINICODER_TEST_DATABASE_URL to a test PostgreSQL database.' >&2; exit 1; }
	$(GO) test -tags=integration -race -count=1 -timeout=2m ./...

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down

migrate-up:
	$(GO) run ./cmd/migrate up

migrate-down:
	$(GO) run ./cmd/migrate down

migrate-status:
	$(GO) run ./cmd/migrate status

generate:
	$(SQLC) generate

generate-check:
	$(SQLC) diff

vet: web-build
	$(GO) vet ./...

build: web-build
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/mini-coder ./cmd/server
	$(GO) build -trimpath -o bin/mini-coder-migrate ./cmd/migrate

check: web-check fmt-check generate-check vet test test-integration build

dist:
	GO="$(GO)" bash scripts/build-release.sh "$(VERSION)"
