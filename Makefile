.PHONY: default build check fmt fmt-check install lint lint-staged run-example \
        setup setup-go-tools test test-e2e test-integration test-race vet

GO ?= go
.DEFAULT_GOAL := default

# Default pipeline for local development.
default: check build

# Compile all library packages and the example without writing binaries.
build:
	$(GO) build ./...

# Verify formatting, static analysis, race tests, and coverage.
check: lint test-race

# Format all repository Go files without installing developer tools.
fmt:
	bash scripts/format.sh

# Fail on formatting drift without modifying files.
fmt-check:
	bash scripts/format.sh --check

# Prepare development tools and hooks, then compile the library and example.
install: setup
	bash -c 'source scripts/go-env.sh; $(MAKE) build'

# Verify formatting and run Go's static analysis.
lint: fmt-check vet

# Check staged Go formatting, then lint and test the working tree.
lint-staged:
	bash scripts/lint-staged.sh

# Run the example application locally.
run-example:
	$(GO) run ./cmd/example

# Full local bootstrap: prepare pinned tools and activate managed hooks.
setup: setup-go-tools
	@echo "Setup complete"

# Prepare development tools and configure .commitlint/hooks.
setup-go-tools:
	bash scripts/setup-go-tools.sh

# Main test entrypoint, with colored output, race detection, and coverage.
test:
	./scripts/test.sh

# Run the HTTP/database lifecycle end-to-end scenario against PostgreSQL.
test-e2e:
	bash scripts/test-integration.sh --e2e

# Run real PostgreSQL integration and end-to-end tests in the isolated test module.
test-integration:
	bash scripts/test-integration.sh --all

# Preserve the explicit race-test target used by existing workflows.
test-race:
	./scripts/test.sh

# Run focused static analysis without checking working-tree formatting.
vet:
	$(GO) vet ./...
