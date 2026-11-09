SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO ?= go
VERSION ?= dev
SQLC_VERSION := v1.31.1
SQLC := $(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

.PHONY: help run fmt fmt-check test test-integration vet build check dist db-up db-down migrate-up migrate-down migrate-status generate generate-check

help:
	@printf '%s\n' \
	  'make run        Start the HTTP server' \
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

run:
	$(GO) run ./cmd/server

fmt:
	gofmt -w cmd internal

fmt-check:
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
	  printf 'Run make fmt to format these files:\n%s\n' "$$unformatted"; \
	  exit 1; \
	fi

test:
	$(GO) test -race -count=1 -cover ./...

test-integration:
	@test -n "$${MINICODER_TEST_DATABASE_URL:-}" || { echo 'Set MINICODER_TEST_DATABASE_URL to a test PostgreSQL database.' >&2; exit 1; }
	$(GO) test -tags=integration -race -count=1 -timeout=2m ./internal/database/...

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

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/mini-coder ./cmd/server
	$(GO) build -trimpath -o bin/mini-coder-migrate ./cmd/migrate

check: fmt-check generate-check vet test test-integration build

dist:
	GO="$(GO)" bash scripts/build-release.sh "$(VERSION)"
