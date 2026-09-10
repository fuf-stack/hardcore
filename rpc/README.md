# RPC error boundaries

`rpc` provides `NormalizeError` and `UnaryErrorInterceptor` for Connect unary
servers. It contains no service contracts, domain error mappings, or access policy.

```go
handler := connect.NewUnaryHandler(
    "/example.v1.EchoService/Echo",
    echo,
    connect.WithInterceptors(rpc.UnaryErrorInterceptor()),
)
```

Register the interceptor first (outermost). Inner logging interceptors can record
the original error before normalization. Nothing is logged automatically.

- Cancellation and deadline errors receive their corresponding Connect codes.
- Explicit Connect errors are trusted public responses; their wrapper text is
  removed, but their messages, details, and metadata are otherwise preserved.
- Internal and unknown Connect errors are exceptions: only their code survives,
  with the public message `internal error` and no original cause or metadata.
- Unclassified errors become internal errors without exposing their cause.

An explicit Connect status takes precedence over wrapped context errors. For
joined context errors without an explicit status, deadline takes precedence.
Applications must supply safe public messages for other explicit status codes;
wrapping a database failure as unavailable does **not** make its text safe.

Domain classification stays in the application. Return a safe Connect error for
expected failures and an ordinary error for unexpected failures. There are no
settings or environment variables. Do not use this boundary on clients.

Streaming RPCs, panic recovery, middleware errors, and protocol-level errors
outside the interceptor chain are not covered. Test the complete application's
HTTP boundary separately. Run `make check` for unit and real HTTP round-trip tests.
