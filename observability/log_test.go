package observability_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fuf-stack/hardcore/observability"
)

// TestLogCorrelation preserves filtering, grouping, and requestless startup logs.
func TestLogCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
	logger.InfoContext(t.Context(), "startup")
	logger.DebugContext(t.Context(), "filtered")
	middleware, err := observability.NewRequestIDMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	h := middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id = observability.RequestID(r.Context())
		logger.With("service", "example").WithGroup("operation").With("kind", "read").InfoContext(r.Context(), "handled", "ok", true)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/?secret=hidden", nil))
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("log count = %d; want 2", len(lines))
	}
	var startup, request map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &startup); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &request); err != nil {
		t.Fatal(err)
	}
	if _, ok := startup["request_id"]; ok {
		t.Fatal("requestless log gained an ID")
	}
	group, ok := request["operation"].(map[string]any)
	if !ok || group["request_id"] != id || group["kind"] != "read" || group["ok"] != true || request["service"] != "example" {
		t.Fatalf("unexpected log: %v", request)
	}
	if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "hidden") {
		t.Fatal("request data was logged")
	}
}

type failingHandler struct {
	record slog.Record
	err    error
}

// Enabled accepts records so the wrapper's error forwarding can be tested.
func (h *failingHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle captures the enriched record and returns a deliberate sink failure.
func (h *failingHandler) Handle(_ context.Context, r slog.Record) error { h.record = r; return h.err }

// WithAttrs satisfies slog.Handler; attribute semantics use the JSON handler test.
func (h *failingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup satisfies slog.Handler; grouping semantics use the JSON handler test.
func (h *failingHandler) WithGroup(string) slog.Handler { return h }

// TestLogHandlerRecordOwnership keeps sink errors and leaves shared records intact.
func TestLogHandlerRecordOwnership(t *testing.T) {
	sink := &failingHandler{err: errors.New("sink unavailable")}
	handler := observability.NewLogHandler(sink)
	middleware, err := observability.NewRequestIDMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		record := slog.NewRecord(time.Now(), slog.LevelInfo, "event", 0)
		for i := range 8 {
			record.AddAttrs(slog.Int(fmt.Sprintf("field%d", i), i))
		}
		if err := handler.Handle(r.Context(), record); err != sink.err {
			t.Fatalf("sink error = %v", err)
		}
		if record.NumAttrs() != 8 || sink.record.NumAttrs() != 9 {
			t.Fatal("shared record mutated or ID missing")
		}
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// TestNilLogHandler fails immediately rather than panicking on a later request.
func TestNilLogHandler(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected nil-handler panic")
		}
	}()
	observability.NewLogHandler(nil)
}

// TestHTTPConcurrentCorrelation checks real HTTP headers and request-log isolation.
func TestHTTPConcurrentCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
	middleware, err := observability.NewRequestIDMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "handled", "marker", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})))
	defer server.Close()
	const count = 24
	type result struct{ marker, id string }
	results := make(chan result, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() {
			marker := fmt.Sprintf("/request-%d", i)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+marker+"?secret=private", nil)
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Authorization", "Bearer private")
			req.Header.Set(observability.RequestIDHeader, "spoofed")
			res, err := server.Client().Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() {
				if err := res.Body.Close(); err != nil {
					t.Error(err)
				}
			}()
			body, err := io.ReadAll(res.Body)
			if err != nil || len(body) != 0 || res.StatusCode != http.StatusAccepted {
				t.Error("response contract changed")
			}
			results <- result{marker, res.Header.Get(observability.RequestIDHeader)}
		})
	}
	wg.Wait()
	close(results)
	want := make(map[string]string)
	seen := make(map[string]bool)
	for res := range results {
		if res.id == "" || res.id == "spoofed" || seen[res.id] {
			t.Fatal("missing, reused, or spoofed ID")
		}
		seen[res.id] = true
		want[res.marker] = res.id
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(want) != count || len(lines) != count {
		t.Fatal("missing response or log record")
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		marker, _ := record["marker"].(string)
		if want[marker] == "" || record["request_id"] != want[marker] {
			t.Fatal("cross-request log correlation")
		}
		delete(want, marker)
	}
	if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "Authorization") || strings.Contains(output.String(), "spoofed") {
		t.Fatal("sensitive request data leaked")
	}
}
