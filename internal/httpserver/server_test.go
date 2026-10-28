package httpserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// readAll is a handler that reports whether it could drain the whole body.
var readAll = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	w.WriteHeader(http.StatusOK)
})

func TestJSONBodyLimit_ConverterUploadGetsItsOwnCap(t *testing.T) {
	h := jsonBodyLimit(readAll)
	big := bytes.Repeat([]byte("x"), maxJSONBody+1<<20) // just over the JSON cap

	post := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(big)))
		return rec.Code
	}
	// A converter upload larger than 8 MiB must reach the handler intact;
	// its own per-file/batch caps apply there.
	if got := post("/api/convert"); got != http.StatusOK {
		t.Fatalf("converter upload over 8 MiB: status %d, want 200", got)
	}
	// Every other generated endpoint keeps the JSON cap.
	if got := post("/api/auth/login"); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("ordinary JSON endpoint: status %d, want the 8 MiB cap to trip", got)
	}
	// Sub-paths are not exempt by prefix.
	if got := post("/api/convert/abc"); got != http.StatusRequestEntityTooLarge {
		t.Fatalf("converter sub-path: status %d, want the JSON cap", got)
	}
}
