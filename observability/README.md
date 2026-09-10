# Request correlation

`observability` supplies opt-in HTTP request IDs and a context-aware `slog.Handler`
wrapper. It uses only the standard library; no global logger, exporter, or sink is
installed. Existing applications are unaffected until they opt in.

```go
logger := slog.New(observability.NewLogHandler(
    slog.NewJSONHandler(os.Stdout, nil),
))
correlate, err := observability.NewRequestIDMiddleware()
if err != nil {
    return err
}
handler := correlate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    logger.InfoContext(r.Context(), "request handled")
    w.WriteHeader(http.StatusNoContent)
}))
// Supply handler to your http.Server. Logger installation remains your choice.
```

## Trust and propagation

Every request receives a fresh random ID by default, even if a client supplies
`X-Request-ID`. The ID is placed in the response header, a cloned downstream
request header, and context (`RequestID(ctx)`). The original request is unchanged.
Outgoing HTTP/RPC clients must explicitly propagate the selected ID; this package
does not instrument clients. Cross-origin browsers need consumer-owned CORS
configuration to read the response header.

To allow an ingress proxy to propagate IDs, pass its direct peer prefixes:

```go
correlate, err := observability.NewRequestIDMiddleware(
    netip.MustParsePrefix("192.0.2.10/32"),
)
```

Configure actual proxy addresses, not entire client networks. The proxy must
replace or validate incoming client IDs itself. Trust is determined solely from
`RemoteAddr` in IP:port form, never `Forwarded` or `X-Forwarded-For`. Install this
middleware **before** any middleware that rewrites `RemoteAddr` (such as RealIP).
Address-based trust is not cryptographic authentication; network access controls
must prevent clients bypassing or impersonating the trusted proxy.

Accepted incoming IDs have exactly one header value containing 1–128 ASCII
letters, digits, `.`, `_`, or `-`. Missing, duplicate, malformed, or excessive
values are replaced, not echoed or logged. IPv4-mapped peers match native IPv4
prefixes. Invalid and mapped-prefix configurations return an error. Prefixes
are copied so caller mutations cannot change the active policy.

## Logging contract

- Use `InfoContext`, `ErrorContext`, or other context-aware log methods.
- Requestless records remain unchanged. Existing log-level filtering and sink
  errors are delegated to the supplied handler.
- `request_id` is added in the active slog group. Reserve this key for the wrapper;
  applications must not add a competing attribute. For top-level correlation,
  use a logger without an active group (group individual payload attributes instead).
- No bodies, URLs, headers, credentials, or arbitrary context values are captured.
  Caller-provided attributes and error strings are not automatically redacted.
- The wrapper does not emit access logs or duplicate records. The underlying
  handler must satisfy slog's concurrency-safety contract.
- Request IDs are diagnostic metadata, not credentials, tenant identifiers,
  globally unique guarantees, or OpenTelemetry trace/span IDs.

Application middleware and handlers can deliberately replace response headers;
install the middleware once and do not overwrite the selected ID downstream.
Nil contexts/handlers passed to request processing follow standard Go contracts;
`NewLogHandler(nil)` panics immediately.

Tracing, metrics, panic recovery, error classification, and original RPC error
logging are separate concerns. When adding RPC logging, place it inside the unary
error normalizer so it can observe the original error, and avoid double logging.

`make check` covers validation, spoofing, request cloning, cancellation, logging
groups/levels, and concurrent real HTTP request-to-log correlation.
