package term

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// Reading a machine's SSH host key without logging in — what ssh-keyscan does,
// and what a "the key changed, is that you?" screen needs before it can show
// anyone the fingerprint they are being asked to trust.
//
// It stops at the key exchange. No credential is offered and no authentication
// is attempted, so probing a host that HomeHub cannot log into still works,
// and nothing lands in the host's auth log.

// errProbeDone aborts the handshake once the key is in hand. Returning an
// error from the callback is how the library is told to stop; this one means
// success, so ProbeHostKey unwraps it rather than reporting it.
var errProbeDone = errors.New("host key captured")

// ProbeHostKey returns the SHA256 fingerprint the machine at host:port is
// presenting right now.
func ProbeHostKey(ctx context.Context, host string, port int) (string, error) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	dialer := net.Dialer{Timeout: 10 * time.Second}
	tcp, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("cannot reach %s: %w", addr, err)
	}
	defer tcp.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = tcp.SetDeadline(deadline)
	} else {
		_ = tcp.SetDeadline(time.Now().Add(15 * time.Second))
	}

	fingerprint := ""
	_, _, _, err = ssh.NewClientConn(tcp, addr, &ssh.ClientConfig{
		// A user with no auth methods: the handshake is aborted from the
		// callback before this matters, and it never gets as far as a login.
		User: "homehub-probe",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			return errProbeDone
		},
		Timeout: 10 * time.Second,
	})
	if fingerprint != "" {
		return fingerprint, nil
	}
	return "", fmt.Errorf("no host key from %s: %w", addr, err)
}
