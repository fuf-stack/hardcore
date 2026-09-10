# Integration and end-to-end tests

This test-only module exercises the local library against PostgreSQL. Its local
`replace` targets the parent checkout, so tests cover unreleased changes rather
than a published version. It is not a separately released product package.
MySQL and PostgreSQL drivers are isolated here; the parent database package
remains driver-neutral and standard-library-only. MySQL is used for offline DSN
contract validation, not a MySQL server connection.

## Running

- `make test-e2e` runs the composed HTTP/database scenario only.
- `make test-integration` runs all real-database and end-to-end scenarios.

Both commands run from the repository root, use the pinned colored test runner,
and enable vet and race detection. They create a disposable PostgreSQL 18
container on a random loopback port with temporary storage, then remove it.
Docker is required only for that automatic database setup.

Alternatively, export `HARDCORE_TEST_DATABASE_URL` pointing to a PostgreSQL test
database. The script then starts or stops no containers. The tests need no
schema, migrations, privileged database role, or external credentials, and make
no persistent data changes. Never use a production database for testing.
Missing configuration is a failure when running Go tests directly, not a skip.

## Scenarios

- Driver-backed startup, a SQL query, explicit pool limits, and pool closure.
- MySQL DSN conversion and PostgreSQL URL preservation checked against the real
  driver parsers, including encoded credentials, database names, and IPv6.
- Real TCP connection loss and recovery through a test-owned proxy. Existing
  sessions are disconnected and new connections rejected while blocked; only
  the test's traffic is affected.
- Startup failure while the network path is unavailable.
- An HTTP application composed from `database`, `health`, and `service`.
  Readiness changes across outage/recovery while liveness stays available.
  A real transaction remains active during shutdown and completes before pool
  cleanup. The readiness gate closes before draining starts.

CI runs these scenarios in a separate job with read-only token permissions, a
PostgreSQL service and no private consumer repositories. The root `make check`
keeps deterministic unit coverage separate; its coverage threshold is unchanged.
Run `go mod tidy` in this directory when intentionally changing test dependencies.
