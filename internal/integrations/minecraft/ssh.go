package minecraft

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConfig is the network's host credential: one login onto the machine
// running the servers, so HomeHub (and the assistant, under the caller's
// own permissions) can reach past the companions for full restarts.
type SSHConfig struct {
	Host     string
	Port     int
	User     string
	Password string // one of Password / PrivateKey
	Key      string // PEM private key
}

// RunSSH executes one command over the network's SSH credential and
// returns combined output. Non-zero exits return the output AND an error
// so callers can show both.
func RunSSH(ctx context.Context, cfg SSHConfig, command string) (string, error) {
	if cfg.Host == "" || cfg.User == "" {
		return "", fmt.Errorf("SSH is not configured for this network")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}

	var auth []ssh.AuthMethod
	if key := strings.TrimSpace(cfg.Key); key != "" {
		signer, err := ssh.ParsePrivateKey([]byte(key))
		if err != nil {
			return "", fmt.Errorf("the SSH key could not be parsed: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
	}
	if len(auth) == 0 {
		return "", fmt.Errorf("no SSH password or key is stored for this network")
	}

	dialer := net.Dialer{Timeout: 8 * time.Second}
	tcp, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)))
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", cfg.Host, err)
	}
	clientConfig := &ssh.ClientConfig{
		User: cfg.User,
		Auth: auth,
		// Homelab reality: host keys are unpinned, same policy as the
		// terminal bridges. The credential only flows TO the host.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         8 * time.Second,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(tcp, cfg.Host, clientConfig)
	if err != nil {
		_ = tcp.Close()
		return "", fmt.Errorf("SSH login failed: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("opening an SSH session: %w", err)
	}
	defer session.Close()

	var out bytes.Buffer
	session.Stdout = &out
	session.Stderr = &out

	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case err := <-done:
		if err != nil {
			return out.String(), fmt.Errorf("command failed: %w", err)
		}
		return out.String(), nil
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return out.String(), fmt.Errorf("command timed out")
	}
}

// shellSingleQuote wraps a value for safe embedding in an sh script.
func shellSingleQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// RestartScript builds the graceful-restart shell run for one server:
// send the stop verb into its screen console, wait for the session to
// end, then relaunch from the operator's OWN start.sh transformed to
// detached mode. start.sh on disk is never modified — the transform runs
// a copy through sed so every JVM flag and jar name stays authoritative.
func RestartScript(screenName, dir, stopVerb string) string {
	// Deliberately no `set -e`: screen subcommands legitimately return
	// non-zero (a query on a gone session, stuff on a closing window), and
	// aborting on the first one would strand a server mid-restart. Each step
	// checks explicitly and the run ends with a definitive liveness verdict.
	return fmt.Sprintf(`NAME=%s
DIR=%s
if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
  echo "stopping $NAME gracefully"
  screen -S "$NAME" -X stuff "$(printf '%s\r')" >/dev/null 2>&1 || echo "note: stop keystroke returned non-zero"
  STOPPED=
  i=0
  while [ $i -lt 90 ]; do
    if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
      i=$((i+1)); sleep 1
    else
      STOPPED=1; break
    fi
  done
  if [ -z "$STOPPED" ]; then
    echo "graceful stop timed out after ${i}s; forcing the session down"
    screen -S "$NAME" -X quit >/dev/null 2>&1
    sleep 2
  else
    echo "$NAME stopped after ${i}s"
  fi
else
  echo "$NAME was not running — starting it fresh"
fi
if [ ! -d "$DIR" ]; then echo "server directory not found: $DIR"; exit 1; fi
cd "$DIR" || { echo "could not enter $DIR"; exit 1; }
if [ ! -f start.sh ]; then echo "no start.sh in $DIR"; exit 1; fi
echo "relaunching from start.sh (detached)"
sed 's/screen -S /screen -dmS /' start.sh | sh
sleep 5
if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
  echo "RESTART_OK $NAME is back up"
else
  echo "RESTART_FAIL $NAME did not come back up — check its console/logs"
  exit 1
fi
`, shellSingleQuote(screenName), shellSingleQuote(dir), stopVerb)
}

// StopScript gracefully stops one server: send the stop verb into its
// screen console, wait for the session to end, force-quit on timeout.
func StopScript(screenName, stopVerb string) string {
	return fmt.Sprintf(`NAME=%s
if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
  echo "stopping $NAME gracefully"
  screen -S "$NAME" -X stuff "$(printf '%s\r')" >/dev/null 2>&1 || echo "note: stop keystroke returned non-zero"
  STOPPED=
  i=0
  while [ $i -lt 90 ]; do
    if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
      i=$((i+1)); sleep 1
    else
      STOPPED=1; break
    fi
  done
  if [ -z "$STOPPED" ]; then
    echo "graceful stop timed out after ${i}s; forcing the session down"
    screen -S "$NAME" -X quit >/dev/null 2>&1
    sleep 2
  fi
  if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
    echo "STOP_FAIL $NAME is still running"; exit 1
  else
    echo "STOP_OK $NAME stopped"
  fi
else
  echo "STOP_OK $NAME was already stopped"
fi
`, shellSingleQuote(screenName), stopVerb)
}

// StartScript launches one server from its start.sh (detached) if it is
// not already running.
func StartScript(screenName, dir string) string {
	return fmt.Sprintf(`NAME=%s
DIR=%s
if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
  echo "START_OK $NAME is already running"; exit 0
fi
if [ ! -d "$DIR" ]; then echo "server directory not found: $DIR"; exit 1; fi
cd "$DIR" || { echo "could not enter $DIR"; exit 1; }
if [ ! -f start.sh ]; then echo "no start.sh in $DIR"; exit 1; fi
echo "launching $NAME from start.sh (detached)"
sed 's/screen -S /screen -dmS /' start.sh | sh
sleep 5
if screen -S "$NAME" -Q select . >/dev/null 2>&1; then
  echo "START_OK $NAME is up"
else
  echo "START_FAIL $NAME did not come up — check its console/logs"; exit 1
fi
`, shellSingleQuote(screenName), shellSingleQuote(dir))
}
