# agents: Universal AI Agent Harness Manager
GOLANGCI := go tool -modfile=golangci-lint.mod golangci-lint
LINT_GOOS ?= linux darwin windows

.PHONY: help fmt lint vuln test check check-fast build install readme-images

help: ## List targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "%-14s %s\n", $$1, $$2}'

fmt: ## Format, tidy and apply modernizers
	go mod tidy
	go fix ./...
	$(GOLANGCI) fmt

lint: ## Tidy check, then format check and lint per target OS
	go mod tidy -diff
	@bin=$$(go tool -n -modfile=golangci-lint.mod golangci-lint) && \
	for os in $(LINT_GOOS); do echo "golangci-lint run (GOOS=$$os)"; GOOS=$$os $$bin run || exit 1; done

vuln: ## Reachable known vulnerabilities (needs the network)
	go tool govulncheck ./...

test: ## Unit tests with the race detector, in random order
	go test -race -shuffle=on -coverprofile=coverage.out ./...

check: lint test ## lint + test

check-fast: ## Tests without the race detector
	go test -shuffle=on ./...

build: ## Static binary into bin/
	CGO_ENABLED=0 go build -trimpath -o bin/agents ./cmd/agents

install: build ## Put the binary on PATH (~/.local/bin) with chezmoi plugin symlink
	install -d $(HOME)/.local/bin
	install bin/agents $(HOME)/.local/bin/agents
	ln -sf $(HOME)/.local/bin/agents $(HOME)/.local/bin/chezmoi-agents

readme-images: ## Frame raw screenshots with macOS canvas styling for README
	@if [ -f scripts/readme-images.mjs ]; then node scripts/readme-images.mjs; fi
