package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fuf-stack/hardcore/health"
)

// TestLivenessAlwaysPasses verifies that liveness reflects the running process
// and does not depend on whether startup or dependency checks have completed.
func TestLivenessAlwaysPasses(t *testing.T) {
	probes := newProbes(t)
	response := request(t, probes.LivenessHandler(), http.MethodGet)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	assertReport(t, response, health.Report{Status: health.StatusPass})
}

// TestReadinessGateStartsClosed protects the safe default: a newly created
// service must not receive traffic before it explicitly completes startup.
func TestReadinessGateStartsClosed(t *testing.T) {
	probes := newProbes(t)
	response := request(t, probes.ReadinessHandler(), http.MethodGet)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	assertReport(t, response, health.Report{Status: health.StatusFail})
}

// TestReadinessPassesWithOpenGateAndNoChecks verifies that opening the process
// gate is sufficient when a service has no external readiness dependencies.
func TestReadinessPassesWithOpenGateAndNoChecks(t *testing.T) {
	probes := newProbes(t)
	probes.SetReady(true)
	response := request(t, probes.ReadinessHandler(), http.MethodGet)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	assertReport(t, response, health.Report{Status: health.StatusPass})
}

// TestReadinessChecks verifies dependency aggregation and ensures that a
// failing check changes readiness without exposing its internal error details.
func TestReadinessChecks(t *testing.T) {
	probes := newProbes(t)
	probes.SetReady(true)
	if err := probes.RegisterReadiness("database", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("register passing check: %v", err)
	}
	if err := probes.RegisterReadiness("queue", func(context.Context) error {
		return errors.New("contains a private connection detail")
	}); err != nil {
		t.Fatalf("register failing check: %v", err)
	}

	response := request(t, probes.ReadinessHandler(), http.MethodGet)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	assertReport(t, response, health.Report{
		Status: health.StatusFail,
		Checks: map[string]health.Status{
			"database": health.StatusPass,
			"queue":    health.StatusFail,
		},
	})
	if got := response.Body.String(); strings.Contains(got, "private connection detail") {
		t.Fatalf("response exposed check error: %s", got)
	}
}

// TestReadinessCheckTimeout verifies that a blocked dependency cannot leave the
// readiness endpoint hanging beyond the configured total check deadline.
func TestReadinessCheckTimeout(t *testing.T) {
	probes, err := health.New(health.WithTimeout(10 * time.Millisecond))
	if err != nil {
		t.Fatalf("new probes: %v", err)
	}
	probes.SetReady(true)
	if err := probes.RegisterReadiness("blocked", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("register check: %v", err)
	}

	response := request(t, probes.ReadinessHandler(), http.MethodGet)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	assertReport(t, response, health.Report{
		Status: health.StatusFail,
		Checks: map[string]health.Status{"blocked": health.StatusFail},
	})
}

// TestRegisterReadinessRejectsInvalidChecks protects check identity by rejecting
// empty names, nil functions, and duplicate registrations.
func TestRegisterReadinessRejectsInvalidChecks(t *testing.T) {
	probes := newProbes(t)
	check := func(context.Context) error { return nil }

	if err := probes.RegisterReadiness("", check); !errors.Is(err, health.ErrInvalidCheck) {
		t.Fatalf("empty name error = %v, want ErrInvalidCheck", err)
	}
	if err := probes.RegisterReadiness("database", nil); !errors.Is(err, health.ErrInvalidCheck) {
		t.Fatalf("nil check error = %v, want ErrInvalidCheck", err)
	}
	if err := probes.RegisterReadiness("database", check); err != nil {
		t.Fatalf("register check: %v", err)
	}
	if err := probes.RegisterReadiness("database", check); !errors.Is(err, health.ErrDuplicateCheck) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateCheck", err)
	}
}

// TestNewRejectsInvalidOptions verifies that invalid configuration fails during
// construction instead of producing probes with ambiguous runtime behavior.
func TestNewRejectsInvalidOptions(t *testing.T) {
	if _, err := health.New(health.WithTimeout(0)); !errors.Is(err, health.ErrInvalidTimeout) {
		t.Fatalf("timeout error = %v, want ErrInvalidTimeout", err)
	}
	if _, err := health.New(nil); !errors.Is(err, health.ErrInvalidCheck) {
		t.Fatalf("nil option error = %v, want ErrInvalidCheck", err)
	}
}

// TestProbeRejectsUnsupportedMethod verifies that probe handlers expose only
// their documented read-only HTTP methods and advertise the allowed methods.
func TestProbeRejectsUnsupportedMethod(t *testing.T) {
	probes := newProbes(t)
	response := request(t, probes.LivenessHandler(), http.MethodPost)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want %q", got, "GET, HEAD")
	}
}

// TestProbeSupportsHeadWithoutResponseBody verifies standards-compliant HEAD
// behavior while preserving the same status result as a GET request.
func TestProbeSupportsHeadWithoutResponseBody(t *testing.T) {
	probes := newProbes(t)
	response := request(t, probes.LivenessHandler(), http.MethodHead)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", response.Body.String())
	}
}

// TestReadinessGateClosesDuringCheck protects shutdown from a dependency result
// that succeeds after the application has withdrawn readiness.
func TestReadinessGateClosesDuringCheck(t *testing.T) {
	probes := newProbes(t)
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseCheck := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCheck()
	if err := probes.RegisterReadiness("dependency", func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	probes.SetReady(true)
	result := make(chan health.Report, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { result <- probes.CheckReadiness(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("check did not start")
	}
	probes.SetReady(false)
	releaseCheck()
	select {
	case report := <-result:
		if report.Status != health.StatusFail {
			t.Fatalf("readiness after gate closed = %s", report.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("readiness did not finish")
	}
}

// TestReadinessPanicFailsSafely ensures callback panics cannot terminate the
// process or expose sensitive panic values through the probe response.
func TestReadinessPanicFailsSafely(t *testing.T) {
	probes := newProbes(t)
	probes.SetReady(true)
	if err := probes.RegisterReadiness("broken", func(context.Context) error {
		panic("private panic detail")
	}); err != nil {
		t.Fatal(err)
	}
	response := request(t, probes.ReadinessHandler(), http.MethodGet)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
	assertReport(t, response, health.Report{
		Status: health.StatusFail,
		Checks: map[string]health.Status{"broken": health.StatusFail},
	})
	if strings.Contains(response.Body.String(), "private panic detail") {
		t.Fatal("panic detail exposed")
	}
}

// TestStuckReadinessCheckDoesNotMultiply verifies that repeated and concurrent
// probes time out without starting additional copies of a stuck callback, and
// that fresh checks can succeed once that callback finally returns.
func TestStuckReadinessCheckDoesNotMultiply(t *testing.T) {
	probes, err := health.New(health.WithTimeout(20 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	probes.SetReady(true)
	var calls atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	started := make(chan struct{})
	if err := probes.RegisterReadiness("blocked", func(context.Context) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release // Deliberately violates cancellation to exercise containment.
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first := make(chan health.Report, 1)
	go func() { first <- probes.CheckReadiness(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("check never started")
	}
	select {
	case report := <-first:
		if report.Status != health.StatusFail {
			t.Fatal("stuck check passed")
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not time out")
	}
	results := make(chan health.Report, 12)
	for range 12 {
		go func() { results <- probes.CheckReadiness(context.Background()) }()
	}
	for range 12 {
		select {
		case report := <-results:
			if report.Status != health.StatusFail {
				t.Fatal("overlapping probe passed")
			}
		case <-time.After(time.Second):
			t.Fatal("overlapping probe did not time out")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("callback executions = %d, want 1", calls.Load())
	}
	unblock()
	deadline := time.Now().Add(time.Second)
	for probes.CheckReadiness(context.Background()).Status != health.StatusPass {
		if time.Now().After(deadline) {
			t.Fatal("check did not recover after release")
		}
	}
	if calls.Load() < 2 {
		t.Fatal("late success was reused instead of a fresh execution")
	}
}

func newProbes(t *testing.T) *health.Probes {
	t.Helper()
	probes, err := health.New()
	if err != nil {
		t.Fatalf("new probes: %v", err)
	}
	return probes
}

func request(t *testing.T, handler http.Handler, method string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, "/", nil))
	return response
}

func assertReport(t *testing.T, response *httptest.ResponseRecorder, want health.Report) {
	t.Helper()
	var got health.Report
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Status != want.Status {
		t.Fatalf("status = %q, want %q", got.Status, want.Status)
	}
	if len(got.Checks) != len(want.Checks) {
		t.Fatalf("checks = %#v, want %#v", got.Checks, want.Checks)
	}
	for name, status := range want.Checks {
		if got.Checks[name] != status {
			t.Fatalf("check %q = %q, want %q", name, got.Checks[name], status)
		}
	}
}
