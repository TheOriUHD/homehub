// Package dbsql implements the MySQL/MariaDB integration: one driver for
// both engines (they share the wire protocol), flavor-detected from the
// server version string.
package dbsql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/TheOriUHD/homehub/internal/integration"
)

const TypeID = "mysql"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "MySQL / MariaDB" }
func (Driver) Description() string {
	return "MySQL and MariaDB servers: databases and tables, live sessions, server health and query-rate history, an audited SQL console, and dumps to your backup targets."
}
func (Driver) SecretNames() []string { return []string{"username", "password"} }

// openPool builds a pooled connection from config + secrets.
func openPool(config map[string]any, secrets map[string]string) (*sql.DB, error) {
	host, _ := config["host"].(string)
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("host is required, e.g. db.lan:3306 or 192.168.1.5")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "3306")
	}
	if secrets["username"] == "" {
		return nil, errors.New("a username is required — a dedicated monitoring user with PROCESS and SELECT on information_schema works best")
	}
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = host
	cfg.User = secrets["username"]
	cfg.Passwd = secrets["password"]
	cfg.Timeout = 10 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	if insecure, _ := config["insecureTls"].(bool); insecure {
		cfg.TLSConfig = "skip-verify"
	} else if tlsOn, _ := config["tls"].(bool); tlsOn {
		cfg.TLSConfig = "true"
	}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Access denied"):
		return "access denied — check the username and password"
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and port (3306 by default)"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for self-signed setups (" + msg + ")"
	case strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "context deadline exceeded"):
		return "connection timed out — check the address and any firewalls"
	}
	return msg
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	db, err := openPool(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	defer db.Close()
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	var dbCount int
	_ = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema','performance_schema','mysql','sys')").
		Scan(&dbCount)
	flavor := "MySQL"
	if strings.Contains(strings.ToLower(version), "mariadb") {
		flavor = "MariaDB"
	}
	return integration.TestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected to %s %s — %d user databases", flavor, version, dbCount),
		Detail:  map[string]any{"version": version, "flavor": flavor},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	user, err := deps.Secret(ctx, "username")
	if err != nil {
		return nil, fmt.Errorf("reading the username: %w", err)
	}
	pass, err := deps.Secret(ctx, "password")
	if err != nil {
		return nil, fmt.Errorf("reading the password: %w", err)
	}
	db, err := openPool(inst.Config, map[string]string{"username": user, "password": pass})
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		db.Close()
		return nil, errors.New(friendlyError(err))
	}
	return &Conn{inst: inst, db: db}, nil
}
