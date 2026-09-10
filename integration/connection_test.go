package integration_test

import (
	"testing"

	"github.com/fuf-stack/hardcore/database"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestIntegrationMySQLURLContract checks conversion using the real driver parser,
// without a server. Driver imports remain confined to this test-only module.
func TestIntegrationMySQLURLContract(t *testing.T) {
	tests := []struct{ name, raw, user, password, addr, db string }{
		// Default TCP port and a database name need no injected driver options.
		{"defaults", "mysql://host/db", "", "", "host:3306", "db"},
		// Reserved characters in credentials and database names must round-trip.
		{"escaping", "mysql://u%40x:p%40%3A%2F%3F%23@[::1]:3307/a%2Fb%3Fc?parseTime=false&loc=UTC", "u@x", "p@:/?#", "[::1]:3307", "a/b?c"},
		// A username without a password must not become protocol input.
		{"user", "mysql://u@host/db", "u", "", "host:3306", "db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := database.ParseURL(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := mysql.ParseDSN(c.DataSourceName())
			if err != nil {
				t.Fatal("driver rejected synthetic DSN")
			}
			if cfg.User != tt.user || cfg.Passwd != tt.password || cfg.Addr != tt.addr || cfg.DBName != tt.db || cfg.Net != "tcp" || cfg.ParseTime {
				t.Fatal("driver interpretation differs from URL contract")
			}
		})
	}
}

// TestIntegrationPostgresURLContract verifies preservation using pgx's parser.
func TestIntegrationPostgresURLContract(t *testing.T) {
	c, err := database.ParseURL("postgresql://u:p%40ss@localhost:5433/a%2Fb?sslmode=disable&application_name=contract")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgconn.ParseConfig(c.DataSourceName())
	if err != nil {
		t.Fatal("pgx rejected synthetic URI")
	}
	if cfg.User != "u" || cfg.Password != "p@ss" || cfg.Port != 5433 || cfg.Database != "a/b" || cfg.RuntimeParams["application_name"] != "contract" {
		t.Fatal("pgx interpretation differs from URL contract")
	}
}
