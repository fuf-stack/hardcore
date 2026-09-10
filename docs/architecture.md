# Architecture and extraction boundary

Hardcore is where FUF applications collaborate on difficult backend mechanisms
without publishing their product architecture. A consumer should be able to
replace Hardcore with an equivalent implementation without changing its domain
model.

## Current package direction

```text
cmd/example
  ├── health
  └── service

database    health    service
   │          │         │
   └──────────┴─────────┴── Go standard library

rpc ── Connect ── Protobuf
```

`database`, `health`, and `service` are independent foundations. The example application
composes them by using a service shutdown hook to close its readiness gate.
No production package depends on another foundation package.

`rpc` independently depends on Connect and its Protobuf dependency. This is the
first third-party runtime dependency: the public contract uses Connect errors
and interceptors directly rather than introducing a parallel status abstraction.
The other foundations retain their standard-library-only imports. Consumers
that do not import `rpc` do not link Connect into their binaries.

The concrete extraction is a unary error boundary: repeated transport handlers
were wrapping unexpected storage and authorization-check failures with raw error
messages. Normalization removes internal diagnostics while leaving domain status
classification in the consumer. Consumer adoption follows the package release;
never commit a temporary workspace override to make an unreleased API build.

Database startup validates connectivity under a deadline and closes failed
pools. Readiness pings compose with health checks through a caller-owned closure.
Post-drain cleanup belongs to service, not database: it accepts arbitrary close
callbacks and runs them in reverse order, including on bind failure. Applications
select drivers, configure credentials, and retain ORM and migration ownership.

The Go packages share one module version. This keeps compatibility and release
work understandable while the library is young. Multiple modules are justified
only when consumers genuinely need independent release cadences.

The separate `integration` module is test infrastructure, not a versioned
foundation. It depends on the parent checkout and a PostgreSQL driver to test
real connectivity and composed HTTP lifecycle behavior without adding driver
dependencies to the library. CI exercises it independently of private consumers.

## Candidate extraction areas

These are directions, not promised packages:

- ConnectRPC server/client conventions, interceptors, errors, batching,
  filtering, and cursor pagination;
- database transactions, dialect behavior, pagination,
  migrations, and integration-test infrastructure;
- authentication principals and authorization interfaces that do not encode
  product permissions;
- shared Protobuf contracts and deterministic generation;
- reusable server, database, and client test harnesses.

Each area starts with an implementation already needed by a real consumer. The
first change should normally adapt that consumer to use the extracted API so we
can prove the boundary works.

## Explicitly private

Hardcore must not contain:

- consumer-specific domain entities;
- a consuming application's database schema;
- editor and runtime behavior;
- product roles, permission names, or access-control policy;
- customer-specific integrations and deployment configuration;
- commercial rules, feature entitlements, or private operational data; or
- product-specific RPC services and HTTP operations.

Small generic types can still reveal a private design when assembled together.
Review the combined architecture, examples, fixtures, and documentation—not
only individual identifiers—before publishing an extraction.
## Go 1.27 and JSON

The module requires Go 1.27. Handwritten serialization uses `encoding/json/v2`.
Health responses retain their newline and existing omission behavior.

Generated code and framework-owned serialization remain unchanged. This is not
a claim that every third-party HTTP or RPC encoder uses the same JSON options.
