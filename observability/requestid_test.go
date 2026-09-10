package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/fuf-stack/hardcore/observability"
)

// TestRequestIDPolicy verifies trusted peers, strict values, and spoof resistance.
func TestRequestIDPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, remote string
		trusted      []netip.Prefix
		header       http.Header
		want         string
	}{
		// Direct client headers never establish trust by default.
		{"untrusted", "192.0.2.1:80", nil, http.Header{"X-Request-Id": {"client-id"}}, ""},
		// A configured direct proxy can propagate a safe ID.
		{"trusted", "192.0.2.1:80", []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, http.Header{"X-Request-Id": {"proxy-ID_1.2"}}, "proxy-ID_1.2"},
		// A nonmatching configured prefix must not authorize the peer.
		{"different_peer", "198.51.100.1:80", []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, http.Header{"X-Request-Id": {"client-id"}}, ""},
		// IPv4-mapped peers match native IPv4 configuration.
		{"mapped_peer", "[::ffff:192.0.2.1]:80", []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, http.Header{"X-Request-Id": {"proxy-id"}}, "proxy-id"},
		// Native IPv6 peers can also be trusted explicitly.
		{"ipv6", "[2001:db8::1]:80", []netip.Prefix{netip.MustParsePrefix("2001:db8::/32")}, http.Header{"X-Request-Id": {"proxy-id"}}, "proxy-id"},
		// Malformed peer addresses fail closed even with a broad trust prefix.
		{"malformed_peer", "not-an-address", []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}, http.Header{"X-Request-Id": {"client-id"}}, ""},
		// Forwarded headers cannot impersonate the immediate peer.
		{"forwarded_spoof", "198.51.100.1:80", []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, http.Header{"X-Request-Id": {"client-id"}, "X-Forwarded-For": {"192.0.2.1"}}, ""},
		// Missing request headers must still produce a usable ID.
		{"nil_headers", "192.0.2.1:80", nil, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			middleware, err := observability.NewRequestIDMiddleware(tc.trusted...)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr, r.Header = tc.remote, tc.header
			before := r.Header.Clone()
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			r = r.WithContext(ctx)
			var got string
			h := middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				got = observability.RequestID(req.Context())
				if got == "" || req.Header.Get(observability.RequestIDHeader) != got || req.Context().Err() != context.Canceled {
					t.Fatal("missing correlation or lost cancellation")
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusAccepted || w.Header().Get(observability.RequestIDHeader) != got {
				t.Fatal("response changed or ID missing")
			}
			if tc.want != "" && got != tc.want {
				t.Fatalf("ID = %q; want %q", got, tc.want)
			}
			if tc.want == "" && (len(got) < 26 || got == r.Header.Get(observability.RequestIDHeader)) {
				t.Fatal("expected fresh random ID")
			}
			if !reflect.DeepEqual(r.Header, before) || observability.RequestID(r.Context()) != "" {
				t.Fatal("original request mutated")
			}
		})
	}
}

// TestIncomingIDValidation rejects ambiguous and unsafe values from trusted peers.
func TestIncomingIDValidation(t *testing.T) {
	middleware, err := observability.NewRequestIDMiddleware(netip.MustParsePrefix("192.0.2.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		header http.Header
		want   string
	}{
		// Absent values are replaced rather than rejecting the request.
		{"missing", http.Header{}, ""},
		// Empty values cannot act as correlation identifiers.
		{"empty", http.Header{"X-Request-Id": {""}}, ""},
		// Multiple values must not be resolved by arbitrary selection.
		{"duplicate", http.Header{"X-Request-Id": {"a", "b"}}, ""},
		// Differently cased map keys must not evade duplicate detection.
		{"duplicate_case", http.Header{"X-Request-Id": {"a"}, "x-request-id": {"b"}}, ""},
		// Comma-joined values are also ambiguous.
		{"comma", http.Header{"X-Request-Id": {"a,b"}}, ""},
		// Whitespace and control characters must not reach log fields.
		{"control", http.Header{"X-Request-Id": {"a\nb"}}, ""},
		// Unicode is outside the deliberately narrow wire alphabet.
		{"unicode", http.Header{"X-Request-Id": {"café"}}, ""},
		// Excessive values are replaced without reflecting them.
		{"too_long", http.Header{"X-Request-Id": {strings.Repeat("a", 129)}}, ""},
		// The inclusive length boundary remains accepted.
		{"max_length", http.Header{"X-Request-Id": {strings.Repeat("a", 128)}}, strings.Repeat("a", 128)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr, r.Header = "192.0.2.1:80", tc.header
			w := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				id := observability.RequestID(req.Context())
				if tc.want != "" && id != tc.want {
					t.Fatalf("ID = %q; want %q", id, tc.want)
				}
				if tc.want == "" && (id == "" || id == tc.header.Get(observability.RequestIDHeader)) {
					t.Fatal("invalid ID retained")
				}
				if len(req.Header.Values(observability.RequestIDHeader)) != 1 {
					t.Fatal("ambiguous downstream header")
				}
			})).ServeHTTP(w, r)
		})
	}
}

// TestProxyConfiguration rejects unusable prefixes and isolates caller mutations.
func TestProxyConfiguration(t *testing.T) {
	for _, prefix := range []netip.Prefix{
		// An invalid prefix must fail configuration instead of silently weakening it.
		{},
		// Mapped prefix notation is rejected in favor of native IPv4 notation.
		netip.MustParsePrefix("::ffff:192.0.2.0/120"),
	} {
		if middleware, err := observability.NewRequestIDMiddleware(prefix); err == nil || middleware != nil {
			t.Fatal("invalid prefix accepted")
		}
	}
	prefixes := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
	middleware, err := observability.NewRequestIDMiddleware(prefixes...)
	if err != nil {
		t.Fatal(err)
	}
	prefixes[0] = netip.MustParsePrefix("198.51.100.0/24")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.1:80"
	r.Header.Set(observability.RequestIDHeader, "retained-policy")
	w := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)
	if w.Header().Get(observability.RequestIDHeader) != "retained-policy" {
		t.Fatal("caller mutation changed proxy policy")
	}
}
