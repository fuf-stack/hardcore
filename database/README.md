# Database lifecycle

Driver-neutral helpers for `database/sql`. This package imports only the standard
library and registers no drivers. SQL dialects, credentials, migrations, ORM
clients, and application initialization belong to the caller.

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
