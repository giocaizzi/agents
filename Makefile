.PHONY: help lint test check prepush
.DEFAULT_GOAL := help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

lint: ## Lint (replace with the project's linters)
	@test -s README.md

test: ## Test (replace with the project's test runner)
	@echo "no tests yet"

check: lint test ## lint + test (the CI gate)

prepush: check ## Local gate before marking a PR ready (= check; tiered repos: light tier + touched integration tests)
