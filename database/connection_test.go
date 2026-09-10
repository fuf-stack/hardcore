package database_test

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/fuf-stack/hardcore/database"
)

// TestParseURL verifies deterministic conversion without drivers or databases.
func TestParseURL(t *testing.T) {
	tests := []struct {
		name, raw, want string
		dialect         database.Dialect
	}{
		// IPv6 brackets and an explicit port survive TCP conversion.
		{"mysql_ipv6", "mysql://u:p@[::1]:3307/db", "u:p@tcp([::1]:3307)/db", database.MySQL},
		// No credentials and no port use only the protocol's default port.
		{"mysql_defaults", "mysql://host/db", "tcp(host:3306)/db", database.MySQL},
		// URL escaping must not become structural delimiters in the DSN database name.
		{"mysql_escaping", "mysql://u:p%40%3A%2F@host/a%2Fb%3Fc?loc=Europe%2FBerlin&parseTime=false", "u:p@:/@tcp(host:3306)/a%2Fb%3Fc?loc=Europe%2FBerlin&parseTime=false", database.MySQL},
		// User-only credentials need no implicit password.
		{"mysql_user", "mysql://u@host/db", "u@tcp(host:3306)/db", database.MySQL},
		// Empty passwords remain representable without adding application defaults.
		{"mysql_empty_password", "mysql://u:@host/db", "u:@tcp(host:3306)/db", database.MySQL},
		// Native PostgreSQL options and encoding remain byte-for-byte unchanged.
		{"postgres", "postgres://u:p@host/db?sslmode=require", "postgres://u:p@host/db?sslmode=require", database.PostgreSQL},
		// The long PostgreSQL scheme maps to the same dialect.
		{"postgresql", "postgresql://host/db", "postgresql://host/db", database.PostgreSQL},
		// Hostless PostgreSQL URIs may rely on driver configuration.
		{"postgres_hostless", "postgres:///db", "postgres:///db", database.PostgreSQL},
		// Relative shorthand must not resolve against the current working directory.
		{"sqlite_relative", "sqlite://./relative.db", "file:./relative.db", database.SQLite},
		// Absolute shorthand retains encoded filenames and read-only options.
		{"sqlite_absolute", "sqlite:///tmp/a%20b.db?mode=ro", "file:/tmp/a%20b.db?mode=ro", database.SQLite},
		// Memory URIs must not be converted into persistent file paths.
		{"sqlite_memory", "file::memory:?cache=shared", "file::memory:?cache=shared", database.SQLite},
		// Native URI authority and options are preserved, including localhost.
		{"sqlite_localhost", "file://localhost/tmp/a.db?mode=ro", "file://localhost/tmp/a.db?mode=ro", database.SQLite},
		// Named in-memory databases retain the caller's mode.
		{"sqlite_named_memory", "file:memdb?mode=memory&cache=shared", "file:memdb?mode=memory&cache=shared", database.SQLite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := database.ParseURL(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got.Dialect() != tt.dialect || got.DataSourceName() != tt.want {
				t.Fatalf("unexpected dialect or DSN for synthetic fixture %s", tt.name)
			}
		})
	}
}

// TestParseURLRejectsAmbiguity verifies safe, classifiable errors and zero results.
func TestParseURLRejectsAmbiguity(t *testing.T) {
	tests := []struct {
		name, raw string
		want      error
	}{
		// Empty input is not an implicit default connection.
		{"empty", "", database.ErrInvalidURL},
		// Native DSNs belong in Open, not in the URL parser.
		{"no_scheme", "host=db password=secret", database.ErrInvalidURL},
		// Raw control characters must not reach a driver.
		{"control", "postgres://host/db\n", database.ErrInvalidURL},
		// Fragments would silently truncate connection information.
		{"fragment", "postgres://host/db#secret", database.ErrInvalidURL},
		// Invalid URL escapes must not leak the standard parser's raw-input error.
		{"bad_escape", "mysql://u:secret%ZZ@host/db", database.ErrInvalidURL},
		// Malformed query escapes must not be discarded.
		{"bad_query", "mysql://host/db?x=%ZZ", database.ErrInvalidURL},
		// Duplicate options are rejected rather than relying on driver precedence.
		{"duplicate", "file:a?mode=ro&mode=rwc", database.ErrInvalidURL},
		// Empty query keys are not meaningful driver options.
		{"empty_key", "file:a?=secret", database.ErrInvalidURL},
		// Decoded controls in keys are equally unsafe.
		{"query_key_control", "file:a?%00=x", database.ErrInvalidURL},
		// Decoded controls in values are rejected before driver parsing.
		{"query_value_control", "file:a?x=%00", database.ErrInvalidURL},
		// Opaque file paths require escape validation too.
		{"opaque_escape", "file:a%ZZ", database.ErrInvalidURL},
		// Encoded path controls must not become filenames.
		{"path_control", "file:a%00", database.ErrInvalidURL},
		// SQLite remote authorities are outside the supported local-file contract.
		{"remote_file", "file://remote/a", database.ErrInvalidURL},
		// SQLite does not accept URL credentials.
		{"file_user", "file://u:secret@localhost/a", database.ErrInvalidURL},
		// A missing filename is not interpreted as a temporary database.
		{"file_empty", "sqlite://", database.ErrInvalidURL},
		// Network URLs require hierarchical syntax.
		{"opaque_network", "postgres:secret", database.ErrInvalidURL},
		// Missing authority delimiters are not accepted for network URLs.
		{"network_slash", "postgres:/db", database.ErrInvalidURL},
		// Multi-host configurations remain a native-driver escape hatch.
		{"multi_host", "postgres://a,b/db", database.ErrInvalidURL},
		// Parentheses cannot be allowed to alter the generated TCP grammar.
		{"host_delimiter", "mysql://a(b)/db", database.ErrInvalidURL},
		// Empty explicit ports are rejected, not replaced with defaults.
		{"empty_port", "mysql://host:/db", database.ErrInvalidURL},
		// Invalid IP literals are not accepted as IPv6 addresses.
		{"bad_ipv6", "mysql://[not:ipv6]/db", database.ErrInvalidURL},
		// Brackets are reserved for IP literals, not DNS hostnames.
		{"bracketed_dns", "mysql://[host]/db", database.ErrInvalidURL},
		// Unbracketed IPv6 is ambiguous in a URL authority.
		{"unbracketed_ipv6", "mysql://::1/db", database.ErrInvalidURL},
		// Ports must fit the TCP range.
		{"large_port", "mysql://host:65536/db", database.ErrInvalidURL},
		// Port zero is not a remote service endpoint.
		{"zero_port", "mysql://host:0/db", database.ErrInvalidURL},
		// Ports without a hostname are ambiguous.
		{"port_without_host", "postgres://:5432/db", database.ErrInvalidURL},
		// Encoded credential controls must not reach a driver.
		{"credential_control", "mysql://u:secret%00@host/db", database.ErrInvalidURL},
		// MySQL requires an explicit host in this TCP URL subset.
		{"mysql_hostless", "mysql:///db", database.ErrInvalidURL},
		// A colon in a username would be read as a password delimiter.
		{"username_colon", "mysql://u%3Ax:secret@host/db", database.ErrInvalidURL},
		// The driver cannot represent a password with an empty username reliably.
		{"password_without_user", "mysql://:secret@host/db", database.ErrInvalidURL},
		// Unrecognized aliases must not silently select a driver.
		{"alias", "mysql+unknown://u:secret@host/db", database.ErrUnsupportedScheme},
		// Unknown schemes are reported without reproducing any input.
		{"unsupported", "unknown://u:secret@host/db", database.ErrUnsupportedScheme},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := database.ParseURL(tt.raw)
			if !errors.Is(err, tt.want) || err != tt.want {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Dialect() != "" || got.DataSourceName() != "" {
				t.Fatal("failure returned sensitive partial result")
			}
		})
	}
}

// TestParseURLDoesNotCreateFiles proves that path interpretation stays pure.
func TestParseURLDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := database.ParseURL("sqlite://nested/missing.db?mode=ro"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("parsing changed the filesystem")
	}
}

// TestConnectionRedaction covers text, debug, JSON, and structured log boundaries.
func TestConnectionRedaction(t *testing.T) {
	c, err := database.ParseURL("postgres://user:secret@host/private?password=secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		// Every supported fmt verb must go through the redaction boundary.
		if got := fmt.Sprintf(format, c); got != c.String() {
			t.Fatal("unsafe formatted connection")
		}
	}
	// Legacy and current JSON encoders must not inspect the sensitive fields.
	for _, marshal := range []func(any) ([]byte, error){jsonv1.Marshal, func(v any) ([]byte, error) { return json.Marshal(v) }} {
		got, err := marshal(c)
		if err != nil || string(got) != `"database.Connection([redacted])"` {
			t.Fatal("unsafe JSON connection")
		}
	}
	// Text encoding must use the same safe representation.
	text, err := c.MarshalText()
	if err != nil || string(text) != c.String() {
		t.Fatal("unsafe text connection")
	}
	// Structured logging must retain a useful marker but no credential or path.
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("connection", "config", c)
	if bytes.Contains(logs.Bytes(), []byte("secret")) || !bytes.Contains(logs.Bytes(), []byte("[redacted]")) {
		t.Fatal("unsafe log connection")
	}
	// The zero value must also be safe to print.
	if (database.Connection{}).String() != c.String() {
		t.Fatal("unsafe zero connection")
	}
}

// FuzzParseURL checks that arbitrary malformed input never panics or leaks into errors.
func FuzzParseURL(f *testing.F) {
	f.Add("mysql://u:p@host/db")
	f.Add("file::memory:?cache=shared")
	f.Add("postgres://u:secret%ZZ@host/db")
	f.Fuzz(func(t *testing.T, raw string) {
		c, err := database.ParseURL(raw)
		if err != nil && err != database.ErrInvalidURL && err != database.ErrUnsupportedScheme {
			t.Fatal("non-sentinel parser error")
		}
		if err != nil && c.DataSourceName() != "" {
			t.Fatal("partial result on failure")
		}
		if fmt.Sprintf("%#v", c) != "database.Connection([redacted])" {
			t.Fatal("unsafe debug output")
		}
	})
}
