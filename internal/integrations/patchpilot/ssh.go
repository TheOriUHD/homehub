package patchpilot

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ptyStdinDelay is how long to wait before writing a password to a su/sudo
// PTY — su reads the terminal, not a pipe, and misses input sent too early.
const ptyStdinDelay = 400 * time.Millisecond

// syncBuffer is a mutex-guarded bytes.Buffer, safe for the concurrent
// stdout/stderr copier goroutines x/crypto/ssh runs.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// Credential is how we authenticate the FIRST connection to a host during
// onboarding: an operator password (or key) for a login account.
type Credential struct {
	User     string
	Password string
	Key      string // PEM private key (optional alternative to password)
}

// Conn is one live SSH connection with an escalation strategy chosen for it.
type SSHConn struct {
	client   *ssh.Client
	escalate EscalationMode
	password string // for sudo -S / su, when the mode needs it
	sourceIP string // local address used to reach the host (for from= pinning)
}

// EscalationMode is how a command is elevated to root. Chosen per §7.2.
type EscalationMode string

const (
	EscDirectRoot   EscalationMode = "direct-root"
	EscSudoNoPasswd EscalationMode = "sudo-nopasswd"
	EscSudoPassword EscalationMode = "sudo-password"
	EscSuRoot       EscalationMode = "su-root"
)

// dial opens a TCP+SSH connection with the given auth, ignoring host keys
// (LAN policy, matching HomeHub's other SSH bridges) and recording the local
// source IP for later authorized_keys pinning.
func dial(ctx context.Context, host string, port int, user string, auth []ssh.AuthMethod) (*ssh.Client, string, error) {
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	d := net.Dialer{Timeout: 10 * time.Second}
	tcp, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("could not reach %s: %w", addr, err)
	}
	sourceIP := ""
	if la, ok := tcp.LocalAddr().(*net.TCPAddr); ok {
		sourceIP = la.IP.String()
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	sc, chans, reqs, err := ssh.NewClientConn(tcp, addr, cfg)
	if err != nil {
		_ = tcp.Close()
		return nil, "", classifyDial(err)
	}
	return ssh.NewClient(sc, chans, reqs), sourceIP, nil
}

func classifyDial(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"), strings.Contains(msg, "no supported methods"):
		return fmt.Errorf("authentication failed — check the username and password")
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("connection refused — is SSH running on that port?")
	case strings.Contains(msg, "no such host"):
		return fmt.Errorf("host not found — check the address")
	case strings.Contains(msg, "i/o timeout"):
		return fmt.Errorf("connection timed out — check the address and firewall")
	}
	return err
}

// ConnectPassword opens a login session for onboarding with a password.
func ConnectPassword(ctx context.Context, host string, port int, cred Credential) (*SSHConn, error) {
	var auth []ssh.AuthMethod
	if k := strings.TrimSpace(cred.Key); k != "" {
		signer, err := ssh.ParsePrivateKey([]byte(k))
		if err != nil {
			return nil, fmt.Errorf("the SSH key could not be parsed: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cred.Password != "" {
		auth = append(auth, ssh.Password(cred.Password))
		auth = append(auth, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			ans := make([]string, len(questions))
			for i := range questions {
				ans[i] = cred.Password
			}
			return ans, nil
		}))
	}
	client, sourceIP, err := dial(ctx, host, port, cred.User, auth)
	if err != nil {
		return nil, err
	}
	return &SSHConn{client: client, password: cred.Password, sourceIP: sourceIP}, nil
}

// ConnectKey opens a session as the managed `patchpilot` account using the
// orchestrator private key — the transport every run uses.
func ConnectKey(ctx context.Context, host string, port int, user, privateKeyPEM string) (*SSHConn, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("orchestrator key unparseable: %w", err)
	}
	client, sourceIP, err := dial(ctx, host, port, user, []ssh.AuthMethod{ssh.PublicKeys(signer)})
	if err != nil {
		return nil, err
	}
	// Runs always escalate through the installed NOPASSWD sudoers.
	return &SSHConn{client: client, escalate: EscSudoNoPasswd, sourceIP: sourceIP}, nil
}

func (c *SSHConn) Close() {
	if c.client != nil {
		_ = c.client.Close()
	}
}

func (c *SSHConn) SourceIP() string { return c.sourceIP }

// Run executes one command and returns combined stdout+stderr plus the exit
// code. A non-zero exit is returned as (output, code, nil) — the caller
// decides whether that's an error — while transport failures return an error.
func (c *SSHConn) Run(ctx context.Context, command string) (string, int, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return "", -1, err
	}
	defer sess.Close()
	// Stdout and Stderr are copied by SEPARATE concurrent goroutines in
	// x/crypto/ssh, so a shared plain bytes.Buffer races (dropping output
	// intermittently). Guard it with a mutex, as CombinedOutput does.
	buf := &syncBuffer{}
	sess.Stdout = buf
	sess.Stderr = buf

	done := make(chan error, 1)
	if err := sess.Start(command); err != nil {
		return "", -1, err
	}
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return buf.String(), -1, ctx.Err()
	case werr := <-done:
		code := 0
		if werr != nil {
			if ee, ok := werr.(*ssh.ExitError); ok {
				code = ee.ExitStatus()
			} else {
				return buf.String(), -1, werr
			}
		}
		return buf.String(), code, nil
	}
}

// RunScript executes a shell script under /bin/sh regardless of the login
// user's shell, deterministically. The script is base64-encoded and run as
// `echo <b64> | base64 -d | /bin/sh` — a single in-shell pipeline, so:
//   - the login shell only ever sees plain words + pipes (works under
//     sh/bash/csh/tcsh/fish alike — no sh-syntax for it to choke on),
//   - the script reaches /bin/sh through the pipe with no stdin-write timing
//     race (the flaky path when streaming to a PTY-less session), and
//   - /bin/sh is an absolute interpreter present on every Linux.
//
// This is the same transport-agnostic trick Ansible uses to run modules.
func (c *SSHConn) RunScript(ctx context.Context, script string) (string, int, error) {
	return c.Run(ctx, b64Pipe(script))
}

// Stream runs a command and delivers stdout+stderr to onChunk as it arrives,
// with stateful ANSI stripping. Returns the exit code.
func (c *SSHConn) Stream(ctx context.Context, command string, onChunk func(string)) (int, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return -1, err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := sess.Start(command); err != nil {
		return -1, err
	}

	stripper := &ansiStripper{}
	pump := func(r io.Reader) {
		b := make([]byte, 4096)
		for {
			n, rerr := r.Read(b)
			if n > 0 {
				onChunk(stripper.strip(string(b[:n])))
			}
			if rerr != nil {
				return
			}
		}
	}
	go pump(stdout)
	go pump(stderr)

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return -1, ctx.Err()
	case werr := <-done:
		if werr != nil {
			if ee, ok := werr.(*ssh.ExitError); ok {
				return ee.ExitStatus(), nil
			}
			return -1, werr
		}
		return 0, nil
	}
}

// RunEscalated runs a command elevated to root per the connection's mode.
// Used during bootstrap where the login user is not root.
func (c *SSHConn) RunEscalated(ctx context.Context, command string) (string, int, error) {
	switch c.escalate {
	case EscDirectRoot:
		// Run under /bin/sh over stdin — the login user's shell may not be
		// POSIX, and the command is already an sh script.
		return c.RunScript(ctx, command)
	case EscSudoNoPasswd:
		return c.RunScript(ctx, "sudo -n /bin/sh -c "+shellSingleQuote(command))
	case EscSudoPassword:
		// The password goes to sudo on stdin, so we can't feed the script on
		// stdin too — instead sudo runs /bin/sh with a base64 pipe, keeping
		// the whole thing shell-agnostic (the login shell only sees plain
		// words + a single-quoted arg). sudo reads one stdin line for the
		// password; the inner /bin/sh reads the script from the pipe.
		// Use a PTY: some hosts require a terminal for sudo (requiretty), and
		// a PTY also satisfies sudo's password prompt cleanly. -S is dropped
		// so sudo prompts on the tty; the password is written after a short
		// delay (same as su).
		return c.runWithPasswordStdin(ctx, "sudo -p '' /bin/sh -c "+shellSingleQuote(b64Pipe(command)), true)
	case EscSuRoot:
		return c.runWithPasswordStdin(ctx, "su -c "+shellSingleQuote(b64Pipe(command)), true)
	}
	return "", -1, fmt.Errorf("no escalation strategy selected")
}

// b64Pipe wraps a shell script so it runs under /bin/sh regardless of the
// surrounding shell: `echo <base64> | base64 -d | /bin/sh`.
func b64Pipe(script string) string {
	return "echo " + base64.StdEncoding.EncodeToString([]byte(script)) + " | base64 -d | /bin/sh"
}

// runWithPasswordStdin feeds the stored password to sudo -S (pipe) or su
// (PTY). su needs a real terminal and a short delay before the password.
func (c *SSHConn) runWithPasswordStdin(ctx context.Context, command string, pty bool) (string, int, error) {
	sess, err := c.client.NewSession()
	if err != nil {
		return "", -1, err
	}
	defer sess.Close()

	out := &syncBuffer{}
	sess.Stdout = out
	sess.Stderr = out
	stdin, err := sess.StdinPipe()
	if err != nil {
		return "", -1, err
	}
	if pty {
		modes := ssh.TerminalModes{ssh.ECHO: 0}
		if err := sess.RequestPty("xterm", 24, 80, modes); err != nil {
			return "", -1, err
		}
	}
	if err := sess.Start(command); err != nil {
		return "", -1, err
	}
	go func() {
		if pty {
			time.Sleep(ptyStdinDelay)
		}
		_, _ = io.WriteString(stdin, c.password+"\n")
	}()

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return out.String(), -1, ctx.Err()
	case werr := <-done:
		text := stripLeadingPasswordPrompt(out.String())
		code := 0
		if werr != nil {
			if ee, ok := werr.(*ssh.ExitError); ok {
				code = ee.ExitStatus()
			} else {
				return text, -1, werr
			}
		}
		return text, code, nil
	}
}

// stripLeadingPasswordPrompt removes the echoed "Password:" line su prints.
func stripLeadingPasswordPrompt(s string) string {
	for _, p := range []string{"Password: ", "Password:", "password:"} {
		s = strings.TrimPrefix(s, p)
	}
	return strings.TrimPrefix(s, "\n")
}

// shellSingleQuote safely wraps a string in single quotes for /bin/sh.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---- ANSI stripping (stateful across chunk boundaries) ----------------------

type ansiStripper struct {
	inEscape bool
	pending  []byte
}

func (a *ansiStripper) strip(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if a.inEscape {
			// CSI sequences end on a byte in @..~ (0x40-0x7E).
			if ch >= 0x40 && ch <= 0x7E {
				a.inEscape = false
			}
			continue
		}
		if ch == 0x1b { // ESC
			a.inEscape = true
			continue
		}
		if ch == '\r' {
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}
