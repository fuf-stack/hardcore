package integration_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fuf-stack/hardcore/database"
	"github.com/fuf-stack/hardcore/health"
	"github.com/fuf-stack/hardcore/service"
)

// TestE2EDatabaseHTTPShutdown composes all three public packages: HTTP readiness
// reflects a real network outage and an active SQL transaction survives draining.
func TestE2EDatabaseHTTPShutdown(t *testing.T) {
	gate, url := newGate(t)
	db := openPool(t, url)
	probes, err := health.New(health.WithTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := probes.RegisterReadiness("database", func(ctx context.Context) error {
		return database.CheckReadiness(ctx, db)
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	draining := make(chan struct{})
	cleaned := make(chan struct{})
	var active atomic.Bool
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	mux := http.NewServeMux()
	mux.Handle("/healthz", probes.LivenessHandler())
	mux.Handle("/readyz", probes.ReadinessHandler())
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		active.Store(true)
		close(started)
		<-release
		var answer int
		if err := tx.QueryRowContext(r.Context(), "SELECT 42").Scan(&answer); err != nil || answer != 42 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	result := make(chan error, 1)
	t.Cleanup(func() {
		unblock()
		cancel()
		_ = server.Close()
		waitFor(t, done)
	})
	probes.SetReady(true)
	go func() {
		defer close(done)
		result <- service.Serve(ctx, server, listener,
			service.WithShutdownTimeout(3*time.Second),
			service.WithShutdownHook(func(context.Context) error {
				probes.SetReady(false)
				close(draining)
				return nil
			}),
			service.WithCleanup(func() error {
				defer close(cleaned)
				var orderingErr error
				if active.Load() {
					select {
					case <-finished:
					default:
						orderingErr = errors.New("cleanup ran before the SQL handler completed")
					}
				}
				return errors.Join(orderingErr, db.Close())
			}))
	}()
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	base := "http://" + listener.Addr().String()
	if status := httpStatus(t, client, base+"/readyz"); status != http.StatusOK {
		t.Fatalf("initial readiness: %d", status)
	}
	gate.setBlocked(true)
	if status := httpStatus(t, client, base+"/readyz"); status != http.StatusServiceUnavailable {
		t.Fatalf("outage readiness: %d", status)
	}
	if status := httpStatus(t, client, base+"/healthz"); status != http.StatusOK {
		t.Fatalf("outage liveness: %d", status)
	}
	gate.setBlocked(false)
	// Recovery uses the same application and pool after dropped sessions.
	for deadline := time.Now().Add(3 * time.Second); ; {
		if httpStatus(t, client, base+"/readyz") == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("HTTP readiness did not recover")
		}
		time.Sleep(10 * time.Millisecond)
	}
	requestResult := make(chan error, 1)
	go func() {
		response, err := client.Get(base + "/query")
		if err == nil {
			err = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = errors.New("database-backed request failed")
			}
		}
		requestResult <- err
	}()
	waitFor(t, started)
	cancel()
	waitFor(t, draining)
	if probes.CheckReadiness(context.Background()).Status != health.StatusFail {
		t.Fatal("gate stayed open during drain")
	}
	select {
	case <-cleaned:
		t.Fatal("pool closed while request was active")
	default:
	}
	unblock()
	waitFor(t, done)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-requestResult; err != nil {
		t.Fatal(err)
	}
	waitFor(t, cleaned)
	if err := database.CheckReadiness(context.Background(), db); err == nil {
		t.Fatal("pool remained open after shutdown")
	}
}

// httpStatus fully consumes responses so connection reuse does not alter test ordering.
func httpStatus(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal("HTTP probe failed")
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal("could not consume HTTP probe")
	}
	return response.StatusCode
}

// waitFor bounds synchronization so lifecycle regressions fail instead of hanging.
func waitFor(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle event timed out")
	}
}
