package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/fuf-stack/hardcore/rpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestNormalizeError covers classification, precedence, and diagnostic redaction.
func TestNormalizeError(t *testing.T) {
	private := errors.New("private storage failure")
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
		{"explicit", connect.NewError(connect.CodeUnavailable, context.Canceled), connect.CodeUnavailable, "context canceled"},
		// Explicit internal errors must not bypass redaction.
		{"internal", connect.NewError(connect.CodeInternal, private), connect.CodeInternal, "internal error"},
		// Raw unexpected failures receive a safe internal status.
		{"ordinary", private, connect.CodeInternal, "internal error"},
		// Unknown remains unknown but cannot expose its original message.
		{"unknown", connect.NewError(connect.CodeUnknown, private), connect.CodeUnknown, "internal error"},
		// Wrappers around expected public errors stay private.
		{"wrapped", fmt.Errorf("private: %w", connect.NewError(connect.CodeInvalidArgument, errors.New("invalid input"))), connect.CodeInvalidArgument, "invalid input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rpc.NormalizeError(tt.err)
			var wire *connect.Error
			if !errors.As(got, &wire) || wire.Code() != tt.code || wire.Message() != tt.message {
				t.Fatalf("NormalizeError = %v; want %v: %s", got, tt.code, tt.message)
			}
			if errors.Is(got, private) {
				t.Fatal("private cause retained")
			}
		})
	}
}

// TestNormalizeErrorMetadata preserves trusted responses but strips internal data.
func TestNormalizeErrorMetadata(t *testing.T) {
	detail, err := connect.NewErrorDetail(&emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []connect.Code{connect.CodeInternal, connect.CodeUnknown, connect.CodeInvalidArgument} {
		t.Run(code.String(), func(t *testing.T) {
			original := connect.NewError(code, errors.New("message"))
			original.Meta().Set("X-Diagnostic", "private")
			original.AddDetail(detail)
			got := rpc.NormalizeError(original).(*connect.Error)
			if code == connect.CodeInvalidArgument {
				if got != original {
					t.Fatal("trusted error replaced")
				}
			} else if len(got.Meta()) != 0 || len(got.Details()) != 0 {
				t.Fatal("internal metadata or details retained")
			}
			if original.Meta().Get("X-Diagnostic") != "private" || len(original.Details()) != 1 {
				t.Fatal("original mutated")
			}
		})
	}
}

// TestUnarySuccess ensures successful responses and nil errors are unchanged.
func TestUnarySuccess(t *testing.T) {
	if rpc.NormalizeError(nil) != nil {
		t.Fatal("nil changed")
	}
	response := connect.NewResponse(&emptypb.Empty{})
	request := connect.NewRequest(&emptypb.Empty{})
	ctx := context.Background()
	wrapped := rpc.UnaryErrorInterceptor().WrapUnary(func(gotCtx context.Context, gotReq connect.AnyRequest) (connect.AnyResponse, error) {
		if gotCtx != ctx || gotReq != request {
			t.Fatal("input changed")
		}
		return response, nil
	})
	got, err := wrapped(ctx, request)
	if got != response || err != nil {
		t.Fatalf("response = %v, %v", got, err)
	}
}

// TestUnaryWireRedaction verifies safe errors over actual Connect HTTP transport.
func TestUnaryWireRedaction(t *testing.T) {
	const procedure = "/example.v1.EchoService/Echo"
	handler := connect.NewUnaryHandler(procedure,
		func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("private database credentials"))
		}, connect.WithInterceptors(rpc.UnaryErrorInterceptor()))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := connect.NewClient[emptypb.Empty, emptypb.Empty](server.Client(), server.URL+procedure)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.CallUnary(ctx, connect.NewRequest(&emptypb.Empty{}))
	if connect.CodeOf(err) != connect.CodeInternal || strings.Contains(err.Error(), "private") {
		t.Fatalf("unexpected wire error: %v", err)
	}
	var wire *connect.Error
	if !errors.As(err, &wire) || wire.Message() != "internal error" {
		t.Fatalf("wire message: %v", err)
	}
}
