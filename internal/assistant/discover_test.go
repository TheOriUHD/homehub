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
