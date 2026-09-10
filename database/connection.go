package database

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// Dialect identifies a database family, not a registered SQL driver or ORM.
type Dialect string

const (
	// MySQL identifies MySQL-compatible databases.
	MySQL Dialect = "mysql"
	// PostgreSQL identifies PostgreSQL-compatible databases.
	PostgreSQL Dialect = "postgres"
	// SQLite identifies SQLite databases.
	SQLite Dialect = "sqlite"
)

var (
	// ErrInvalidURL indicates malformed or ambiguous connection input.
	ErrInvalidURL = errors.New("database: invalid connection URL")
	// ErrUnsupportedScheme indicates an unsupported URL scheme.
	ErrUnsupportedScheme = errors.New("database: unsupported connection URL scheme")
)

// Connection holds sensitive driver input. Only DataSourceName reveals it;
// formatting, structured logging, text, and JSON encoding are redacted.
// The zero value is not a usable connection. Parsing has no external effects.
type Connection struct {
	dialect Dialect
	dsn     string
}

// Dialect returns the database family; callers select and register their driver.
func (c Connection) Dialect() Dialect { return c.dialect }

// DataSourceName explicitly exposes sensitive input for database.Open. Do not log it.
func (c Connection) DataSourceName() string { return c.dsn }

// String returns a fixed redacted representation, including for the zero value.
func (c Connection) String() string { return "database.Connection([redacted])" }

// Format prevents debug formatting such as %#v from exposing private fields.
func (c Connection) Format(state fmt.State, _ rune) { _, _ = io.WriteString(state, c.String()) }

// LogValue prevents structured log handlers from inspecting sensitive fields.
func (c Connection) LogValue() slog.Value { return slog.StringValue(c.String()) }

// MarshalText provides a redacted representation for text-based encoders.
func (c Connection) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// MarshalJSON provides a redacted representation for both Go JSON encoders.
func (c Connection) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(c.String())), nil }

// ParseURL interprets mysql:// TCP URLs, postgres:// and postgresql:// URIs,
// SQLite file: URIs, and sqlite:// path shorthand. Native DSNs remain supported
// by Open directly. No drivers are imported, connections opened, paths resolved,
// directories created, or environment variables read. Duplicate query keys,
// fragments, control characters, and ambiguous authorities are rejected.
// Driver-specific option values are validated by the chosen driver, not here.
// Errors never contain input or wrap potentially sensitive parser errors.
func ParseURL(raw string) (Connection, error) {
	if raw == "" || strings.Contains(raw, "#") || hasControl(raw) {
		return Connection{}, ErrInvalidURL
	}
	// sqlite:// is a path shorthand, not an authority: relative paths stay relative.
	input := raw
	if path, ok := strings.CutPrefix(raw, "sqlite://"); ok {
		input = "file:" + path
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme == "" {
		return Connection{}, ErrInvalidURL
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Connection{}, ErrInvalidURL
	}
	for key, values := range q {
		if key == "" || len(values) != 1 || hasControl(key) || hasControl(values[0]) {
			return Connection{}, ErrInvalidURL
		}
	}
	decoded, err := url.PathUnescape(u.EscapedPath() + u.Opaque)
	if err != nil || hasControl(decoded) {
		return Connection{}, ErrInvalidURL
	}
	switch u.Scheme {
	case "file":
		if u.User != nil || (u.Host != "" && u.Host != "localhost") || (u.Path == "" && u.Opaque == "") {
			return Connection{}, ErrInvalidURL
		}
		return Connection{dialect: SQLite, dsn: input}, nil
	case "mysql", "postgres", "postgresql":
		if u.Opaque != "" || !strings.Contains(input, "://") || !validAuthority(u) {
			return Connection{}, ErrInvalidURL
		}
		if u.User != nil {
			password, _ := u.User.Password()
			if hasControl(u.User.Username()) || hasControl(password) {
				return Connection{}, ErrInvalidURL
			}
		}
		if u.Scheme != "mysql" {
			return Connection{dialect: PostgreSQL, dsn: raw}, nil
		}
		return mysqlConnection(u, q)
	default:
		return Connection{}, ErrUnsupportedScheme
	}
}

// validAuthority bounds the URL API to a single host; native multi-host/socket
// configurations can still be passed directly to Open in their driver format.
func validAuthority(u *url.URL) bool {
	host := u.Hostname()
	if hasControl(host) || strings.ContainsAny(host, ",()/\\ ") || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if strings.Contains(host, ":") || strings.HasPrefix(u.Host, "[") {
		if _, err := netip.ParseAddr(host); err != nil || !strings.HasPrefix(u.Host, "[") {
			return false
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || host == "" {
			return false
		}
	}
	return true
}

// mysqlConnection implements the driver's TCP DSN grammar without registering
// the driver. Usernames containing ':' cannot be represented without ambiguity.
func mysqlConnection(u *url.URL, q url.Values) (Connection, error) {
	if u.Hostname() == "" {
		return Connection{}, ErrInvalidURL
	}
	credentials := ""
	if u.User != nil {
		user := u.User.Username()
		password, hasPassword := u.User.Password()
		if strings.Contains(user, ":") || (user == "" && password != "") {
			return Connection{}, ErrInvalidURL
		}
		credentials = user
		if hasPassword {
			credentials += ":" + password
		}
		credentials += "@"
	}
	port := u.Port()
	if port == "" {
		port = "3306"
	}
	dsn := credentials + "tcp(" + net.JoinHostPort(u.Hostname(), port) + ")/" + url.PathEscape(strings.TrimPrefix(u.Path, "/"))
	if len(q) != 0 {
		dsn += "?" + q.Encode()
	}
	return Connection{dialect: MySQL, dsn: dsn}, nil
}

// hasControl rejects embedded control bytes before passing input to a driver.
func hasControl(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
