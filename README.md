# Hardcore

Hardcore contains the hard-to-build, product-neutral backend foundations used
across FUF applications.

Pixels are what users see. Hardcore is what keeps everything running.

Hardcore is a library, not a project starter and not a public mirror of any FUF
product. Implementations live and are released here; applications consume them
as normal Go packages.

## Packages

| Package | Responsibility |
| --- | --- |
| [`database`](./database) | Bounded SQL startup, pool configuration, and readiness checks |
| [`health`](./health) | Liveness and readiness probes with bounded dependency checks |
| [`rpc`](./rpc) | Safe Connect unary server error boundaries |
| [`service`](./service) | HTTP serving, graceful shutdown, and post-drain resource cleanup |

The foundations cover process lifecycle, health, and driver-neutral SQL pool
lifecycle and unary RPC error boundaries. Further RPC conventions, queries, authentication mechanisms, shared
contracts, and test infrastructure will be extracted only when a real consumer
gives us a concrete API to design.

## Example

```go
probes, err := health.New()
if err != nil {
	return err
}

mux := http.NewServeMux()
mux.Handle("GET /healthz", probes.LivenessHandler())
mux.Handle("GET /readyz", probes.ReadinessHandler())

server := &http.Server{
	Addr:    ":8080",
	Handler: mux,
}

probes.SetReady(true)
return service.ListenAndServe(
	ctx,
	server,
	service.WithShutdownHook(func(context.Context) error {
		probes.SetReady(false)
		return nil
	}),
)
```

A runnable version lives in [`cmd/example`](./cmd/example).

## Development

Hardcore requires the Go version declared in [`go.mod`](./go.mod).

```sh
make check
make run-example
```

`make check` verifies formatting, runs pinned repo-local golangci-lint (including
explicit `govet`), and executes the test suite
with the race detector. Tests use the version of `gotestsum` pinned in
[`tools/go.mod`](./tools/go.mod) and force colored, test-name-oriented output.
The pinned version always runs through `go run`, regardless of binaries on PATH.
The suite enforces at least 95% total statement coverage.

Real PostgreSQL integration and HTTP lifecycle tests live in
[`integration`](./integration), isolated from library dependencies. Run
`make test-integration` (all scenarios) or `make test-e2e` (HTTP lifecycle only).
These commands use a disposable database unless `HARDCORE_TEST_DATABASE_URL`
is set. CI runs the full suite separately from the unit coverage gate.

Serving failures, cancellation, and external shutdown all run shutdown hooks and
drain active HTTP requests before returning. Hooks must respect their shared
shutdown context. Readiness rechecks its gate after dependency checks finish;
a dependency check that panics reports failure without exposing panic details.

Concurrent probes share an in-flight execution of each registered readiness
check. Each probe has its own waiting deadline; the callback uses the context
of the probe that started it. A callback that ignores cancellation remains the
only execution for that check until it returns, and its late success is rejected.
Checks must honor cancellation to release their resources promptly.

The shutdown deadline is cooperative: hooks run synchronously and must return
when their context expires. A stuck hook can prevent HTTP shutdown from starting.
Go cannot forcibly interrupt arbitrary application code.

## Commit conventions

Run `make install` after cloning to prepare tools, activate hooks, and build the
library and example without installing a global example binary.
Run `make setup` (or `make setup-go-tools`) for setup alone to activate the
repository-managed hooks in `.commitlint/hooks`. Tools are pinned in
`tools/go.mod` and executed through `go run`; no global installation is needed.
Setup preserves an existing custom hook path and asks you to integrate it.

Hook scripts discover Go and `gofmt` in standard Go, Homebrew, asdf, and mise
locations when GUI clients provide a minimal PATH. They preserve existing PATH
precedence and do not source shell profiles or install tools during commits.

The pre-commit hook checks the staged versions of Go files for formatting, then
runs the repo-local linter and `make test` against the working tree (including unstaged edits). It
does not format files, stash edits, or change staging. Fix reported formatting
and review what you stage before retrying. Run it manually with `make lint-staged`.

`make fmt` applies `gofmt -s` to repository Go files, including tools and
integration tests, without installing developer tools. `make fmt-check` checks
the same files without writing and fails on formatter errors. Both `make lint`
and CI's `make check` include this gate; staged checks use the same rules.

The shared configuration in `.commitlint.yaml` validates local commits and PR
titles in CI. Scopes are optional; when present, use one of:

```text
ci, database, deps, docs, example, health, release, rpc, service, tooling
```

For example, `fix(health): handle dependency timeouts` or
`chore: update repository documentation`. Add package scopes as packages are
introduced. Release Please generates `chore(release): release hardcore …` titles.
Git-generated merge and revert messages retain commitlint's default exemptions.

To validate a proposed message without creating a commit:

```sh
printf '%s\n' 'fix(service): drain active requests' | bash scripts/commitlint.sh
```

## What belongs here?

A feature belongs in Hardcore when it:

- solves a difficult mechanism rather than a product use case;
- has at least one concrete consumer;
- can be named, documented, and tested without private consumer concepts;
- has a public contract that another application could reasonably adopt; and
- can evolve without exposing private product architecture.

Domain entities, customer integrations, product permissions, and
product-specific API operations remain in consuming applications. See
[`docs/architecture.md`](./docs/architecture.md) for the complete boundary.

## Versioning

Hardcore follows semantic versioning: major versions for breaking changes,
minor versions for backward-compatible features, and patch versions for fixes.

## License

Hardcore is licensed under the MIT License. See [`LICENSE`](./LICENSE).
