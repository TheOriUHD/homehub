// Package docker implements the multi-host Docker integration driver.
// Hosts connect over the local socket, TCP with mutual TLS, or SSH (running
// `docker system dial-stdio` on the remote host, authenticated with
// credentials from the vault).
package docker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	dockerclient "github.com/docker/docker/client"

	"github.com/TheOriUHD/homehub/internal/integration"
)

const TypeID = "docker"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Docker" }
func (Driver) Description() string {
	return "Manage Docker hosts: containers, images, volumes, networks, logs, shells, and compose stacks."
}
func (Driver) SecretNames() []string {
	return []string{"caCert", "clientCert", "clientKey", "sshSecret"}
}

type config struct {
	Transport  string // socket | tcp | ssh
	SocketPath string
	Host       string
	Port       int
	Username   string
	AuthType   string // password | key (ssh)
	TLS        bool
}

func parseConfig(raw map[string]any) (config, error) {
	str := func(key string) string {
		v, _ := raw[key].(string)
		return strings.TrimSpace(v)
	}
	cfg := config{
		Transport:  str("transport"),
		SocketPath: str("socketPath"),
		Host:       str("host"),
		Username:   str("username"),
		AuthType:   str("authType"),
	}
	if v, ok := raw["port"].(float64); ok {
		cfg.Port = int(v)
	}
	if v, ok := raw["tls"].(bool); ok {
		cfg.TLS = v
	}
	switch cfg.Transport {
	case "socket":
		if cfg.SocketPath == "" {
			cfg.SocketPath = "/var/run/docker.sock"
		}
	case "tcp":
		if cfg.Host == "" {
			return cfg, errors.New("host is required for TCP, e.g. 192.168.1.20:2376")
		}
		if !strings.Contains(cfg.Host, ":") {
			if cfg.TLS {
				cfg.Host += ":2376"
			} else {
				cfg.Host += ":2375"
			}
		}
	case "ssh":
		if cfg.Host == "" {
			return cfg, errors.New("host is required for SSH")
		}
		if cfg.Username == "" {
			return cfg, errors.New("SSH username is required")
		}
		if cfg.Port == 0 {
			cfg.Port = 22
		}
		if cfg.AuthType == "" {
			cfg.AuthType = "password"
		}
	default:
		return cfg, errors.New("transport must be socket, tcp, or ssh")
	}
	return cfg, nil
}

// buildClient constructs the SDK client for a transport.
func buildClient(cfg config, secrets map[string]string) (*dockerclient.Client, error) {
	opts := []dockerclient.Opt{dockerclient.WithAPIVersionNegotiation()}

	switch cfg.Transport {
	case "socket":
		opts = append(opts, dockerclient.WithHost("unix://"+cfg.SocketPath))
	case "tcp":
		opts = append(opts, dockerclient.WithHost("tcp://"+cfg.Host))
		if cfg.TLS {
			tlsConfig := &tls.Config{}
			if ca := secrets["caCert"]; ca != "" {
				pool := x509.NewCertPool()
				if !pool.AppendCertsFromPEM([]byte(ca)) {
					return nil, errors.New("CA certificate is not valid PEM")
				}
				tlsConfig.RootCAs = pool
			}
			if secrets["clientCert"] != "" || secrets["clientKey"] != "" {
				pair, err := tls.X509KeyPair([]byte(secrets["clientCert"]), []byte(secrets["clientKey"]))
				if err != nil {
					return nil, fmt.Errorf("client certificate/key not usable: %w", err)
				}
				tlsConfig.Certificates = []tls.Certificate{pair}
			}
			opts = append(opts, dockerclient.WithHTTPClient(&http.Client{
				Transport: &http.Transport{
					TLSClientConfig: tlsConfig,
					DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				},
			}))
		}
	case "ssh":
		dialer := &sshDialer{
			addr:     net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)),
			username: cfg.Username,
			authType: cfg.AuthType,
			secret:   secrets["sshSecret"],
		}
		opts = append(opts,
			dockerclient.WithHost("http://docker.via.ssh"),
			dockerclient.WithDialContext(dialer.DialContext),
		)
	}
	return dockerclient.NewClientWithOpts(opts...)
}

func (Driver) Test(ctx context.Context, raw map[string]any, secrets map[string]string) integration.TestResult {
	cfg, err := parseConfig(raw)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	cli, err := buildClient(cfg, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	defer cli.Close()

	info, err := cli.Info(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(cfg, err)}
	}
	version, _ := cli.ServerVersion(ctx)
	msg := fmt.Sprintf("Connected to Docker %s on %q — %d containers (%d running), %d images",
		version.Version, info.Name, info.Containers, info.ContainersRunning, info.Images)
	return integration.TestResult{
		OK:      true,
		Message: msg,
		Detail: map[string]any{
			"version": version.Version, "os": info.OperatingSystem,
			"containers": info.Containers, "running": info.ContainersRunning,
		},
	}
}

func friendlyError(cfg config, err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "permission denied") && cfg.Transport == "socket":
		return "permission denied on the Docker socket — HomeHub's user needs access (mount the socket into the container, or add the user to the docker group)"
	case strings.Contains(msg, "no such file") && cfg.Transport == "socket":
		return fmt.Sprintf("socket %s does not exist — is Docker running, and is the socket mounted into HomeHub's container?", cfg.SocketPath)
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS handshake failed — check the CA and client certificate/key (" + msg + ")"
	case strings.Contains(msg, "connection refused"):
		return "connection refused — is the Docker API listening on this address, and reachable from HomeHub?"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "ssh:") || strings.Contains(msg, "handshake failed"):
		return "SSH connection failed — check host, port, username, and credentials (" + msg + ")"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "timeout"):
		return "connection timed out — check the address and any firewalls"
	case strings.Contains(msg, "dial-stdio"):
		return "the remote host accepted SSH but `docker system dial-stdio` failed — is the docker CLI installed for that user?"
	}
	return msg
}

// FriendlyError is used by API handlers to surface upstream failures.
func FriendlyError(err error) string {
	return friendlyError(config{}, err)
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	cfg, err := parseConfig(inst.Config)
	if err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	for _, name := range d.SecretNames() {
		if v, serr := deps.Secret(ctx, name); serr == nil {
			secrets[name] = v
		}
	}
	cli, err := buildClient(cfg, secrets)
	if err != nil {
		return nil, err
	}
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, errors.New(friendlyError(cfg, err))
	}
	conn := &Conn{
		Client: cli,
		inst:   inst,
		deps:   deps,
		stop:   make(chan struct{}),
	}
	go conn.watchEvents()
	return conn, nil
}
