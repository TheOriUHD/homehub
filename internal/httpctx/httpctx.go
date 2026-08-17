// Package httpctx passes request-scoped HTTP objects and the authenticated
// principal through context, bridging the strict (transport-agnostic) API
// handlers with cookie and header access.
package httpctx

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"homehub/internal/store"
)

type ctxKey int

const (
	keyHTTP ctxKey = iota
	keyIdentity
)

type httpPair struct {
	w http.ResponseWriter
	r *http.Request
}

// Identity is the authenticated caller attached by the session middleware.
type Identity struct {
	User       *store.User
	SessionID  uuid.UUID
	ViewAsRole *uuid.UUID
	IP         string
	UserAgent  string
}

func WithHTTP(ctx context.Context, w http.ResponseWriter, r *http.Request) context.Context {
	return context.WithValue(ctx, keyHTTP, httpPair{w, r})
}

// Writer returns the response writer for the in-flight request (for setting
// cookies from strict handlers). Panics outside a request — a programming
// error, not a runtime condition.
func Writer(ctx context.Context) http.ResponseWriter {
	return ctx.Value(keyHTTP).(httpPair).w
}

// Request returns the in-flight request.
func Request(ctx context.Context) *http.Request {
	return ctx.Value(keyHTTP).(httpPair).r
}

func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, keyIdentity, id)
}

// IdentityFrom returns the authenticated identity, or nil for anonymous
// requests.
func IdentityFrom(ctx context.Context) *Identity {
	id, _ := ctx.Value(keyIdentity).(*Identity)
	return id
}
