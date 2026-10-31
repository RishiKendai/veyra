BINARY  := veyra
PKG     := ./cmd/veyra
DIST    := dist
# Release version: the git tag when there is one, otherwise "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

# Arguments for `make dev`, e.g.  make dev ARGS="-t image ."   (empty = interactive menu)
ARGS ?=

.DEFAULT_GOAL := help
.PHONY: help dev test test-dev vet fmt build install dist dist-mac dist-linux dist-windows checksums clean

help: ## Show this help
	@awk 'BEGIN{FS=":.*## "} /^[a-zA-Z_-]+:.*## /{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev: ## Run from source (no build step). Example: make dev ARGS="-t image ."
	go run $(PKG) $(ARGS)

test: ## Run all tests
	go test ./... -count=1

test-dev: ## Tests with the race detector, vet and a gofmt check (what CI runs, plus -race)
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }
	go vet ./...
	go test -race ./... -count=1

vet: ## go vet
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

build: ## Build for this machine into ./veyra
	$(GOBUILD) -o $(BINARY) $(PKG)

install: ## Install into $GOBIN / $GOPATH/bin
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

dist-mac: ## Downloadables for macOS (Apple Silicon + Intel)
	@$(MAKE) --no-print-directory _pkg OS=darwin ARCH=arm64
	@$(MAKE) --no-print-directory _pkg OS=darwin ARCH=amd64

dist-linux: ## Downloadables for Linux (amd64 + arm64)
	@$(MAKE) --no-print-directory _pkg OS=linux ARCH=amd64
	@$(MAKE) --no-print-directory _pkg OS=linux ARCH=arm64

dist-windows: ## Downloadables for Windows (amd64 + arm64)
	@$(MAKE) --no-print-directory _pkg OS=windows ARCH=amd64
	@$(MAKE) --no-print-directory _pkg OS=windows ARCH=arm64

dist: clean dist-mac dist-linux dist-windows checksums ## Everything: all platforms + checksums.txt in ./dist

# One archive per platform, named veyra_<os>_<arch> to match install.sh / install.ps1 / .goreleaser.yaml.
# Internal: build and archive one OS/ARCH pair (OS and ARCH are set by the caller).
.PHONY: _pkg
_pkg:
	@rm -rf $(DIST)/stage && mkdir -p $(DIST)/stage
	@echo "  build  $(OS)/$(ARCH)"
	@GOOS=$(OS) GOARCH=$(ARCH) $(GOBUILD) -o $(DIST)/stage/$(BINARY)$(if $(filter windows,$(OS)),.exe) $(PKG)
	@cp README.md $(DIST)/stage/
ifeq ($(OS),windows)
	@cp install.ps1 install.cmd $(DIST)/stage/
	@cd $(DIST)/stage && zip -q ../$(BINARY)_$(OS)_$(ARCH).zip $(BINARY).exe install.cmd install.ps1 README.md
else
	@cp install.sh $(DIST)/stage/
	@tar -czf $(DIST)/$(BINARY)_$(OS)_$(ARCH).tar.gz -C $(DIST)/stage $(BINARY) install.sh README.md
endif
	@rm -rf $(DIST)/stage

checksums: ## Write dist/checksums.txt (SHA-256 of every archive)
	@cd $(DIST) && { command -v sha256sum >/dev/null && sha256sum $(BINARY)_* || shasum -a 256 $(BINARY)_*; } > checksums.txt
	@echo "  wrote  $(DIST)/checksums.txt"; ls -1 $(DIST)

clean: ## Remove build output
	rm -rf $(DIST) $(BINARY) $(BINARY).exe
