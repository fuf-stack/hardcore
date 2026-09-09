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
| [`health`](./health) | Liveness and readiness probes with bounded dependency checks |
| [`service`](./service) | HTTP serving, graceful shutdown, and shutdown hooks |

The initial vertical slice deliberately covers only process lifecycle and
health. RPC conventions, database access, authentication mechanisms, shared
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

`make check` verifies formatting, runs `go vet`, and executes the test suite
with the race detector. Tests use the version of `gotestsum` pinned in
[`tools/go.mod`](./tools/go.mod) and force colored, test-name-oriented output.
The pinned version always runs through `go run`, regardless of binaries on PATH.
The suite enforces at least 95% total statement coverage.

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

All Go packages currently share one module version. Public exported APIs follow
semantic versioning. Release Please prepares version PRs and GitHub releases;
Go consumers resolve published tags directly through the module path.

## License

Hardcore is licensed under the MIT License. See [`LICENSE`](./LICENSE).
