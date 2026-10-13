// caddy-reader — a read-only window onto a local Caddy instance.
//
// Caddy's admin API binds to 127.0.0.1:2019 and is UNAUTHENTICATED. Anything
// that can reach it can replace the entire configuration, which on a reverse
// proxy means routing your domains anywhere, serving any file on the box, and
// terminating TLS for you. So the answer to "HomeHub is on another machine"
// is emphatically NOT to move the admin API onto the LAN.
//
// This runs on the Caddy host instead and exposes a narrow surface: a projection
// of the running config, and — only when asked for — the Caddyfile.
//
// # TWO MODES, AND THE DIFFERENCE IS THE WHOLE POINT
//
// Read-only is the default and needs no privileges. It answers GET /config with
// a projection and nothing else.
//
// --writable additionally allows PUT /caddyfile, so HomeHub's manager can
// change your sites. That is a real capability and it is off unless you ask for
// it, because a token that can rewrite a reverse proxy is worth much more to an
// attacker than one that can read it.
//
// WHAT IT CANNOT DO IN EITHER MODE
//   - It cannot replace Caddy's configuration through the admin API. The only
//     request it makes there is GET {admin}/config/, and that path is a constant
//     in this file, never taken from the incoming request.
//   - It cannot be used as a general proxy. It forwards no paths, methods,
//     headers or bodies.
//   - It cannot write anywhere except the one Caddyfile it was pointed at, and
//     only after Caddy itself has validated the replacement.
//
// Static, dependency-free, and one binary: nothing to install on the machine
// that fronts your traffic, and nothing that can break when that machine's
// Python or OpenSSL is upgraded.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Version is stamped at build time: -ldflags "-X main.Version=..."
var Version = "dev"

// The ONLY path this program ever asks Caddy for. Not a variable, not derived
// from a request — a constant, so "can it write to Caddy" is answerable by
// reading one line.
const caddyConfigPath = "/config/"

const (
	defaultAdmin  = "http://127.0.0.1:2019"
	defaultListen = "0.0.0.0:9776"

	// A config is read into memory to be re-encoded. Without a ceiling, an
	// admin address pointing at something that streams forever is a way to
	// exhaust this process.
	maxConfigBytes = 32 << 20
)

type server struct {
	admin     string
	token     string
	raw       bool
	client    *http.Client
	caddyfile string
	caddyBin  string
	// Writing is off unless asked for. The read-only default is what makes this
	// safe to put on a machine that fronts everything.
	writable bool

	// What was last posted to Caddy. Only read by tests, which need to check
	// that the CADDYFILE went across rather than JSON adapted here.
	lastContentType string
	lastBody        string
}

func (s *server) fetchConfig(timeout time.Duration) (any, error) {
	client := *s.client
	client.Timeout = timeout

	url := strings.TrimRight(s.admin, "/") + caddyConfigPath
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf(
			"could not reach Caddy's admin API at %s: %w. Is Caddy running, and is its admin "+
				"endpoint on that address? If Caddy runs in a container, this helper has to be "+
				"able to reach it", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Caddy answered %d at %s", resp.StatusCode, url)
	}

	var config any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxConfigBytes)).Decode(&config); err != nil {
		return nil, fmt.Errorf("%s did not return JSON. That address is probably not Caddy's admin API", url)
	}
	return config, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, `{"error":"could not encode response"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *server) authorised(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	presented, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		return false
	}
	// Constant-time: this token is the only thing between the LAN and a readout
	// of the proxy's configuration.
	return subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) == 1
}

// refuseWrite answers everything that is not a GET.
//
// Spelled out rather than left to the mux's default, so that the refusal is a
// stated property of the program instead of an accident of the library.
func refuseWrite(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
		"error": "Not something this helper does. It reads the running config, and — with " +
			"--writable — replaces the Caddyfile. Nothing else.",
	})
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Deliberately unauthenticated, and deliberately says nothing about the
	// configuration — only whether this helper and Caddy are alive.
	if _, err := s.fetchConfig(3 * time.Second); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "degraded", "caddy": err.Error(), "version": Version,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "caddy": "reachable", "version": Version,
	})
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Missing or wrong bearer token."})
		return
	}

	config, err := s.fetchConfig(10 * time.Second)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	payload := any(ProjectConfig(config))
	if s.raw {
		payload = config
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config": payload, "projected": !s.raw, "version": Version,
	})
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// Exact patterns, and GET spelled out. Go 1.22+ method patterns mean
	// anything else falls through to the catch-all below rather than being
	// quietly accepted.
	//
	// The `{$}` variants match the trailing slash EXACTLY, not as a subtree:
	// someone checking this by hand will type /config/ as often as /config, and
	// answering 404 to that reads as "the helper is broken".
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /health/{$}", s.handleHealth)
	mux.HandleFunc("GET /config", s.handleConfig)
	mux.HandleFunc("GET /config/{$}", s.handleConfig)

	// The Caddyfile itself, for the manager. GET always; PUT only with --writable.
	mux.HandleFunc("GET /caddyfile", s.handleGetCaddyfile)
	mux.HandleFunc("GET /caddyfile/{$}", s.handleGetCaddyfile)
	mux.HandleFunc("PUT /caddyfile", s.handlePutCaddyfile)
	mux.HandleFunc("PUT /caddyfile/{$}", s.handlePutCaddyfile)

	// The one route that is not behind the token, because it is how the token
	// is obtained. Guarded by a code that expires, counts wrong guesses, and
	// burns itself on use.
	mux.HandleFunc("POST /pair", s.handlePair)
	mux.HandleFunc("POST /pair/{$}", s.handlePair)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			refuseWrite(w, r)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "This helper serves /health, /config and /caddyfile, nothing else.",
		})
	})

	return mux
}

// project reads a config on stdin and writes the projection on stdout.
//
// It exists so you can see exactly what this would send before trusting it with
// your reverse proxy:
//
//	curl -s localhost:2019/config/ | caddy-reader project
//
// HomeHub's test suite uses the same subcommand to check that this
// allowlist and its parser still agree.
func project() int {
	var config any
	if err := json.NewDecoder(io.LimitReader(os.Stdin, maxConfigBytes)).Decode(&config); err != nil {
		fmt.Fprintf(os.Stderr, "stdin is not JSON: %v\n", err)
		return 2
	}
	out, err := json.Marshal(ProjectConfig(config))
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not encode: %v\n", err)
		return 2
	}
	fmt.Println(string(out))
	return 0
}

/**
 * What this program does, for someone holding it and no documentation.
 *
 * Printed for `help` and for an unrecognised subcommand. Neither should demand a
 * token first — being told to generate a secret in order to ask what a command
 * does is a small insult.
 */
func usage() {
	fmt.Printf(`%s — HomeHub's window onto a local Caddy.

  sudo caddy-reader setup       set it up, and print a code to pair with
  sudo caddy-reader pair        print a fresh pairing code
  caddy-reader status           what it is doing right now
  sudo caddy-reader start       start it
  sudo caddy-reader stop        stop it
  sudo caddy-reader restart     restart it
  caddy-reader verify [host]    check it does what it claims
  sudo caddy-reader uninstall   remove it. Your Caddy config is untouched.

Setup without the questions:

  sudo caddy-reader setup --yes                  accept everything detected
  sudo caddy-reader setup --writable             …and allow changes
  sudo caddy-reader setup --read-only            …and do not

  --caddyfile PATH    default /etc/caddy/Caddyfile, or read off your caddy unit
  --admin URL         default %s
  --listen ADDR       default %s

Also:  caddy-reader project   read a config on stdin, print what would be sent
       caddy-reader paths     the files setup writes
       caddy-reader version
`, bold("caddy-reader "+Version), defaultAdmin, defaultListen)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func run() int {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup", "install":
			return setup(os.Args[2:])
		case "pair":
			return pair(os.Args[2:])
		case "status":
			return status(os.Args[2:])
		case "start", "stop", "restart":
			return serviceCommand(os.Args[1])
		case "uninstall":
			return uninstall(os.Args[2:])
		case "verify":
			return verify(os.Args[2:])
		case "paths":
			fmt.Println(installPaths())
			return 0
		case "project":
			return project()
		case "version", "--version", "-v":
			fmt.Println(Version)
			return 0
		case "help", "-h", "--help":
			usage()
			return 0
		}
	}

	// A bare word that is not a known subcommand is a typo, not a flag value.
	// Falling through to flag parsing turns `caddy-reader verifyy` into a
	// confusing complaint about a missing token.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\n", os.Args[1])
		usage()
		return 2
	}

	listen := flag.String("listen", env("READER_LISTEN", defaultListen), "address to serve on")
	admin := flag.String("admin", env("CADDY_ADMIN", defaultAdmin), "Caddy's admin API")
	token := flag.String("token", os.Getenv("READER_TOKEN"), "shared secret; required")
	caddyfile := flag.String("caddyfile", env("CADDYFILE", "/etc/caddy/Caddyfile"),
		"path to the Caddyfile the manager reads and writes")
	// Kept for compatibility with existing unit files. Nothing uses it any more:
	// Caddy adapts the Caddyfile itself, in its own process, so there is no
	// subprocess to run and no second binary that has to match.
	caddyBin := flag.String("caddy", env("CADDY_BIN", "caddy"), "unused; Caddy does its own adapting")
	writable := flag.Bool("writable", false,
		"allow HomeHub to CHANGE the Caddyfile. Off by default: read-only is what makes this safe to run here")
	raw := flag.Bool("raw", false,
		"return the WHOLE config instead of the projection — can include basic_auth password hashes and ACME EAB keys")
	flag.Parse()

	if *token == "" {
		// Refusing rather than generating one: a token printed at startup is
		// lost the moment systemd restarts the service, and "it worked
		// yesterday" is a bad way to find that out.
		//
		// Lead with `install`, because running this by hand is the unusual case
		// and someone reaching this message probably wanted the service.
		fmt.Fprintln(os.Stderr,
			"A token is required.\n\n"+
				"  sudo caddy-reader install     set it up as a service, token and all\n\n"+
				"To run it by hand instead:\n\n"+
				"  caddy-reader --token \"$(openssl rand -hex 32)\"\n\n"+
				"caddy-reader help  lists everything.")
		return 2
	}
	if len(*token) < 16 {
		fmt.Fprintln(os.Stderr, "That token is too short to be worth having. Use at least 16 characters.")
		return 2
	}

	s := &server{
		admin:     *admin,
		token:     *token,
		raw:       *raw,
		caddyfile: *caddyfile,
		caddyBin:  *caddyBin,
		writable:  *writable,
		// No redirects followed, ever: the admin address is trusted, and
		// anything it redirects to is not.
		client: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	// Fail loudly at startup rather than on HomeHub's first request.
	caddyState := "reachable"
	if _, err := s.fetchConfig(3 * time.Second); err != nil {
		caddyState = "NOT reachable yet: " + err.Error()
	}

	// Everything write mode needs, checked now rather than when someone presses
	// save: the file, permission to replace it, and the caddy binary that
	// validates and reloads.
	writeState := "read-only"
	if *writable {
		writeState = "writable"
		if _, err := os.Stat(*caddyfile); err != nil {
			fmt.Fprintf(os.Stderr, "--writable, but %s cannot be read: %v\n", *caddyfile, err)
			return 2
		}
		// Writing means replacing the file, which needs write permission on its
		// DIRECTORY, not just on the file.
		probe, err := os.CreateTemp(filepath.Dir(*caddyfile), ".homehub-probe-*")
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"--writable, but nothing can be written next to %s: %v\n"+
					"The systemd unit ships locked down (DynamicUser, ReadOnlyPaths=/). "+
					"Write mode needs that relaxed — see helper/README.md.\n", *caddyfile, err)
			return 2
		}
		probe.Close()
		os.Remove(probe.Name())

	}

	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not listen on %s: %v\n", *listen, err)
		return 1
	}

	mode := "projected"
	if *raw {
		mode = "raw (UNFILTERED)"
	}
	startup, _ := json.Marshal(map[string]any{
		"msg": "caddy-reader listening", "listen": listener.Addr().String(),
		"admin": *admin, "caddy": caddyState, "mode": mode, "version": Version,
		"caddyfile": *caddyfile, "write": writeState,
	})
	fmt.Println(string(startup))

	httpServer := &http.Server{
		Handler: s.routes(),
		// A client that opens a connection and says nothing must not hold a
		// slot indefinitely.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          log.New(os.Stderr, "", log.LstdFlags),
	}

	// systemctl stop should be a clean stop, not a killed process.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		_ = httpServer.Close()
	}()

	if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "server stopped: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}
