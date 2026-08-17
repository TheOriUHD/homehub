package main

// `caddy-reader verify` — check that a running helper does what it claims.
//
// The same checks as verify.sh, built in, so the single binary the installer put
// on the box is enough to audit itself. The shell version stays for checking a
// helper on ANOTHER machine from wherever you happen to be.
//
// The point is not that it works. The point is that the things it must refuse
// are actually refused.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type checker struct {
	target string
	token  string
	pass   int
	fail   int
}

func (c *checker) status(method, path string) int {
	req, err := http.NewRequest(method, "http://"+c.target+path, strings.NewReader("{}"))
	if err != nil {
		return 0
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func (c *checker) body(path string) string {
	req, _ := http.NewRequest("GET", "http://"+c.target+path, nil)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(out)
}

func (c *checker) check(what string, want, got int, why string) {
	if want == got {
		fmt.Printf("  PASS  %-44s %d\n", what, got)
		c.pass++
		return
	}
	fmt.Printf("  FAIL  %-44s got %d, expected %d\n", what, got, want)
	if why != "" {
		fmt.Printf("        %s\n", why)
	}
	c.fail++
}

func verify(args []string) int {
	target := "localhost:9776"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
	}

	token := os.Getenv("READER_TOKEN")
	if token == "" {
		// Where the installer keeps it.
		if data, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "READER_TOKEN="); ok {
					token = strings.TrimSpace(v)
				}
			}
		}
	}
	if token == "" {
		fmt.Fprintf(os.Stderr, "No token. Set READER_TOKEN, or run where %s is readable.\n", envPath)
		return 2
	}

	c := &checker{target: target, token: token}
	fmt.Printf("\ncaddy-reader  %s\n\n", target)

	health := c.body("/health")
	if health == "" {
		fmt.Printf("  FAIL  nothing is answering on %s\n\n", target)
		fmt.Println("  Is it running?   systemctl status caddy-reader")
		return 1
	}
	if strings.Contains(health, `"caddy":"reachable"`) {
		fmt.Printf("  PASS  %-44s ok\n", "helper is up, and can see Caddy")
		c.pass++
	} else {
		fmt.Printf("  FAIL  %-44s %s\n", "helper is up, and can see Caddy", health)
		c.fail++
	}

	c.check("a request with no token is refused", 401,
		(&checker{target: target}).status("GET", "/config"),
		"Anyone on the network could read your proxy layout.")

	c.check("a request with the wrong token is refused", 401,
		(&checker{target: target, token: "definitely-not-the-token"}).status("GET", "/config"), "")

	// Caddy's admin API has many endpoints. None may be reachable through here,
	// because the path is never taken from the request.
	for _, path := range []string{"/load", "/id/foo", "/stop"} {
		c.check("admin path "+path+" is not forwarded", 404, c.status("GET", path), "")
	}

	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		c.check("a "+method+" to /config is refused", 405, c.status(method, "/config"),
			"This helper must never be able to replace Caddy's configuration.")
	}

	c.check("a request with the token is served", 200, c.status("GET", "/config"), "")

	config := c.body("/config")
	if strings.Contains(config, `"projected":true`) {
		fmt.Printf("  PASS  %-44s filtered\n", "it is filtering, not forwarding wholesale")
		c.pass++
	} else {
		fmt.Printf("  FAIL  %-44s RAW\n", "it is filtering, not forwarding wholesale")
		fmt.Println("        This helper runs with --raw and sends the whole config, secrets included.")
		c.fail++
	}

	// Write access is not a failure — the manager needs it — but it is a much
	// bigger capability than reading, and should never be a surprise.
	caddyfile := c.body("/caddyfile")
	switch {
	case strings.Contains(caddyfile, `"writable":true`):
		fmt.Printf("\n  !!    %-44s ENABLED\n", "write access")
		fmt.Println("        devtracker can rewrite your Caddyfile. Required by the manager;")
		fmt.Println("        to remove it, run: sudo caddy-reader setup --read-only")
	case strings.Contains(caddyfile, `"writable"`):
		fmt.Printf("\n  --    %-44s read-only\n", "write access")
	default:
		fmt.Printf("\n  --    %-44s not served by this version\n", "the Caddyfile endpoint")
	}

	fmt.Println()
	if c.fail == 0 {
		fmt.Printf("  All %d checks passed. It is doing what it says.\n\n", c.pass)
		return 0
	}
	fmt.Printf("  %d of %d checks failed. Do not connect it until they pass.\n\n", c.fail, c.pass+c.fail)
	return 1
}
