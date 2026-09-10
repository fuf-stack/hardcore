package observability_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/fuf-stack/hardcore/observability"
	"github.com/fuf-stack/hardcore/rpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestConnectErrorCorrelation logs once inside normalization without wire leakage.
func TestConnectErrorCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
	logErrors := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			if err != nil {
				// The application deliberately selects a safe diagnostic, not raw error text.
				logger.ErrorContext(ctx, "operation failed", "error_class", "internal")
			}
			return res, err
		}
	})
	correlate, err := observability.NewRequestIDMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	handler := connect.NewUnaryHandler("/example.v1.Service/Call",
		func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			return nil, errors.New("private backend detail")
		}, connect.WithInterceptors(rpc.UnaryErrorInterceptor(), logErrors))
	server := httptest.NewServer(correlate(handler))
	defer server.Close()
	client := connect.NewClient[emptypb.Empty, emptypb.Empty](server.Client(), server.URL+"/example.v1.Service/Call")
	_, err = client.CallUnary(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "private") {
		t.Fatalf("unexpected wire error: %v", err)
	}
	wire, ok := errors.AsType[*connect.Error](err)
	if !ok {
		t.Fatal("missing Connect error")
	}
	id := wire.Meta().Get(observability.RequestIDHeader)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log count = %d; want 1", len(lines))
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatal(err)
	}
	if id == "" || record["request_id"] != id || record["error_class"] != "internal" {
		t.Fatalf("missing error correlation: %v", record)
	}
	if strings.Contains(output.String(), "private") {
		t.Fatal("unselected error detail logged")
	}
}
