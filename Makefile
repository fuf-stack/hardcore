.PHONY: default build check fmt fmt-check lint lint-staged run-example \
        setup setup-go-tools test test-race vet

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

# Preserve the explicit race-test target used by existing workflows.
test-race:
	./scripts/test.sh

# Run focused static analysis without checking working-tree formatting.
vet:
	$(GO) vet ./...
