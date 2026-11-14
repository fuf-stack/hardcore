// Package connectv2 provides product-neutral Connect v2 server error boundaries.
// It mirrors package rpc, which targets Connect v1, for servers built on
// connectrpc.com/connect/v2.
package connectv2

import (
	"context"
	"errors"

	"connectrpc.com/connect/v2"
)

// internalMessage is the only public message for redacted failures.
const internalMessage = "internal error"

// NormalizeError prepares a handler error for public transport. Nil stays nil.
// Explicit local Connect errors are trusted public responses, except Internal
// and Unknown: their messages, details, and causes are discarded. Remote errors,
// a peer's verdict returned unchanged by the handler, become Internal without
// their code, message, or details. Wrapped Connect errors retain only the
// underlying Connect error, not wrapper text. Otherwise context errors receive
// stable codes and all other errors become Internal. The original error is not
// retained; log it before this boundary. Consumers remain responsible for safe
// messages and details on other explicit status codes.
func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	if wire, ok := errors.AsType[*connect.Error](err); ok && wire != nil {
		switch {
		case wire.IsRemote():
			return connect.NewError(connect.CodeInternal, internalMessage)
		case wire.Code() == connect.CodeInternal, wire.Code() == connect.CodeUnknown:
			return connect.NewError(wire.Code(), internalMessage)
		default:
			return wire
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded.Error())
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, context.Canceled.Error())
	}
	return connect.NewError(connect.CodeInternal, internalMessage)
}

// ServerErrorInterceptor normalizes the error that ends any server RPC, unary
// or streaming. Pass it first to connect.NewServer so it surrounds the other
// interceptors; inner logging can observe the original error. Messages already
// sent on a stream are not affected. It does not recover panics or cover HTTP
// middleware or protocol errors raised before the interceptor chain runs.
// Do not use it on clients: it deliberately discards diagnostic information.
func ServerErrorInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			return NormalizeError(next(ctx, spec, stream))
		}
	}
}
