package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fuf-stack/hardcore/health"
)

// TestRunStopsWhenContextIsAlreadyCancelled verifies that the example composes
// startup and shutdown correctly even when termination has already been requested.
func TestRunStopsWhenContextIsAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := run(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestExampleRoutes verifies that the runnable example exposes its application,
// liveness, and readiness handlers with the documented successful responses.
func TestExampleRoutes(t *testing.T) {
	probes, err := health.New()
	if err != nil {
		t.Fatalf("new probes: %v", err)
	}
	probes.SetReady(true)
	server := newServer("127.0.0.1:0", probes)

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{path: "/", wantStatus: http.StatusOK, wantBody: "Hardcore example service\n"},
		{path: "/healthz", wantStatus: http.StatusOK, wantBody: "{\"status\":\"pass\"}\n"},
		{path: "/readyz", wantStatus: http.StatusOK, wantBody: "{\"status\":\"pass\"}\n"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			body, err := io.ReadAll(response.Result().Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if got := string(body); got != test.wantBody {
				t.Fatalf("body = %q, want %q", got, test.wantBody)
			}
		})
	}
}
