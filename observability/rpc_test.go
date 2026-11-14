package observability_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/fuf-stack/hardcore/observability"
	"github.com/fuf-stack/hardcore/rpc/connectv2"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestConnectErrorCorrelation logs once inside normalization without wire leakage.
func TestConnectErrorCorrelation(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(observability.NewLogHandler(slog.NewJSONHandler(&output, nil)))
	logErrors := func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			err := next(ctx, spec, stream)
			if err != nil {
				// The application deliberately selects a safe diagnostic, not raw error text.
				logger.ErrorContext(ctx, "operation failed", "error_class", "internal")
			}
			return err
		}
	}
	correlate, err := observability.NewRequestIDMiddleware()
	if err != nil {
		t.Fatal(err)
	}
	spec := connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: "/example.v1.Service/Call"}
	server := connect.NewServer(connectv2.ServerErrorInterceptor(), logErrors)
	server.Register(connect.Method{Spec: spec, Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
		if err := stream.Receive(&emptypb.Empty{}); err != nil {
			return err
		}
		return errors.New("private backend detail")
	}})
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	httpServer := httptest.NewServer(correlate(mux))
	defer httpServer.Close()
	client := connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL))
	ctx, info := connect.NewClientContext(t.Context())
	err = client.CallUnary(ctx, spec, &emptypb.Empty{}, &emptypb.Empty{})
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "private") {
		t.Fatalf("unexpected wire error: %v", err)
	}
	if _, ok := errors.AsType[*connect.Error](err); !ok {
		t.Fatal("missing Connect error")
	}
	id := info.ResponseHeader().Get(observability.RequestIDHeader)
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
