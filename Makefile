SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

GO ?= go
VERSION ?= dev

.PHONY: help run fmt fmt-check test vet build check dist

help:
	@printf '%s\n' \
	  'make run        Start the HTTP server' \
	  'make fmt        Format Go source files' \
	  'make check      Check formatting, vet, test, and build' \
	  'make test       Run tests with the race detector' \
	  'make build      Build bin/mini-coder' \
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

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/mini-coder ./cmd/server

check: fmt-check vet test build

dist:
	GO="$(GO)" bash scripts/build-release.sh "$(VERSION)"
