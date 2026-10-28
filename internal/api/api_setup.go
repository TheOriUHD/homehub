package api

import (
	"context"
	"errors"
	"strings"

	"homehub/internal/audit"
	"homehub/internal/auth"
	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/store"
	"homehub/internal/version"
)

func (s *Server) GetSetupStatus(ctx context.Context, _ GetSetupStatusRequestObject) (GetSetupStatusResponseObject, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	resp := GetSetupStatus200JSONResponse{NeedsSetup: count == 0, Version: version.Version}
	if count == 0 {
		// Also covers an instance whose last user was deleted after startup:
		// the wizard is reachable again, so it needs a token again.
		if gate := s.auth.SetupGate(); gate != nil {
			if err := gate.Ensure(); err != nil {
				s.logger.Error("prepare setup token", "error", err)
			}
			path := gate.Path()
			resp.SetupTokenPath = &path
		}
	}
	return resp, nil
}

// CompleteSetup creates the first administrator. It is unauthenticated, so
// it demands the one-time setup token (proof of access to the server's log or
// data directory), and the Owner is created atomically in the store:
// concurrent submissions serialize there and exactly one wins, the rest get
// 409.
func (s *Server) CompleteSetup(ctx context.Context, req CompleteSetupRequestObject) (CompleteSetupResponseObject, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return CompleteSetup409JSONResponse{ConflictJSONResponse(errBody("already_set_up", "this HomeHub is already set up"))}, nil
	}
	r := httpctx.Request(ctx)
	ip := ClientIP(r)
	gate := s.auth.SetupGate()
	if gate == nil || !gate.Check(req.Body.SetupToken) {
		s.audit.Log(ctx, audit.Actor{Kind: "user", Name: "anonymous", IP: ip},
			"setup.completed", "", audit.OutcomeFailure, map[string]any{"reason": "bad_setup_token"})
		return CompleteSetup403JSONResponse{ForbiddenJSONResponse(errBody("invalid_setup_token",
			"the setup token is wrong — copy it from the server log or the setup-token file in the data directory"))}, nil
	}

	username := strings.TrimSpace(req.Body.Username)
	displayName := strings.TrimSpace(req.Body.DisplayName)
	if err := auth.ValidatePassword(req.Body.Password); err != nil {
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(errBody("weak_password", err.Error()))}, nil
	}
	hash, err := auth.HashPassword(req.Body.Password)
	if err != nil {
		return nil, err
	}

	user, err := s.store.CreateFirstOwner(ctx, username, displayName, hash, rbac.RoleOwner)
	if errors.Is(err, store.ErrAlreadySetUp) {
		return CompleteSetup409JSONResponse{ConflictJSONResponse(errBody("already_set_up", "this HomeHub is already set up"))}, nil
	}
	if err != nil {
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(errBody("invalid_username", "could not create this account: "+err.Error()))}, nil
	}
	gate.Consume()

	// The app URL powers invite links and passkeys; default it to the origin
	// the admin reached the wizard on so both work with zero configuration.
	appURL := strings.TrimSpace(deref(req.Body.AppUrl))
	if appURL == "" {
		appURL = requestOrigin(r)
	}
	if err := s.settings.SetAppURL(ctx, appURL); err != nil {
		s.logger.Warn("could not store app url from setup", "url", appURL, "error", err)
	}

	token, sess, err := s.auth.IssueSession(ctx, user.ID, ip, r.UserAgent())
	if err != nil {
		return nil, err
	}
	setSessionCookie(ctx, token, sess.ExpiresAt)

	s.audit.Log(ctx, audit.Actor{Kind: "user", ID: &user.ID, Name: user.Username, SessionID: &sess.ID, IP: ip},
		"setup.completed", "", audit.OutcomeSuccess, map[string]any{"username": user.Username})

	apiUser, err := s.apiUserFor(ctx, user)
	if err != nil {
		return nil, err
	}
	return CompleteSetup200JSONResponse{SessionUserJSONResponse{User: apiUser}}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
