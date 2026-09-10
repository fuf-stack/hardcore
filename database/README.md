# Database lifecycle

Driver-neutral helpers for `database/sql`. This package imports only the standard
library and registers no drivers. SQL dialects, credentials, migrations, ORM
clients, and application initialization belong to the caller. URL parsing can
identify a database family without selecting or importing its SQL driver.

## Connection URLs

`ParseURL(raw)` returns a `Connection` with `Dialect()` and an explicit,
sensitive `DataSourceName()` accessor. Map the dialect to your registered driver
and pass the DSN to `Open`. The existing `Open` API still accepts native driver
DSNs for configurations outside the URL parser's supported subset.

- MySQL: `mysql://` TCP URLs, a single DNS/IPv4/bracketed IPv6 host, optional port
  (default 3306), URL credentials, and an escaped database name. Conversion follows
  go-sql-driver/mysql's DSN grammar (database-name escaping requires v1.8 or newer).
  Explicit query options survive; `loc` and `parseTime` defaults are caller policy.
  Colons in usernames and passwords without usernames are not representable and
  are rejected. Arbitrary `mysql+...` aliases are not accepted.
- PostgreSQL: `postgres://` and `postgresql://` URIs retain their original bytes.
  Single-host and hostless URIs are supported; native multi-host configurations
  can bypass this parser through `Open`.
- SQLite: `file:` URIs retain their options and paths. `sqlite://relative.db`
  becomes `file:relative.db`; `sqlite:///absolute.db` becomes `file:/absolute.db`.
  Relative paths, read-only options, and memory modes are preserved. Remote file
  authorities are rejected; empty authorities and `localhost` are supported.

Parsing never reads environment variables, opens connections, resolves relative
paths, creates directories, or enables WAL/foreign keys. Driver-specific option
values still require driver validation. Malformed escapes, duplicate query keys,
fragments, control bytes, and invalid ports are rejected rather than silently
normalized. Catch `ErrInvalidURL` and `ErrUnsupportedScheme` using `errors.Is`.
These errors never echo input or wrap raw parser errors.

`Connection` formatting (including `%#v`), `slog`, text, and JSON encoding are
redacted, including for the zero value. Calling `DataSourceName()` intentionally
crosses that boundary: do not log its return value. Driver errors from `Open`
remain potentially sensitive. A zero `Connection` is not usable for opening a DB.

```go
connection, err := database.ParseURL(rawURL)
if err != nil {
    return err
}
if connection.Dialect() != database.PostgreSQL {
    return errors.New("unsupported database for this application")
}
db, err := database.Open(ctx, "pgx", connection.DataSourceName())
```

Contract tests compare conversion with real MySQL and PostgreSQL driver parsers
in the isolated integration module. The base package remains standard-library-only.

## API

- `CheckReadiness(ctx, db)` pings a pool; nil and closed pools are unready. Supply
  a bounded context, for example from `health.Probes`. Never expose raw driver
  errors in public responses.
- `Open(ctx, driverName, dataSourceName, options...)` creates a pool and validates
  connectivity. A failed ping closes the pool and preserves cleanup errors.
- `WithPoolConfig(PoolConfig{...})` explicitly sets idle/open limits and connection
  lifetimes. Omitting it preserves SQL defaults. Within an explicit configuration,
  zero idle connections disables retention, zero open connections means unlimited,
  and zero durations disable expiration. Negative or contradictory limits fail.
- `WithStartupTimeout(duration)` changes the five-second initial ping deadline.
  An earlier caller deadline wins. Drivers must respect cancellation.

## Composition

Register your chosen SQL driver in the application before calling `Open`.

```go
db, err := database.Open(ctx, driverName, dataSourceName,
    database.WithPoolConfig(database.PoolConfig{
        MaxIdleConns: 5,
        MaxOpenConns: 20,
    }),
)
if err != nil {
    return err
}
if err := probes.RegisterReadiness("database", func(ctx context.Context) error {
    return database.CheckReadiness(ctx, db)
}); err != nil {
    return errors.Join(err, db.Close())
}
return service.ListenAndServe(ctx, server,
    service.WithShutdownHook(func(context.Context) error {
        probes.SetReady(false)
        return nil
    }),
    service.WithCleanup(db.Close),
)
```

The process opens its readiness gate only after all initialization succeeds.
Until serving takes ownership, the caller must close the pool on later startup
failures. If an ORM owns the same pool, register its close function instead of
closing both owners. Cleanup after a drain timeout follows forced connection
closure; it cannot wait forever for handlers that ignore cancellation.

Tests use isolated SQL drivers to verify defaults, validation, deadlines, failed
startup cleanup, readiness recovery, and safe composition with health responses.
Run `make check` for race tests and the repository-wide coverage gate.
