.PHONY: default build check fmt fmt-check lint run-example \
        setup setup-go-tools test test-race vet

GO ?= go
.DEFAULT_GOAL := default

# Default pipeline for local development.
default: check build

# Compile all library packages and the example without writing binaries.
build:
	$(GO) build ./...

# Verify formatting, static analysis, race tests, and coverage.
check: fmt-check lint test-race

# Format all Go code.
fmt:
	$(GO) fmt ./...

# Fail on formatting drift without modifying files.
fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "The following files need gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

# Run Go's static analysis.
lint:
	$(GO) vet ./...

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

# Preserve the explicit vet target for focused verification.
vet: lint
