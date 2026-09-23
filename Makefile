.PHONY: setup check build test cover lint fmt check-literals

# The TypeScript analyzer embeds Tree-sitter through cgo; a C compiler is required.
export CGO_ENABLED = 1

GOLANGCI_LINT_VERSION = v2.13.2
GOLANGCI_LINT = bin/golangci-lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  = -X github.com/jonasalessi/cdd-lint/cmd.version=$(VERSION) \
           -X github.com/jonasalessi/cdd-lint/cmd.commit=$(COMMIT) \
           -X github.com/jonasalessi/cdd-lint/cmd.date=$(DATE)

## setup: configure the local clone (installs the git hooks in .githooks)
setup:
	git config core.hooksPath .githooks
	chmod +x .githooks/*
	@echo "Git hooks installed from .githooks/"

## check: the definition of done — build, test, lint, and every Go file already gofmt-clean
check: build test lint
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then \
		echo "gofmt would change these files; run make fmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

## build: compile the cdd binary into bin/ with version info injected
build:
	go build -ldflags "$(LDFLAGS)" -o bin/cdd .

## test: run every test with the race detector on
test:
	go test ./... -race

## cover: run the tests and print per-function coverage
cover:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out

## lint: run golangci-lint (auto-installed into bin/) and the vocabulary literal check
lint: check-literals $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

$(GOLANGCI_LINT):
	curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b bin $(GOLANGCI_LINT_VERSION)

## fmt: rewrite every Go file with gofmt
fmt:
	gofmt -l -w .

## check-literals: vocabulary ids (languages, metrics, modes, formats) may only be spelled out in
## vocabulary.go and the language spec files, and language tables only in the language dirs
check-literals:
	go test ./internal/languages -count=1 -run '^TestLiterals$$'
