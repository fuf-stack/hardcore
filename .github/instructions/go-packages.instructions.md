---
applyTo: '{database,health,rpc,service}/**/*.go'
---

Treat every exported identifier and observable behavior as public API. Prefer
stdlib types, explicit ownership, and caller-controlled contexts. Check races,
cancellation, bounded shutdown, sensitive error exposure, and cleanup on every
return path. New abstractions require a demonstrated consumer use case and must
not contain private product terminology.
