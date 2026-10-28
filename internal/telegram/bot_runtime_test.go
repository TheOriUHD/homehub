package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "123456789:AAE-secret-bot-token"

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func okResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// A transport failure quotes the request URL, and the URL carries the token.
// Whatever reaches the status line or the journal must not.
func TestCallErrorsNeverCarryTheToken(t *testing.T) {
	b := &Bot{}
	b.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: lookup api.telegram.org: no such host")
	})
	err := b.call(context.Background(), testToken, "getMe", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "AAE-secret") {
		t.Fatalf("error leaks the token: %v", err)
	}
	if !strings.Contains(err.Error(), "getMe") || !strings.Contains(err.Error(), "no such host") {
		t.Errorf("error lost what actually went wrong: %v", err)
	}

	// The backstop: any error text quoting the token comes out redacted.
	leaky := errors.New(`Post "https://api.telegram.org/bot` + testToken + `/getMe": EOF`)
	if got := redact(leaky, testToken).Error(); strings.Contains(got, testToken) || !strings.Contains(got, "<redacted>") {
		t.Errorf("redact = %q", got)
	}
	if redact(nil, testToken) != nil {
		t.Error("redact(nil) must stay nil")
	}
}

// A bridge whose first getMe fails — the network not up yet at boot — keeps
// trying and comes up once Telegram answers, showing the (redacted) failure
// in the meantime.
func TestBridgeRetriesUntilTelegramAnswers(t *testing.T) {
	oldBase, oldMax := retryBase, retryMax
	retryBase, retryMax = time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() { retryBase, retryMax = oldBase, oldMax })

	var getMe atomic.Int32
	var mu sync.Mutex
	var sawErr string
	b := &Bot{Logger: quietLogger()}
	b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			if getMe.Add(1) <= 3 {
				// Capture the status the failing attempts left behind.
				_, lastErr := b.Status()
				mu.Lock()
				if lastErr != "" {
					sawErr = lastErr
				}
				mu.Unlock()
				return nil, errors.New("connect: network is unreachable")
			}
			return okResponse(`{"ok":true,"result":{"username":"hh_bot"}}`), nil
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return okResponse(`{"ok":true,"result":true}`), nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.run(ctx, testToken); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if user, lastErr := b.Status(); user == "hh_bot" && lastErr == "" {
			break
		}
		if time.Now().After(deadline) {
			user, lastErr := b.Status()
			t.Fatalf("bridge never came up: user=%q lastErr=%q after %d getMe", user, lastErr, getMe.Load())
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	if !strings.Contains(sawErr, "getMe failed") || strings.Contains(sawErr, testToken) {
		t.Errorf("status during retries = %q; want the redacted getMe failure", sawErr)
	}
	mu.Unlock()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the poller did not stop when its context ended")
	}
}

// Switching the bridge off while it is still retrying stops it for good.
func TestDisablingStopsARetryingBridge(t *testing.T) {
	oldBase := retryBase
	retryBase = time.Hour // it would otherwise sit in the backoff
	t.Cleanup(func() { retryBase = oldBase })

	var calls atomic.Int32
	enabled := atomic.Bool{}
	enabled.Store(true)
	b := &Bot{
		Logger:    quietLogger(),
		TokenFn:   func(context.Context) string { return testToken },
		EnabledFn: func(context.Context) bool { return enabled.Load() },
	}
	b.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("connect: network is unreachable")
	})
	b.Reload(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	enabled.Store(false)
	b.Reload(context.Background())

	b.mu.Lock()
	running := b.cancel != nil
	b.mu.Unlock()
	if running {
		t.Fatal("a disabled bridge still has a poller")
	}
	if user, lastErr := b.Status(); user != "" || lastErr != "" {
		t.Errorf("disabled bridge status = (%q, %q), want empty", user, lastErr)
	}
}
