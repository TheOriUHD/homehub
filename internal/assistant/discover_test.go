package assistant

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestDiscoverFindsLMStudioSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5-7b-instruct"},{"id":"llama-3.2-3b"}]}`))
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portStr)

	found := discoverLMStudio(context.Background(), []string{host, "192.0.2.1"}, port)
	if len(found) != 1 {
		t.Fatalf("expected 1 instance, got %+v", found)
	}
	if found[0].BaseURL != "http://"+host+":"+portStr+"/v1" || len(found[0].Models) != 2 {
		t.Fatalf("wrong instance: %+v", found[0])
	}
}

func TestDiscoverIgnoresNonLMStudio(t *testing.T) {
	// A listener that is not an OpenAI server (404 on /v1/models).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portStr)
	if found := discoverLMStudio(context.Background(), []string{host}, port); len(found) != 0 {
		t.Fatalf("non-LM-Studio listener reported: %+v", found)
	}
}

func TestOpenAIEndpointNormalization(t *testing.T) {
	cases := map[string]string{
		"http://h:1234/v1":                  "http://h:1234/v1/chat/completions",
		"http://h:1234/v1/":                 "http://h:1234/v1/chat/completions",
		"http://h:1234/v1/chat/completions": "http://h:1234/v1/chat/completions",
		"https://api.groq.com/openai/v1":    "https://api.groq.com/openai/v1/chat/completions",
		" http://h:1234/v1 ":                "http://h:1234/v1/chat/completions",
	}
	for in, want := range cases {
		if got := openAIEndpoint(in); got != want {
			t.Errorf("openAIEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerifyDiagnosesMissingV1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Correct URL: models come back.
	models, err := VerifyOpenAIEndpoint(context.Background(), srv.URL+"/v1", "")
	if err != nil || len(models) != 1 || models[0] != "m1" {
		t.Fatalf("good URL failed: %v %v", models, err)
	}
	// Full pasted path: accepted.
	if _, err := VerifyOpenAIEndpoint(context.Background(), srv.URL+"/v1/chat/completions", ""); err != nil {
		t.Fatalf("pasted full path rejected: %v", err)
	}
	// Missing /v1: diagnosed, not just failed.
	_, err = VerifyOpenAIEndpoint(context.Background(), srv.URL, "")
	if err == nil || !strings.Contains(err.Error(), "/v1") {
		t.Fatalf("missing /v1 not diagnosed: %v", err)
	}
	// Bad scheme refused.
	if _, err := VerifyOpenAIEndpoint(context.Background(), "ftp://x", ""); err == nil {
		t.Fatal("bad scheme accepted")
	}
}

// A locked server is still a find, and a key unlocks the verifier.
func TestAuthAwareDiscoveryAndVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sekrit" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.Atoi(portStr)

	found := discoverLMStudio(context.Background(), []string{host}, port)
	if len(found) != 1 || !found[0].RequiresKey {
		t.Fatalf("locked server not reported as requiring a key: %+v", found)
	}
	if _, err := VerifyOpenAIEndpoint(context.Background(), srv.URL+"/v1", ""); err == nil || !strings.Contains(err.Error(), "requires an API key") {
		t.Fatalf("keyless verify not diagnosed: %v", err)
	}
	if _, err := VerifyOpenAIEndpoint(context.Background(), srv.URL+"/v1", "wrong"); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("wrong key not diagnosed: %v", err)
	}
	models, err := VerifyOpenAIEndpoint(context.Background(), srv.URL+"/v1", "sekrit")
	if err != nil || len(models) != 1 {
		t.Fatalf("correct key failed: %v %v", models, err)
	}
}
