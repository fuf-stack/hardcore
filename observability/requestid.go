// Package observability provides opt-in request correlation without configuring
// global loggers, telemetry exporters, or application access policy.
package observability

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// RequestIDHeader carries diagnostic correlation, never authentication or trust.
const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

// RequestID returns the middleware's request ID, or empty when none was assigned.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// NewRequestIDMiddleware creates request correlation with an explicit proxy policy.
// By default every request receives a fresh cryptographically random ID. Only a
// direct peer in trustedProxies may supply an ID, using exactly one header value
// of 1–128 ASCII letters, digits, dots, underscores, or hyphens. Other input is
// replaced. Trust uses RemoteAddr only, never forwarding headers; install this
// middleware before anything that rewrites RemoteAddr. Trust only proxies that
// replace or validate client-supplied IDs themselves.
//
// Prefixes are copied. Invalid, zoned, or IPv4-mapped IPv6 prefixes are rejected;
// use native IPv4 prefixes for mapped peers. Both the response header and a cloned
// downstream request header receive the chosen ID. The original request is not
// mutated. This middleware does not authorize, log, trace, or recover panics.
func NewRequestIDMiddleware(trustedProxies ...netip.Prefix) (func(http.Handler) http.Handler, error) {
	for _, prefix := range trustedProxies {
		if !prefix.IsValid() || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
			return nil, errors.New("observability: invalid trusted proxy prefix")
		}
	}
	trustedProxies = slices.Clone(trustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := ""
			if trustedPeer(r.RemoteAddr, trustedProxies) {
				id = incomingRequestID(r.Header)
			}
			if id == "" {
				id = rand.Text()
			}

			r = r.Clone(context.WithValue(r.Context(), requestIDKey{}, id))
			r.Header = withRequestID(r.Header, id)
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r)
		})
	}, nil
}

// trustedPeer reports whether the direct peer address is in a trusted prefix.
func trustedPeer(remoteAddr string, trustedProxies []netip.Prefix) bool {
	peer, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return false
	}
	addr := peer.Addr().Unmap().WithZone("")
	return slices.ContainsFunc(trustedProxies, func(prefix netip.Prefix) bool {
		return prefix.Contains(addr)
	})
}

// withRequestID sets the ID on a cloned header, removing differently cased
// duplicates too, including in in-process requests.
func withRequestID(header http.Header, id string) http.Header {
	if header == nil {
		header = make(http.Header)
	}
	for key := range header {
		if strings.EqualFold(key, RequestIDHeader) {
			delete(header, key)
		}
	}
	header.Set(RequestIDHeader, id)
	return header
}

// incomingRequestID rejects ambiguous or unsafe correlation values, not requests.
func incomingRequestID(header http.Header) string {
	var values []string
	for key, candidates := range header {
		if strings.EqualFold(key, RequestIDHeader) {
			values = append(values, candidates...)
		}
	}
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > 128 {
		return ""
	}
	for _, c := range values[0] {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return ""
		}
	}
	return values[0]
}
