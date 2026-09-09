.PHONY: check fmt fmt-check run-example test test-race vet

GO ?= go

fmt:
	$(GO) fmt ./...

fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "The following files need gofmt:"; \
		echo "$$files"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	./scripts/test.sh

test-race:
	./scripts/test.sh

check: fmt-check vet test-race

run-example:
	$(GO) run ./cmd/example
