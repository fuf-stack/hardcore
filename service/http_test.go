package service_test

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

	"github.com/fuf-stack/hardcore/service"
)

// TestServeShutsDownAfterCancellation verifies the normal lifecycle path:
// cancellation runs registered hooks and closes the server without an error.
func TestServeShutsDownAfterCancellation(t *testing.T) {
	listener := listen(t)
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, "ok")
		}),
		ReadHeaderTimeout: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	var hookCalled atomic.Bool
	go func() {
		result <- service.Serve(ctx, server, listener,
			service.WithShutdownTimeout(time.Second),
			service.WithShutdownHook(func(context.Context) error {
				hookCalled.Store(true)
				return nil
			}),
		)
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("GET service: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not shut down")
	}
	if !hookCalled.Load() {
		t.Fatal("shutdown hook was not called")
	}
}

// TestServeWaitsForInFlightRequest verifies that graceful shutdown drains an
// active handler rather than returning as soon as cancellation is received.
func TestServeWaitsForInFlightRequest(t *testing.T) {
	listener := listen(t)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	handlerFinished := make(chan struct{})
	var cleaned atomic.Bool
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			defer close(handlerFinished)
			close(requestStarted)
			<-releaseRequest
			writer.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- service.Serve(ctx, server, listener, service.WithShutdownTimeout(time.Second),
			service.WithCleanup(func() error {
				select {
				case <-handlerFinished:
				default:
					t.Error("cleanup ran before the handler finished")
				}
				cleaned.Store(true)
				return nil
			}))
	}()

	requestResult := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			err = response.Body.Close()
		}
		requestResult <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()

	select {
	case err := <-serveResult:
		t.Fatalf("service returned before request drained: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseRequest)
	if err := <-requestResult; err != nil {
		t.Fatalf("request: %v", err)
	}
	if err := <-serveResult; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if !cleaned.Load() {
		t.Fatal("cleanup did not run after draining")
	}
}

// TestServeReturnsListenerFailure verifies that an unexpected accept failure is
// preserved for the caller instead of being mistaken for graceful shutdown.
func TestServeReturnsListenerFailure(t *testing.T) {
	listener := listen(t)
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	err := service.Serve(context.Background(), &http.Server{}, listener)
	if err == nil {
		t.Fatal("serve error = nil, want listener failure")
	}
}

// TestOptionsRejectInvalidValues verifies that unusable lifecycle options fail
// before the server takes ownership of the provided listener.
func TestOptionsRejectInvalidValues(t *testing.T) {
	server := &http.Server{}
	listener := listen(t)
	defer listener.Close()

	if err := service.Serve(context.Background(), server, listener, service.WithShutdownTimeout(0)); !errors.Is(err, service.ErrInvalidShutdownTimeout) {
		t.Fatalf("timeout error = %v, want ErrInvalidShutdownTimeout", err)
	}
	if err := service.Serve(context.Background(), server, listener, service.WithShutdownHook(nil)); !errors.Is(err, service.ErrInvalidArgument) {
		t.Fatalf("hook error = %v, want ErrInvalidArgument", err)
	}
	if err := service.Serve(context.Background(), server, listener, nil); !errors.Is(err, service.ErrInvalidArgument) {
		t.Fatalf("nil option error = %v, want ErrInvalidArgument", err)
	}
}

// TestShutdownHookErrorDoesNotPreventShutdown verifies that hook failures are
// reported while the HTTP server is still shut down normally.
func TestShutdownHookErrorDoesNotPreventShutdown(t *testing.T) {
	listener := listen(t)
	server := &http.Server{
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ReadHeaderTimeout: time.Second,
	}
	wantErr := errors.New("close dependency")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- service.Serve(ctx, server, listener,
			service.WithShutdownHook(func(context.Context) error { return wantErr }),
		)
	}()

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, wantErr) {
			t.Fatalf("serve error = %v, want wrapped hook error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not shut down after hook error")
	}
}

// TestListenAndServeReturnsBindFailure verifies that an occupied address is
// reported synchronously with its underlying bind error intact.
func TestListenAndServeReturnsBindFailure(t *testing.T) {
	listener := listen(t)
	defer listener.Close()

	server := &http.Server{Addr: listener.Addr().String()}
	if err := service.ListenAndServe(context.Background(), server); err == nil {
		t.Fatal("listen error = nil, want bind failure")
	}
}

// TestListenAndServeStopsOnCancelledContext exercises successful listener
// creation followed by immediate, clean shutdown for a cancelled context.
func TestListenAndServeStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	server := &http.Server{Addr: "127.0.0.1:0"}
	if err := service.ListenAndServe(ctx, server); err != nil {
		t.Fatalf("listen and serve: %v", err)
	}
}

// TestListenAndServeRejectsInvalidValues verifies that invalid arguments and
// options are rejected before an address is bound.
func TestListenAndServeRejectsInvalidValues(t *testing.T) {
	if err := service.ListenAndServe(nil, &http.Server{}); !errors.Is(err, service.ErrInvalidArgument) {
		t.Fatalf("argument error = %v, want ErrInvalidArgument", err)
	}
	if err := service.ListenAndServe(context.Background(), &http.Server{}, service.WithShutdownTimeout(0)); !errors.Is(err, service.ErrInvalidShutdownTimeout) {
		t.Fatalf("option error = %v, want ErrInvalidShutdownTimeout", err)
	}
}

// TestServeForceClosesAfterShutdownTimeout verifies that a handler which cannot
// drain is force-closed and that the shutdown deadline error reaches the caller.
func TestServeForceClosesAfterShutdownTimeout(t *testing.T) {
	listener := listen(t)
	var cleaned atomic.Bool
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(requestStarted)
			<-releaseRequest
		}),
		ReadHeaderTimeout: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- service.Serve(ctx, server, listener, service.WithShutdownTimeout(50*time.Millisecond),
			service.WithCleanup(func() error { cleaned.Store(true); return nil }))
	}()
	requestResult := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			err = response.Body.Close()
		}
		requestResult <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()

	select {
	case err := <-serveResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("serve error = %v, want context deadline exceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not force shutdown")
	}

	if !cleaned.Load() {
		t.Error("cleanup did not run after forced connection closure")
	}
	close(releaseRequest)
	<-requestResult
}

// TestServeReturnsAfterExternalShutdown verifies that another owner calling
// http.Server.Shutdown is treated as a normal, successful termination.
func TestServeReturnsAfterExternalShutdown(t *testing.T) {
	listener := listen(t)
	server := &http.Server{
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ReadHeaderTimeout: time.Second,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- service.Serve(context.Background(), server, listener)
	}()

	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("GET service: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("external shutdown: %v", err)
	}

	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not return after external shutdown")
	}
}

// TestServePreservesFailureDuringShutdown verifies that an accept failure racing
// with cancellation is not hidden by the subsequent graceful shutdown.
func TestServePreservesFailureDuringShutdown(t *testing.T) {
	wantErr := errors.New("accept failed")
	listener := newObservedErrorListener(wantErr)
	server := &http.Server{}
	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- service.Serve(ctx, server, listener,
			service.WithShutdownHook(func(context.Context) error {
				listener.fail()
				<-listener.failureObserved
				return nil
			}),
		)
	}()

	select {
	case <-listener.accepting:
	case <-time.After(time.Second):
		t.Fatal("server did not begin accepting")
	}
	cancel()

	select {
	case err := <-serveResult:
		if !errors.Is(err, wantErr) {
			t.Fatalf("serve error = %v, want accept failure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not return after accept failure")
	}
}

// TestServeRejectsMissingArguments verifies that required lifecycle ownership
// inputs are validated instead of causing a nil dereference in a goroutine.
func TestServeRejectsMissingArguments(t *testing.T) {
	if err := service.Serve(nil, &http.Server{}, nil); !errors.Is(err, service.ErrInvalidArgument) {
		t.Fatalf("argument error = %v, want ErrInvalidArgument", err)
	}
}

// TestServeDrainsOnEveryExit verifies that listener failures and external
// shutdown run hooks once, preserve errors, and wait for active requests.
func TestServeDrainsOnEveryExit(t *testing.T) {
	for _, external := range []bool{false, true} {
		name := "listener failure"
		if external {
			name = "external shutdown"
		}
		// Both exit causes must obey the same cleanup and request-draining contract.
		t.Run(name, func(t *testing.T) {
			listener := listen(t)
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				w.WriteHeader(http.StatusNoContent)
			}), ReadHeaderTimeout: time.Second}
			t.Cleanup(func() { releaseHandler(); _ = server.Close(); _ = listener.Close() })
			hookStarted := make(chan struct{})
			var hookCalls atomic.Int32
			hookErr := errors.New("hook cleanup failure")
			result := make(chan error, 1)
			go func() {
				result <- service.Serve(context.Background(), server, listener,
					service.WithShutdownTimeout(2*time.Second),
					service.WithShutdownHook(func(context.Context) error {
						if hookCalls.Add(1) == 1 {
							close(hookStarted)
						}
						return hookErr
					}))
			}()
			client := &http.Client{Timeout: 3 * time.Second}
			requestDone := make(chan error, 1)
			go func() {
				response, err := client.Get("http://" + listener.Addr().String())
				if err == nil {
					_ = response.Body.Close()
					if response.StatusCode != http.StatusNoContent {
						err = errors.New("unexpected response status")
					}
				}
				requestDone <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			externalDone := make(chan error, 1)
			if external {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					externalDone <- server.Shutdown(ctx)
				}()
			} else if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-hookStarted:
			case <-time.After(time.Second):
				t.Fatal("cleanup hook did not run")
			}
			select {
			case err := <-result:
				t.Fatalf("returned before request drained: %v", err)
			case <-time.After(25 * time.Millisecond):
			}
			releaseHandler()
			select {
			case err := <-result:
				if !errors.Is(err, hookErr) {
					t.Fatalf("hook error lost: %v", err)
				}
				if !external && !errors.Is(err, net.ErrClosed) {
					t.Fatalf("listener error lost: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("service did not finish")
			}
			if hookCalls.Load() != 1 {
				t.Fatalf("hook calls = %d", hookCalls.Load())
			}
			if err := <-requestDone; err != nil {
				t.Fatal(err)
			}
			if external {
				if err := <-externalDone; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return listener
}

type observedErrorListener struct {
	accepting       chan struct{}
	failure         chan struct{}
	failureObserved chan struct{}
	wantErr         error
	acceptOnce      sync.Once
	failOnce        sync.Once
	observedOnce    sync.Once
}

func newObservedErrorListener(wantErr error) *observedErrorListener {
	return &observedErrorListener{
		accepting:       make(chan struct{}),
		failure:         make(chan struct{}),
		failureObserved: make(chan struct{}),
		wantErr:         wantErr,
	}
}

func (l *observedErrorListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() { close(l.accepting) })
	<-l.failure
	return nil, observedNetError{listener: l}
}

func (l *observedErrorListener) Close() error {
	l.fail()
	return nil
}

func (l *observedErrorListener) Addr() net.Addr {
	return staticAddr("observed-error-listener")
}

func (l *observedErrorListener) fail() {
	l.failOnce.Do(func() { close(l.failure) })
}

type observedNetError struct {
	listener *observedErrorListener
}

func (e observedNetError) Error() string { return e.listener.wantErr.Error() }
func (e observedNetError) Timeout() bool { return false }
func (e observedNetError) Unwrap() error { return e.listener.wantErr }
func (e observedNetError) Temporary() bool {
	e.listener.observedOnce.Do(func() { close(e.listener.failureObserved) })
	return false
}

type staticAddr string

func (a staticAddr) Network() string { return string(a) }
func (a staticAddr) String() string  { return string(a) }
