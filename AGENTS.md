# Repository instructions

Hardcore is a public Go library of product-neutral backend foundations. It is
not a starter and must not become a public copy of a consuming application's
architecture.

## Architecture

- `health` owns HTTP liveness/readiness behavior and dependency checks.
- `service` owns process-serving and graceful-shutdown behavior.
- `cmd/example` demonstrates composition and may depend on every public package.
- Public library packages must not depend on `cmd`, examples, or a consumer.
- Keep foundation packages independent unless a dependency creates a clear,
  durable contract. Composition in the consumer is preferred over hidden
  coupling between packages.

Changing dependency direction is an architecture change. Explain it in the pull
request and update `docs/architecture.md` in the same change.

## Extraction boundary

- Begin with a concrete problem observed in a consuming application.
- Extract the mechanism, not the product terminology or domain model.
- Do not introduce consumer domain entities, product roles, customer
  integrations, commercial rules, or product-specific RPC operations.
- Do not add a generalized abstraction for a hypothetical future requirement.
- A proposed package must be independently explainable and testable.

## Public API and releases

- Every exported identifier and documented runtime behavior is public API.
- Prefer the standard library and small interfaces owned by the consumer.
- Preserve API behavior unless a breaking release and migration are explicit.
- Use conventional commits. Release Please owns normal version and changelog
  updates; do not hand-edit them for routine changes.
- Keep the repository as one Go module until independently versioned modules
  solve an observed release problem.

## Verification

- Run focused tests while iterating and `make check` before review.
- Run the normal suite through `make test` so the pinned, colored `gotestsum`
  output remains consistent locally and in CI.
- Tests must cover cancellation, timeouts, error paths, and concurrency when the
  changed code participates in service lifecycle.
- Keep total statement coverage at or above the threshold enforced by
  `scripts/test.sh`; do not exclude packages or production files to raise it.
- Do not weaken tests or checks merely to make a change pass.
- Run `go mod tidy` after dependency changes and commit genuine module changes.
- Update documentation and examples when public behavior changes.
- Generated code must have an explicit generation command and a CI cleanliness
  check before it is committed.

## AI-assisted work

- Read the nearest `AGENTS.md` before editing.
- Treat generated suggestions as untrusted until tests and public API review
  confirm them.
- Record material AI assistance and manual verification in the pull request.
- Never expose credentials, private fixtures, customer data, or private product
  source in prompts, tests, examples, issues, or generated documentation.
