package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const defaultShutdownTimeout = 10 * time.Second

var (
	// ErrInvalidArgument indicates a missing server, listener, context, or option.
	ErrInvalidArgument = errors.New("service: invalid argument")
	// ErrInvalidShutdownTimeout indicates that graceful shutdown has no positive bound.
	ErrInvalidShutdownTimeout = errors.New("service: shutdown timeout must be positive")
)

// ShutdownHook runs when cancellation, a serving failure, or external shutdown
// ends serving, before Hardcore drains the HTTP server. Hooks share the
// configured shutdown deadline and should respect context cancellation.
// Hooks run synchronously. A hook that ignores cancellation can delay shutdown
// indefinitely; the timeout cannot forcibly interrupt application code.
type ShutdownHook func(context.Context) error

// Option configures HTTP service lifecycle.
type Option func(*config) error

type config struct {
	cleanup         []func() error
	shutdownTimeout time.Duration
	shutdownHooks   []ShutdownHook
}

// WithShutdownTimeout changes the total time available to shutdown hooks and
// the HTTP server. The default is ten seconds.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(cfg *config) error {
		if timeout <= 0 {
			return ErrInvalidShutdownTimeout
		}
		cfg.shutdownTimeout = timeout
		return nil
	}
}

// WithShutdownHook registers work to perform before graceful HTTP shutdown.
// Hooks run in registration order. A hook error is returned but does not stop
// later hooks or server shutdown.
func WithShutdownHook(hook ShutdownHook) Option {
	return func(cfg *config) error {
		if hook == nil {
			return fmt.Errorf("%w: nil shutdown hook", ErrInvalidArgument)
		}
		cfg.shutdownHooks = append(cfg.shutdownHooks, hook)
		return nil
	}
}

// WithCleanup registers resource cleanup after HTTP draining or forced connection
// closure on timeout, including bind failures. Callbacks run in reverse registration
// order and all errors are retained. Invalid arguments/options do not transfer
// ownership and do not run cleanup. Callbacks run synchronously without a deadline;
// they must return promptly. Forced closure cannot stop noncooperative handlers.
func WithCleanup(cleanup func() error) Option {
	return func(cfg *config) error {
		if cleanup == nil {
			return fmt.Errorf("%w: nil cleanup", ErrInvalidArgument)
		}
		cfg.cleanup = append(cfg.cleanup, cleanup)
		return nil
	}
}

// cleanupResources releases dependencies in reverse order, preserving every error.
func cleanupResources(cfg config) error {
	var errs []error
	for i := len(cfg.cleanup) - 1; i >= 0; i-- {
		if err := cfg.cleanup[i](); err != nil {
			errs = append(errs, fmt.Errorf("service: cleanup: %w", err))
		}
	}
	return errors.Join(errs...)
}

// ListenAndServe binds server.Addr and serves until the context is cancelled,
// the server is shut down externally, or serving fails.
func ListenAndServe(ctx context.Context, server *http.Server, options ...Option) (err error) {
	if ctx == nil || server == nil {
		return ErrInvalidArgument
	}

	cfg, err := configure(options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cleanupResources(cfg)) }()

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("service: listen on %q: %w", server.Addr, err)
	}

	return serve(ctx, server, listener, cfg)
}

// Serve runs server on an existing listener. Once called, the HTTP server owns
// the listener. This form is useful for socket activation and deterministic
// integration tests.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, options ...Option) (err error) {
	if ctx == nil || server == nil || listener == nil {
		return ErrInvalidArgument
	}

	cfg, err := configure(options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cleanupResources(cfg)) }()
	return serve(ctx, server, listener, cfg)
}

func configure(options []Option) (config, error) {
	cfg := config{shutdownTimeout: defaultShutdownTimeout}
	for _, option := range options {
		if option == nil {
			return config{}, fmt.Errorf("%w: nil option", ErrInvalidArgument)
		}
		if err := option(&cfg); err != nil {
			return config{}, err
		}
	}
	return cfg, nil
}

func serve(ctx context.Context, server *http.Server, listener net.Listener, cfg config) error {
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- normalizeServeError(server.Serve(listener))
	}()

	var serveErr error
	serveFinished := false
	select {
	case serveErr = <-serveResult:
		serveFinished = true
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()

	var shutdownErrors []error
	for _, hook := range cfg.shutdownHooks {
		if err := hook(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("service: shutdown hook: %w", err))
		}
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("service: graceful shutdown: %w", err))
		// Shutdown has already closed and waited for listeners. Close is a
		// best-effort fallback for connections that did not drain in time.
		_ = server.Close()
	}

	if !serveFinished {
		serveErr = <-serveResult
	}
	if serveErr != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("service: serve HTTP: %w", serveErr))
	}
	return errors.Join(shutdownErrors...)
}

func normalizeServeError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
