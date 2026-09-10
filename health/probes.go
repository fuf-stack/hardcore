package health

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultCheckTimeout = 2 * time.Second

var (
	// ErrDuplicateCheck indicates that a readiness check name is already in use.
	ErrDuplicateCheck = errors.New("health: readiness check already registered")
	// ErrInvalidCheck indicates that a readiness check has no name or function.
	ErrInvalidCheck = errors.New("health: invalid readiness check")
	// ErrInvalidTimeout indicates that the configured check timeout is not positive.
	ErrInvalidTimeout = errors.New("health: timeout must be positive")
)

// Status is the externally visible state of a probe or dependency check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
)

// Check reports whether a dependency is ready. Implementations should respect
// context cancellation and must not include sensitive details in returned
// errors. Error text is deliberately not exposed by the HTTP handler.
// Panics in a check are recovered and reported as failed readiness checks.
// Implementations must honor cancellation: Go cannot forcibly stop a callback.
type Check func(context.Context) error

// Report is the JSON representation returned by a probe.
type Report struct {
	Status Status            `json:"status"`
	Checks map[string]Status `json:"checks,omitempty"`
}

// Option configures Probes.
type Option func(*config) error

type config struct {
	timeout time.Duration
}

// WithTimeout bounds the total time allowed for readiness checks.
func WithTimeout(timeout time.Duration) Option {
	return func(cfg *config) error {
		if timeout <= 0 {
			return ErrInvalidTimeout
		}
		cfg.timeout = timeout
		return nil
	}
}

// Probes owns a liveness endpoint, an explicit readiness gate, and optional
// readiness checks. It is safe for concurrent use.
type Probes struct {
	timeout time.Duration
	ready   atomic.Bool

	mu     sync.RWMutex
	checks map[string]*checkState
}

type checkRun struct {
	done chan struct{}
	err  error
}

type checkState struct {
	check   Check
	mu      sync.Mutex
	running *checkRun
}

// Concurrent probes share one execution, including one that outlives its context.
// A stuck callback therefore cannot accumulate additional callback goroutines.
func (c *checkState) start(ctx context.Context) *checkRun {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running != nil {
		return c.running
	}
	run := &checkRun{done: make(chan struct{})}
	c.running = run
	go func() {
		err := runCheck(ctx, c.check)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.mu.Lock()
		run.err = err
		c.running = nil
		close(run.done)
		c.mu.Unlock()
	}()
	return run
}

// New creates probes whose readiness gate is initially closed.
func New(options ...Option) (*Probes, error) {
	cfg := config{timeout: defaultCheckTimeout}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("%w: nil option", ErrInvalidCheck)
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}

	return &Probes{
		timeout: cfg.timeout,
		checks:  make(map[string]*checkState),
	}, nil
}

// RegisterReadiness adds a named dependency check. Names must be unique and
// registration may safely happen while probes are being served.
func (p *Probes) RegisterReadiness(name string, check Check) error {
	name = strings.TrimSpace(name)
	if name == "" || check == nil {
		return ErrInvalidCheck
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.checks[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateCheck, name)
	}
	p.checks[name] = &checkState{check: check}
	return nil
}

// SetReady opens or closes the process-level readiness gate. New probes start
// with the gate closed so incomplete startup cannot report ready accidentally.
func (p *Probes) SetReady(ready bool) {
	p.ready.Store(ready)
}

// CheckReadiness evaluates the gate and a snapshot of all registered checks.
// Checks run concurrently and share the configured total timeout.
// Overlapping probes share each running check, using its initiating probe's
// context. Each waiting probe retains its own timeout. A timed-out check must
// return before a new execution can start; its late success is treated as failure.
func (p *Probes) CheckReadiness(ctx context.Context) Report {
	if !p.ready.Load() {
		return Report{Status: StatusFail}
	}

	checks := p.snapshot()
	if len(checks) == 0 {
		return Report{Status: StatusPass}
	}

	checkCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	type result struct {
		name string
		err  error
	}

	results := make(chan result, len(checks))
	for name, check := range checks {
		run := check.start(checkCtx)
		go func() {
			select {
			case <-run.done:
				results <- result{name: name, err: run.err}
			case <-checkCtx.Done():
			}
		}()
	}

	report := Report{
		Status: StatusPass,
		Checks: make(map[string]Status, len(checks)),
	}

	for len(report.Checks) < len(checks) {
		select {
		case result := <-results:
			if result.err != nil {
				report.Status = StatusFail
				report.Checks[result.name] = StatusFail
				continue
			}
			report.Checks[result.name] = StatusPass
		case <-checkCtx.Done():
			report.Status = StatusFail
			for name := range checks {
				if _, finished := report.Checks[name]; !finished {
					report.Checks[name] = StatusFail
				}
			}
		}
	}

	// A dependency result must not override a gate closed while checks ran.
	if !p.ready.Load() {
		report.Status = StatusFail
	}
	return report
}

func runCheck(ctx context.Context, check Check) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("health: readiness check panicked")
		}
	}()
	return check(ctx)
}

// LivenessHandler returns a handler that reports whether this process can
// answer HTTP requests. It intentionally performs no dependency checks.
func (p *Probes) LivenessHandler() http.Handler {
	return probeHandler(func(context.Context) (Report, int) {
		return Report{Status: StatusPass}, http.StatusOK
	})
}

// ReadinessHandler returns a handler that evaluates the readiness gate and
// registered dependency checks.
func (p *Probes) ReadinessHandler() http.Handler {
	return probeHandler(func(ctx context.Context) (Report, int) {
		report := p.CheckReadiness(ctx)
		if report.Status == StatusPass {
			return report, http.StatusOK
		}
		return report, http.StatusServiceUnavailable
	})
}

func (p *Probes) snapshot() map[string]*checkState {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return maps.Clone(p.checks)
}

func probeHandler(report func(context.Context) (Report, int)) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		body, status := report(request.Context())
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		if request.Method == http.MethodHead {
			return
		}
		_ = json.MarshalEncode(jsontext.NewEncoder(writer), body)
	})
}
