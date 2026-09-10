package integration_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fuf-stack/hardcore/database"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// openPool exercises real driver startup and registers ownership cleanup.
func openPool(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), "pgx", url,
		database.WithPoolConfig(database.PoolConfig{MaxIdleConns: 1, MaxOpenConns: 2}),
		database.WithStartupTimeout(3*time.Second))
	if err != nil {
		t.Fatal("PostgreSQL startup failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

// TestIntegrationDatabaseStartup verifies real SQL execution and explicit pool limits.
func TestIntegrationDatabaseStartup(t *testing.T) {
	db := openPool(t, databaseURL(t))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var result int
	if err := db.QueryRowContext(ctx, "SELECT 42").Scan(&result); err != nil {
		t.Fatal(err)
	}
	if result != 42 || db.Stats().MaxOpenConnections != 2 {
		t.Fatal("unexpected query result or pool limit")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.CheckReadiness(ctx, db); err == nil {
		t.Fatal("closed pool was ready")
	}
}

// TestIntegrationConnectionLossRecovery severs live connections and verifies the
// same pool reconnects after the network path is restored.
func TestIntegrationConnectionLossRecovery(t *testing.T) {
	gate, url := newGate(t)
	db := openPool(t, url)
	gate.setBlocked(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := database.CheckReadiness(ctx, db)
	cancel()
	if err == nil {
		t.Fatal("disconnected database was ready")
	}
	gate.setBlocked(false)
	deadline, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	// A driver can discard a stale connection on its first retry after an outage.
	for {
		if err := database.CheckReadiness(deadline, db); err == nil {
			break
		}
		select {
		case <-deadline.Done():
			t.Fatal("pool did not recover")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestIntegrationUnavailableStartup returns no owned pool when a real endpoint is blocked.
func TestIntegrationUnavailableStartup(t *testing.T) {
	gate, url := newGate(t)
	gate.setBlocked(true)
	db, err := database.Open(context.Background(), "pgx", url, database.WithStartupTimeout(time.Second))
	if db != nil {
		_ = db.Close()
		t.Fatal("failed startup returned a pool")
	}
	if err == nil {
		t.Fatal("unavailable startup succeeded")
	}
}
