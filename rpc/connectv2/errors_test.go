package connectv2_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	"github.com/fuf-stack/hardcore/rpc/connectv2"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// privateDetail returns an error detail that must never reach a client.
func privateDetail(t *testing.T) *connect.ErrorDetail {
	t.Helper()
	detail, err := connectproto.NewErrorDetail(wrapperspb.String("private detail"))
	if err != nil {
		t.Fatal(err)
	}
	return detail
}

// TestNormalizeError covers classification, precedence, and diagnostic redaction.
func TestNormalizeError(t *testing.T) {
	private := errors.New("private storage failure")
	public := connect.NewError(connect.CodeInvalidArgument, "invalid input")
	tests := []struct {
		name    string
		err     error
		code    connect.Code
		message string
	}{
		// Wrapped cancellations must not disclose wrapper diagnostics.
		{"canceled", fmt.Errorf("private: %w", context.Canceled), connect.CodeCanceled, "context canceled"},
		// Deadlines take precedence when both context sentinels are joined.
		{"deadline", errors.Join(context.Canceled, context.DeadlineExceeded), connect.CodeDeadlineExceeded, "context deadline exceeded"},
		// An explicitly mapped status takes precedence over its underlying cause.
		{"explicit", connect.NewError(connect.CodeUnavailable, "try again").WithCause(context.Canceled), connect.CodeUnavailable, "try again"},
		// Explicit internal errors must not bypass redaction.
		{"internal", connect.NewError(connect.CodeInternal, "private").WithCause(private), connect.CodeInternal, "internal error"},
		// Raw unexpected failures receive a safe internal status.
		{"ordinary", private, connect.CodeInternal, "internal error"},
		// A peer's verdict is not the handler's own and must not be forwarded.
		{"remote", connect.NewError(connect.CodeNotFound, "private upstream").WithRemote(), connect.CodeInternal, "internal error"},
		// Wrapped remote verdicts are redacted the same way.
		{"remote wrapped", fmt.Errorf("private: %w", connect.NewError(connect.CodePermissionDenied, "private upstream").WithRemote()), connect.CodeInternal, "internal error"},
		// Unknown remains unknown but cannot expose its original message.
		{"unknown", connect.NewError(connect.CodeUnknown, "private").WithCause(private), connect.CodeUnknown, "internal error"},
		// Wrappers around expected public errors stay private.
		{"wrapped", fmt.Errorf("private: %w", public), connect.CodeInvalidArgument, "invalid input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := connectv2.NormalizeError(tt.err)
			wire, ok := errors.AsType[*connect.Error](got)
			if !ok || wire.Code() != tt.code || wire.Message() != tt.message {
				t.Fatalf("NormalizeError = %v; want %v: %s", got, tt.code, tt.message)
			}
			if errors.Is(got, private) || strings.Contains(got.Error(), "private") || wire.IsRemote() {
				t.Fatalf("private diagnostics retained: %v", got)
			}
		})
	}
	if connectv2.NormalizeError(nil) != nil {
		t.Fatal("nil changed")
	}
}

// TestNormalizeErrorDetails preserves trusted responses but strips internal data.
func TestNormalizeErrorDetails(t *testing.T) {
	detail := privateDetail(t)
	for _, tc := range []struct {
		name    string
		err     *connect.Error
		trusted bool
	}{
		{"internal", connect.NewError(connect.CodeInternal, "message"), false},
		{"invalid argument", connect.NewError(connect.CodeInvalidArgument, "message"), true},
		{"remote", connect.NewError(connect.CodeInvalidArgument, "message").WithRemote(), false},
		{"unknown", connect.NewError(connect.CodeUnknown, "message"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.err.WithDetail(detail).WithCause(errors.New("cause"))
			got, ok := errors.AsType[*connect.Error](connectv2.NormalizeError(original))
			if !ok {
				t.Fatal("not a Connect error")
			}
			if tc.trusted {
				if got != original {
					t.Fatal("trusted error replaced")
				}
			} else if len(got.Details()) != 0 || got.Unwrap() != nil {
				t.Fatal("internal details or cause retained")
			}
			if len(original.Details()) != 1 || original.Unwrap() == nil {
				t.Fatal("original mutated")
			}
		})
	}
}

// TestServerErrorInterceptorSuccess ensures inputs and nil errors pass unchanged.
func TestServerErrorInterceptorSuccess(t *testing.T) {
	spec := connect.Spec{Procedure: "/example.v1.EchoService/Echo"}
	ctx := t.Context()
	called := false
	handler := connectv2.ServerErrorInterceptor()(func(gotCtx context.Context, gotSpec connect.Spec, stream connect.ServerStream) error {
		called = true
		if gotCtx != ctx || gotSpec != spec || stream != nil {
			t.Fatal("input changed")
		}
		return nil
	})
	if err := handler(ctx, spec, nil); err != nil || !called {
		t.Fatalf("handler = %v, called %v", err, called)
	}
}

// echoProcedure and streamProcedure are neutral test procedures.
const (
	echoProcedure   = "/example.v1.EchoService/Echo"
	streamProcedure = "/example.v1.EchoService/Watch"
)

// newServer serves a unary and a server-streaming method over real HTTP. Both
// fail with handlerErr; the streaming method first sends one message. The
// logging interceptor records the error it observes inside the boundary.
func newServer(t *testing.T, handlerErr error, observed *[]error) (*connect.Client, connect.Spec, connect.Spec) {
	t.Helper()
	unary := connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: echoProcedure}
	streaming := connect.Spec{StreamType: connect.StreamTypeServer, Procedure: streamProcedure}
	logging := func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			err := next(ctx, spec, stream)
			*observed = append(*observed, err)
			return err
		}
	}
	server := connect.NewServer(connectv2.ServerErrorInterceptor(), logging)
	server.Register(
		connect.Method{Spec: unary, Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				return err
			}
			return handlerErr
		}},
		connect.Method{Spec: streaming, Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
			if err := stream.Receive(&emptypb.Empty{}); err != nil {
				return err
			}
			if err := stream.Send(wrapperspb.String("public")); err != nil {
				return err
			}
			return handlerErr
		}},
	)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	return connect.NewClient(connecthttp.NewTransport(httpServer.Client(), httpServer.URL)), unary, streaming
}

// TestServerErrorInterceptorWire verifies safe errors over actual HTTP transport
// for unary and server-streaming RPCs, after inner interceptors saw the original.
func TestServerErrorInterceptorWire(t *testing.T) {
	private := errors.New("private database credentials")
	for _, tc := range []struct {
		name    string
		err     error
		code    connect.Code
		message string
		details int
	}{
		{"internal", connect.NewError(connect.CodeInternal, "private").WithDetail(privateDetail(t)).WithCause(private), connect.CodeInternal, "internal error", 0},
		{"ordinary", private, connect.CodeInternal, "internal error", 0},
		{"remote", connect.NewError(connect.CodeNotFound, "private upstream").WithRemote(), connect.CodeInternal, "internal error", 0},
		// Safe explicit limits stay visible to clients with their public detail.
		{"resource exhausted", fmt.Errorf("private: %w", connect.NewError(connect.CodeResourceExhausted, "too many items").WithDetail(privateDetail(t))), connect.CodeResourceExhausted, "too many items", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var observed []error
			client, unary, streaming := newServer(t, tc.err, &observed)
			check := func(err error) {
				t.Helper()
				wire, ok := errors.AsType[*connect.Error](err)
				if !ok || wire.Code() != tc.code || wire.Message() != tc.message || len(wire.Details()) != tc.details {
					t.Fatalf("wire error = %v (%d details); want %v: %s", err, len(wire.Details()), tc.code, tc.message)
				}
				if tc.details == 0 && strings.Contains(err.Error(), "private") {
					t.Fatalf("private diagnostics on the wire: %v", err)
				}
			}
			check(client.CallUnary(t.Context(), unary, &emptypb.Empty{}, &emptypb.Empty{}))

			stream, err := client.CallServerStream(t.Context(), streaming, &emptypb.Empty{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = stream.Close() })
			msg := &wrapperspb.StringValue{}
			if err := stream.Receive(msg); err != nil || msg.GetValue() != "public" {
				t.Fatalf("first message = %v, %v", msg, err)
			}
			check(stream.Receive(msg))

			if len(observed) != 2 || observed[0] != tc.err || observed[1] != tc.err {
				t.Fatalf("logging interceptor observed %v; want the original error twice", observed)
			}
		})
	}
}

// TestServerErrorInterceptorWireSuccess keeps successful RPCs unchanged on the wire.
func TestServerErrorInterceptorWireSuccess(t *testing.T) {
	var observed []error
	client, unary, _ := newServer(t, nil, &observed)
	if err := client.CallUnary(t.Context(), unary, &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
}
