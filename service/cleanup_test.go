package service_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"testing"

	"github.com/fuf-stack/hardcore/service"
)

// TestCleanupOnBindFailure preserves both errors and releases in reverse order.
func TestCleanupOnBindFailure(t *testing.T) {
	var calls []int
	closeErr := errors.New("close failure")
	err := service.ListenAndServe(context.Background(), &http.Server{Addr: "invalid address"},
		service.WithCleanup(func() error { calls = append(calls, 1); return nil }),
		service.WithCleanup(func() error { calls = append(calls, 2); return closeErr }),
	)
	var addrErr *net.AddrError
	if !errors.Is(err, closeErr) || !errors.As(err, &addrErr) {
		t.Fatalf("lost errors: %v", err)
	}
	if !slices.Equal(calls, []int{2, 1}) {
		t.Fatalf("cleanup order: %v", calls)
	}
}

// TestCleanupAfterCancellation preserves hook and cleanup errors on successful drain.
func TestCleanupAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hookErr, closeErr := errors.New("hook failure"), errors.New("close failure")
	var calls []string
	err := service.ListenAndServe(ctx, &http.Server{Addr: "127.0.0.1:0"},
		service.WithShutdownHook(func(context.Context) error { calls = append(calls, "hook"); return hookErr }),
		service.WithCleanup(func() error { calls = append(calls, "cleanup"); return closeErr }),
	)
	if !errors.Is(err, hookErr) || !errors.Is(err, closeErr) {
		t.Fatalf("lost errors: %v", err)
	}
	if !slices.Equal(calls, []string{"hook", "cleanup"}) {
		t.Fatalf("order: %v", calls)
	}
}

// TestInvalidOptionsRetainOwnership ensures validation never partially transfers resources.
func TestInvalidOptionsRetainOwnership(t *testing.T) {
	calls := 0
	cleanup := service.WithCleanup(func() error { calls++; return nil })
	err := service.ListenAndServe(context.Background(), &http.Server{}, cleanup, service.WithCleanup(nil))
	if !errors.Is(err, service.ErrInvalidArgument) || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
