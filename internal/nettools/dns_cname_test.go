package nettools

import (
	"context"
	"testing"
	"time"
)

// TestLookupCNAME_NoFabrication guards the fix for the review finding that
// net.Resolver.LookupCNAME returns a host's own canonical FQDN (with no error)
// for A/AAAA-only names, which used to be reported as a phantom CNAME record.
// A name with address records but no CNAME must yield zero CNAME records.
func TestLookupCNAME_NoFabrication(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// example.com has A/AAAA records but no CNAME.
	res := Lookup(ctx, "example.com", "CNAME", "1.1.1.1:53")
	if res.Error != "" {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	if len(res.Records) != 0 {
		t.Fatalf("expected no CNAME records for a no-CNAME domain, got %+v", res.Records)
	}
}

// TestDoHTTP_HeaderKeysNonNil guards the fix for the review finding that a
// response with no header lines left HeaderKeys as a nil slice (marshaled to
// JSON null), crashing the frontend's .map(). It must always be non-nil.
func TestDoHTTP_HeaderKeysNonNil(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	resp, err := DoHTTP(ctx, HTTPRequest{Method: "GET", URL: "https://api.github.com/zen"})
	if err != nil {
		t.Fatalf("DoHTTP: %v", err)
	}
	if resp.HeaderKeys == nil {
		t.Fatal("HeaderKeys is nil — would marshal to JSON null and crash the frontend")
	}
}
