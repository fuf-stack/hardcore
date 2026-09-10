package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fuf-stack/hardcore/health"
)

var driverID atomic.Int64

type testDriver struct {
	closed   atomic.Int64
	ping     func(context.Context) error
	closeErr error
}
type testConn struct{ d *testDriver }

func (d *testDriver) Open(string) (driver.Conn, error) { return &testConn{d}, nil }
func (c *testConn) Close() error                       { c.d.closed.Add(1); return c.d.closeErr }
func (c *testConn) Ping(ctx context.Context) error {
	if c.d.ping != nil {
		return c.d.ping(ctx)
	}
	return nil
}
func (*testConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }
func (*testConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }

// registerDriver isolates each test's pool state without global driver mutation.
func registerDriver(d *testDriver) string {
	name := fmt.Sprintf("database-test-%d", driverID.Add(1))
	sql.Register(name, d)
	return name
}

// TestOpenConfiguresPool verifies explicit settings and caller-owned successful cleanup.
func TestOpenConfiguresPool(t *testing.T) {
	d := &testDriver{}
	db, err := Open(context.Background(), registerDriver(d), "", WithPoolConfig(PoolConfig{
		ConnMaxIdleTime: time.Minute, ConnMaxLifetime: time.Hour, MaxIdleConns: 0, MaxOpenConns: 3,
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if db.Stats().MaxOpenConnections != 3 || db.Stats().Idle != 0 {
		t.Fatalf("unexpected pool stats: %+v", db.Stats())
	}
	if d.closed.Load() != 1 {
		t.Fatal("zero idle limit did not close startup connection")
	}
}

// TestOpenPreservesDefaults ensures omitting pool settings preserves SQL's idle pool.
func TestOpenPreservesDefaults(t *testing.T) {
	db, err := Open(context.Background(), registerDriver(&testDriver{}), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if db.Stats().MaxOpenConnections != 0 || db.Stats().Idle != 1 {
		t.Fatalf("unexpected defaults: %+v", db.Stats())
	}
}

// TestOpenFailureClosesPool retains startup and close errors without leaking a connection.
func TestOpenFailureClosesPool(t *testing.T) {
	pingErr, closeErr := errors.New("ping failed"), errors.New("close failed")
	d := &testDriver{ping: func(context.Context) error { return pingErr }, closeErr: closeErr}
	db, err := Open(context.Background(), registerDriver(d), "")
	if db != nil || !errors.Is(err, pingErr) || !errors.Is(err, closeErr) {
		t.Fatalf("db=%v err=%v", db, err)
	}
	if d.closed.Load() != 1 {
		t.Fatalf("closed %d connections", d.closed.Load())
	}
}

// TestOpenTimeoutCancelsDriver enforces the deadline and releases the failed pool.
func TestOpenTimeoutCancelsDriver(t *testing.T) {
	d := &testDriver{ping: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	db, err := Open(context.Background(), registerDriver(d), "", WithStartupTimeout(time.Millisecond))
	if db != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("db=%v err=%v", db, err)
	}
	if d.closed.Load() != 1 {
		t.Fatal("timed-out connection leaked")
	}
}

// TestOpenRejectsLateSuccess prevents a driver returning nil after the deadline from opening readiness.
func TestOpenRejectsLateSuccess(t *testing.T) {
	d := &testDriver{ping: func(ctx context.Context) error { <-ctx.Done(); return nil }}
	db, err := Open(context.Background(), registerDriver(d), "", WithStartupTimeout(time.Millisecond))
	if db != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("db=%v err=%v", db, err)
	}
	if d.closed.Load() != 1 {
		t.Fatal("late startup connection leaked")
	}
}

// TestOpenHonorsCallerCancellation never replaces an earlier caller cancellation.
func TestOpenHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := Open(ctx, registerDriver(&testDriver{}), "")
	if db != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("db=%v err=%v", db, err)
	}
}

// TestOpenRejectsInvalidInput validates options before attempting to open a driver.
func TestOpenRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		options []Option
	}{
		{"nil context", nil, nil},
		{"nil option", context.Background(), []Option{nil}},
		{"zero timeout", context.Background(), []Option{WithStartupTimeout(0)}},
		{"negative idle time", context.Background(), []Option{WithPoolConfig(PoolConfig{ConnMaxIdleTime: -1})}},
		{"negative lifetime", context.Background(), []Option{WithPoolConfig(PoolConfig{ConnMaxLifetime: -1})}},
		{"negative idle count", context.Background(), []Option{WithPoolConfig(PoolConfig{MaxIdleConns: -1})}},
		{"negative open count", context.Background(), []Option{WithPoolConfig(PoolConfig{MaxOpenConns: -1})}},
		{"idle above open limit", context.Background(), []Option{WithPoolConfig(PoolConfig{MaxIdleConns: 2, MaxOpenConns: 1})}},
	}
	// Invalid settings must return the shared sentinel and no owned pool.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, err := Open(test.ctx, "not-registered", "", test.options...)
			if db != nil || !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("db=%v err=%v", db, err)
			}
		})
	}
	// Driver selection remains external and unknown drivers fail without panicking.
	t.Run("unknown driver", func(t *testing.T) {
		db, err := Open(context.Background(), "not-registered", "")
		if db != nil || err == nil {
			t.Fatalf("db=%v err=%v", db, err)
		}
	})
}

// TestReadinessRecoversAndHidesErrors composes SQL checks with health HTTP responses.
func TestReadinessRecoversAndHidesErrors(t *testing.T) {
	var offline atomic.Bool
	d := &testDriver{ping: func(ctx context.Context) error {
		if offline.Load() {
			return errors.New("private driver details")
		}
		return ctx.Err()
	}}
	db, err := Open(context.Background(), registerDriver(d), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	probes, err := health.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := probes.RegisterReadiness("database", func(ctx context.Context) error { return CheckReadiness(ctx, db) }); err != nil {
		t.Fatal(err)
	}
	probes.SetReady(true)
	// Recovery must use the same pool, not require application reconstruction.
	for _, test := range []struct {
		name    string
		offline bool
		status  int
	}{
		{"healthy", false, 200}, {"outage", true, 503}, {"recovery", false, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			offline.Store(test.offline)
			response := httptest.NewRecorder()
			probes.ReadinessHandler().ServeHTTP(response, httptest.NewRequest("GET", "/readyz", nil))
			if response.Code != test.status {
				t.Fatalf("status=%d", response.Code)
			}
			if strings.Contains(response.Body.String(), "private driver details") {
				t.Fatal("driver error leaked")
			}
		})
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckReadiness(context.Background(), db); err == nil {
		t.Fatal("closed pool was ready")
	}
}

// TestReadinessInvalidInputAndCancellation keeps nil pools and cancelled checks unready.
func TestReadinessInvalidInputAndCancellation(t *testing.T) {
	if !errors.Is(CheckReadiness(nil, nil), ErrInvalidArgument) { //nolint:staticcheck // Exercise the documented nil-context rejection.
		t.Fatal("nil context accepted")
	}
	if !errors.Is(CheckReadiness(context.Background(), nil), ErrUnavailable) {
		t.Fatal("nil pool accepted")
	}
	db, err := Open(context.Background(), registerDriver(&testDriver{}), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(CheckReadiness(ctx, db), context.Canceled) {
		t.Fatal("cancelled check succeeded")
	}
}
