// Package rpc provides product-neutral Connect server error boundaries.
package rpc

import (
	"context"
	"errors"

	"connectrpc.com/connect"
)

// NormalizeError prepares a handler error for public transport. Nil stays nil.
// Explicit Connect errors are trusted public responses, except Internal and
// Unknown: their messages, details, metadata, and causes are discarded. Wrapped
// Connect errors retain only the underlying Connect error, not wrapper text.
// Otherwise context errors receive stable codes and all other errors become
// Internal. The original error is not retained; log it before this boundary.
// Consumers remain responsible for safe messages on other explicit status codes.
func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	var wire *connect.Error
	if errors.As(err, &wire) && wire != nil {
		switch wire.Code() {
		case connect.CodeInternal, connect.CodeUnknown:
			return connect.NewError(wire.Code(), errors.New("internal error"))
		default:
			return wire
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded)
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, context.Canceled)
	}
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

// UnaryErrorInterceptor normalizes errors returned by unary server handlers.
// Register it first so it surrounds other interceptors; inner logging can observe
// the original error. It does not recover panics or cover streaming RPCs, HTTP
// middleware, or protocol errors raised outside the handler interceptor chain.
// Do not install it on clients: it deliberately discards diagnostic information.
func UnaryErrorInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			if err != nil {
				return nil, NormalizeError(err)
			}
			return res, nil
		}
	}
}
