# Connect v2 error boundaries

`connectv2` provides `NormalizeError` and `ServerErrorInterceptor` for servers
built on `connectrpc.com/connect/v2`. It is the Connect v2 counterpart of
[`rpc`](..), which keeps serving Connect v1. It contains no service contracts,
domain error mappings, or access policy.

```go
server := connect.NewServer(connectv2.ServerErrorInterceptor(), logging)
examplev1connect.RegisterEchoServiceHandler(server, echo)
connecthttp.Mount(mux, server)
```

Pass the interceptor first (outermost). Inner logging interceptors can record
the original error before normalization. Nothing is logged automatically.

- Cancellation and deadline errors receive their corresponding Connect codes.
- Explicit local Connect errors are trusted public responses; their wrapper
  text is removed, but their messages and details are otherwise preserved.
- Internal and unknown Connect errors are exceptions: only their code survives,
  with the public message `internal error` and no details or cause.
- Remote Connect errors, a peer's verdict returned unchanged by the handler,
  become internal errors without the peer's code, message, or details.
- Unclassified errors become internal errors without exposing their cause.

## Differences from `rpc`

Connect v2 server interceptors surround the whole call, so the boundary covers
unary and streaming RPCs alike. On streams it replaces only the final error;
messages already sent are unaffected. Connect v2 errors carry no metadata, so
response headers and trailers set by handlers pass through unchanged. Remote
errors are handled explicitly because v2 marks them; `rpc` has no such signal.

An explicit Connect status takes precedence over wrapped context errors. For
joined context errors without an explicit status, deadline takes precedence.
Applications must supply safe public messages and details for other explicit
status codes; wrapping a database failure as unavailable does **not** make its
text safe.

Domain classification stays in the application. Return a safe Connect error for
expected failures and an ordinary error for unexpected failures. There are no
settings or environment variables. Do not use this boundary on clients.

Panic recovery, HTTP middleware errors, and protocol-level errors raised before
the interceptor chain runs are not covered. Test the complete application's HTTP
boundary separately. Run `make check` for unit and real HTTP round-trip tests of
unary and server-streaming calls.
