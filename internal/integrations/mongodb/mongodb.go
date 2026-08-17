// Package mongodb implements the MongoDB integration over the official
// driver: server health, databases and collections, live operations, and
// op-rate history.
package mongodb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"homehub/internal/integration"
)

const TypeID = "mongodb"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "MongoDB" }
func (Driver) Description() string {
	return "MongoDB servers: databases and collections with live stats, current operations, connection and op-rate history, and dumps to your backup targets."
}
func (Driver) SecretNames() []string { return []string{"username", "password"} }

// buildURI assembles the connection string from config + secrets.
func buildURI(config map[string]any, secrets map[string]string) (string, error) {
	host, _ := config["host"].(string)
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errors.New("host is required, e.g. mongo.lan:27017 or 192.168.1.7")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "27017")
	}
	u := &url.URL{Scheme: "mongodb", Host: host, Path: "/"}
	query := url.Values{}
	if secrets["username"] != "" {
		u.User = url.UserPassword(secrets["username"], secrets["password"])
		authSource, _ := config["authSource"].(string)
		if authSource == "" {
			authSource = "admin"
		}
		query.Set("authSource", authSource)
	}
	if tlsOn, _ := config["tls"].(bool); tlsOn {
		query.Set("tls", "true")
	}
	if insecure, _ := config["insecureTls"].(bool); insecure {
		query.Set("tls", "true")
		query.Set("tlsInsecure", "true")
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func connect(ctx context.Context, config map[string]any, secrets map[string]string) (*mongo.Client, error) {
	uri, err := buildURI(config, secrets)
	if err != nil {
		return nil, err
	}
	opts := options.Client().ApplyURI(uri).
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(10 * time.Second).
		SetMaxPoolSize(4)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return client, nil
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "auth error") || strings.Contains(msg, "Authentication failed"):
		return "authentication failed — check the username, password, and auth database (admin by default)"
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and port (27017 by default)"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for self-signed setups (" + msg + ")"
	case strings.Contains(msg, "server selection error") || strings.Contains(msg, "context deadline exceeded"):
		return "connection timed out — check the address and any firewalls"
	}
	return msg
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	client, err := connect(ctx, config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	c := &Conn{client: client}
	stats, err := c.Stats(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	dbs, _ := c.Databases(ctx)
	return integration.TestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected to MongoDB %s — %d databases, %d current connections", stats.Version, len(dbs), stats.ConnectionsCurrent),
		Detail:  map[string]any{"version": stats.Version},
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
	client, err := connect(ctx, inst.Config, map[string]string{"username": user, "password": pass})
	if err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return &Conn{inst: inst, client: client}, nil
}
