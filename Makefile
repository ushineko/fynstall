# fynstall — see specs/001-installer-prototype.md

default: help

.PHONY: help
help: ## Show this help
	@echo
	@echo "Available commands:"
	@echo
	@awk -F ':|##' '/^[^\t].+?:.*?##/ {printf "\033[36m%-30s\033[0m %s\n", $$1, $$NF}' $(MAKEFILE_LIST)

BINDIR=$(shell go env GOPATH)
MODULE=github.com/ushineko/fynstall

# A build that is not the release says so. Released is HEAD sitting exactly
# on this version's tag with nothing modified; anything else is
# `0.1.0-1a2b3c4-dev`. This matters more here than in most programs: the
# builder's version is the runtime version every installer it builds is
# pinned to (spec 001, R5).
TAG     := $(shell cat .tag)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
DIRTY   := $(shell git status --porcelain 2>/dev/null | head -1)
ATTAG   := $(shell git describe --exact-match --tags --match 'v$(TAG)' HEAD 2>/dev/null)
VERSION := $(if $(and $(ATTAG),$(if $(DIRTY),,x)),$(TAG),$(TAG)-$(COMMIT)-dev)
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION)
GOFLAGS := -trimpath

LINT_NAME?=golangci-lint
LINT_VERSION?=v2.12.2
LINT_PROGRAM=$(LINT_NAME)-$(LINT_VERSION)

# Release asset coordinates for the pinned linter version.
# (The upstream install.sh is not used: its checksum extraction matches the
# .sbom.json asset line and fails verification on recent releases.)
LINT_VERSION_NUM=$(LINT_VERSION:v%=%)
LINT_BASE_URL=https://github.com/golangci/golangci-lint/releases/download/$(LINT_VERSION)

.PHONY: install-lint
install-lint: $(BINDIR)/bin/$(LINT_PROGRAM) ## Install the pinned linter

$(BINDIR)/bin/$(LINT_PROGRAM):
	@echo "Setting up $(LINT_PROGRAM) ..."
	@set -e; \
	os=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
	arch=$$(uname -m); \
	case "$$arch" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; esac; \
	dist="$(LINT_NAME)-$(LINT_VERSION_NUM)-$$os-$$arch"; \
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	curl -fsSL "$(LINT_BASE_URL)/$$dist.tar.gz" -o "$$tmp/$$dist.tar.gz"; \
	curl -fsSL "$(LINT_BASE_URL)/$(LINT_NAME)-$(LINT_VERSION_NUM)-checksums.txt" -o "$$tmp/checksums.txt"; \
	want=$$(awk -v f="$$dist.tar.gz" '$$2 == f {print $$1}' "$$tmp/checksums.txt"); \
	got=$$( (sha256sum "$$tmp/$$dist.tar.gz" 2>/dev/null || shasum -a 256 "$$tmp/$$dist.tar.gz") | awk '{print $$1}'); \
	if [ -z "$$want" ] || [ "$$want" != "$$got" ]; then echo "checksum mismatch for $$dist.tar.gz: want '$$want' got '$$got'"; exit 1; fi; \
	tar -C "$$tmp" -xzf "$$tmp/$$dist.tar.gz"; \
	mkdir -p "$(BINDIR)/bin"; \
	mv -v "$$tmp/$$dist/$(LINT_NAME)" "$(BINDIR)/bin/$(LINT_PROGRAM)"

.PHONY: setup
setup: install-lint ## Set up the machine for local development
	@echo "Make sure your PATH includes $$(go env GOPATH)/bin."

# golangci-lint type-checks against the standard library sources of whichever Go
# it finds, using a go/types built into the linter binary. A linter built with
# Go 1.26 panics on a machine whose GOROOT is newer. go.mod carries no
# `toolchain` line, so the pin lives here, matching the Go that this linter
# release was built with. Bump it together with LINT_VERSION.
LINT_GO_TOOLCHAIN?=go1.26.0

# The linter's cache records file paths relative to the checkout that filled
# it. Shared between git worktrees, it reports findings against files in a
# worktree that has since been removed. One cache per checkout, ignored.
.PHONY: lint
lint: export GOTOOLCHAIN = $(LINT_GO_TOOLCHAIN)
lint: export GOLANGCI_LINT_CACHE = $(CURDIR)/.cache/golangci-lint
lint: install-lint ## Lint the module
	@go version
	$(BINDIR)/bin/$(LINT_PROGRAM) run --timeout 5m0s --config config/.golangci-$(LINT_VERSION).yml ./...

# Headless: the Fyne test driver draws into memory and needs no display.
# Nothing here writes outside temporary directories or asks for elevation.
.PHONY: test
test: ## Run the tests with the race detector
	go test -race ./...

.PHONY: vuln
vuln: ## Check dependencies for known vulnerabilities
	govulncheck ./...

.PHONY: build
build: ## Build the fynstall builder into bin/
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/fynstall ./cmd/fynstall

# The example program every test and desk check installs. Needs cgo and the
# graphics headers, as any Fyne window does.
.PHONY: hello
hello: ## Build examples/hello into examples/hello/bin/, where its fynstall.yaml looks
	go build $(GOFLAGS) -o examples/hello/bin/hello$(shell go env GOEXE) ./examples/hello

# The multi-target example, for every target its fynstall.yaml can name.
# Pure Go, so one machine builds them all.
GREET_TARGETS := linux/amd64 linux/arm64 windows/amd64

.PHONY: greet
greet: ## Build examples/greet for each target into examples/greet/build/
	@for t in $(GREET_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -o examples/greet/build/$$os-$$arch/greet$$ext ./examples/greet || exit 1; \
		echo "  examples/greet/build/$$os-$$arch/greet$$ext"; \
	done

.PHONY: beacon
beacon: ## Build examples/beacon into examples/beacon/bin/, for the actions example
	CGO_ENABLED=0 go build $(GOFLAGS) -o examples/beacon/bin/beacon$(shell go env GOEXE) ./examples/beacon

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist
