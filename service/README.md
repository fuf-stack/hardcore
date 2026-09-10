# HTTP lifecycle

`ListenAndServe` binds an address; `Serve` accepts a listener. Both drain active
requests on cancellation, serving failure, or external shutdown.

- `WithCleanup(func() error)` registers resource release after draining, or after
  forced connection closure on timeout. It also runs on bind failure. Callbacks
  run once per serving invocation in reverse registration order; all returned
  errors are preserved. They run synchronously without a timeout and must return
  promptly. Invalid arguments or options do not transfer ownership or run cleanup.
- `WithShutdownHook` runs before draining, in registration order. Use it to close
  readiness and stop new background work.
- `WithShutdownTimeout` bounds hooks and HTTP draining together (ten seconds by
  default). Hooks and handlers must honor cancellation; Go cannot forcibly stop
  callbacks. On a drain timeout, connections are force-closed before cleanup.

For example, pass `WithCleanup(db.Close)` for a caller-owned pool. Register the
ORM's close callback instead when it owns that pool. Never register both.
Dependencies initialized before calling this API remain the caller's
responsibility if option validation fails or another startup step fails.

Tests cover bind failure, cancellation, hook errors, active-request draining,
forced closure, resource ordering, and error preservation. Run `make check`.
