package nettools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// HTTPRequest is a user-composed request for the HTTP tester.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

// HTTPResponse is the bounded result returned to the browser.
type HTTPResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Proto      string            `json:"proto"`
	DurationMs float64           `json:"durationMs"`
	Size       int64             `json:"size"`
	Headers    map[string]string `json:"headers"`
	HeaderKeys []string          `json:"headerKeys"`
	Body       string            `json:"body"`
	Truncated  bool              `json:"truncated"`
	Binary     bool              `json:"binary"`
	FinalURL   string            `json:"finalUrl"`
}

var allowedMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

const maxHTTPBody = 5 << 20 // 5 MiB response cap

// DoHTTP performs a user-composed HTTP request from the server. Bounds: only
// http/https schemes, an allow-listed method, ≤10s, ≤5 redirects, ≤5 MiB
// response read. On a homelab this is meant to reach internal APIs, so no
// host filtering — but everything is capped and it only runs for an
// authenticated user.
func DoHTTP(ctx context.Context, in HTTPRequest) (*HTTPResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = "GET"
	}
	if !allowedMethods[method] {
		return nil, fmt.Errorf("unsupported method %q", method)
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("enter a valid http:// or https:// URL")
	}

	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	for k, v := range in.Headers {
		if k = strings.TrimSpace(k); k != "" {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "HomeHub-HTTP-Tester/1.0")
	}

	client := &http.Client{
		Timeout: 12 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects (>5)")
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, cleanHTTPErr(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	truncated := int64(len(raw)) > maxHTTPBody
	if truncated {
		raw = raw[:maxHTTPBody]
	}

	out := &HTTPResponse{
		Status:     resp.StatusCode,
		StatusText: strings.TrimPrefix(resp.Status, fmt.Sprintf("%d ", resp.StatusCode)),
		Proto:      resp.Proto,
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		Size:       int64(len(raw)),
		Headers:    map[string]string{},
		HeaderKeys: []string{},
		Truncated:  truncated,
		FinalURL:   resp.Request.URL.String(),
	}
	for k, v := range resp.Header {
		out.Headers[k] = strings.Join(v, ", ")
		out.HeaderKeys = append(out.HeaderKeys, k)
	}
	sort.Strings(out.HeaderKeys)

	if utf8.Valid(raw) {
		out.Body = string(raw)
	} else {
		out.Binary = true
		out.Body = fmt.Sprintf("[binary response, %d bytes — not shown]", out.Size)
	}
	return out, nil
}

func cleanHTTPErr(err error) error {
	msg := err.Error()
	if u, ok := err.(*url.Error); ok {
		msg = u.Err.Error()
	}
	return fmt.Errorf("%s", msg)
}
