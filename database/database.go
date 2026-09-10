// Package database provides driver-neutral SQL startup and readiness helpers.
// Drivers, credentials, migrations, and query models remain caller-owned.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidArgument indicates a missing context or invalid option.
	ErrInvalidArgument = errors.New("database: invalid argument")
	// ErrUnavailable indicates that no database pool was supplied.
	ErrUnavailable = errors.New("database: unavailable")
)

// PoolConfig configures database/sql pooling. Zero open connections means no
// limit; zero idle connections disables idle retention; zero durations disable
// expiry. When omitted, Open preserves database/sql defaults.
type PoolConfig struct {
	ConnMaxIdleTime time.Duration
	ConnMaxLifetime time.Duration
	MaxIdleConns    int
	MaxOpenConns    int
}

// Option configures connection startup.
type Option func(*config) error

type config struct {
	pool    *PoolConfig
	timeout time.Duration
}

// WithPoolConfig applies explicit pool settings before the startup ping.
// Negative values and idle limits above a positive open limit are rejected.
func WithPoolConfig(pool PoolConfig) Option {
	return func(cfg *config) error {
		if pool.ConnMaxIdleTime < 0 || pool.ConnMaxLifetime < 0 ||
			pool.MaxIdleConns < 0 || pool.MaxOpenConns < 0 ||
			(pool.MaxOpenConns > 0 && pool.MaxIdleConns > pool.MaxOpenConns) {
			return fmt.Errorf("%w: invalid pool settings", ErrInvalidArgument)
		}
		cfg.pool = &pool
		return nil
	}
}

// WithStartupTimeout bounds the initial ping, respecting any earlier caller
// deadline. The default is five seconds. Drivers must honor cancellation.
func WithStartupTimeout(timeout time.Duration) Option {
	return func(cfg *config) error {
		if timeout <= 0 {
			return fmt.Errorf("%w: startup timeout must be positive", ErrInvalidArgument)
		}
		cfg.timeout = timeout
		return nil
	}
}

// Open creates and validates a pool using a driver registered by the caller.
// On success the caller owns Close. Failed startup closes the pool and preserves
// both ping and cleanup errors. Driver errors may contain sensitive details:
// log them only at trusted boundaries, never return them directly over HTTP.
func Open(ctx context.Context, driverName, dataSourceName string, options ...Option) (*sql.DB, error) {
	if ctx == nil {
		return nil, ErrInvalidArgument
	}
	cfg := config{timeout: 5 * time.Second}
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidArgument
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	if p := cfg.pool; p != nil {
		db.SetMaxOpenConns(p.MaxOpenConns)
		db.SetMaxIdleConns(p.MaxIdleConns)
		db.SetConnMaxIdleTime(p.ConnMaxIdleTime)
		db.SetConnMaxLifetime(p.ConnMaxLifetime)
	}
	startupCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	err = db.PingContext(startupCtx)
	if err == nil {
		err = startupCtx.Err()
	}
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// CheckReadiness pings an existing pool without owning it. A nil pool is unready.
// The caller supplies the deadline, typically through health.Probes.
// Driver errors are for internal use, not direct HTTP responses.
func CheckReadiness(ctx context.Context, db *sql.DB) error {
	if ctx == nil {
		return ErrInvalidArgument
	}
	if db == nil {
		return ErrUnavailable
	}
	return db.PingContext(ctx)
}
