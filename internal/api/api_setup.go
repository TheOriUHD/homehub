package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"homehub/internal/audit"
	"homehub/internal/auth"
	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/version"
)

func (s *Server) GetSetupStatus(ctx context.Context, _ GetSetupStatusRequestObject) (GetSetupStatusResponseObject, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	return GetSetupStatus200JSONResponse{NeedsSetup: count == 0, Version: version.Version}, nil
}

// CompleteSetup creates the first administrator. It is inherently
// unauthenticated and therefore only valid while the instance is empty; a
// race is prevented by re-checking inside the flow and by the unique
// username index.
func (s *Server) CompleteSetup(ctx context.Context, req CompleteSetupRequestObject) (CompleteSetupResponseObject, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return CompleteSetup409JSONResponse{ConflictJSONResponse(errBody("already_set_up", "this HomeHub is already set up"))}, nil
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

	user, err := s.store.CreateUser(ctx, username, displayName, nil, &hash, false)
	if err != nil {
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(errBody("invalid_username", "could not create this account: "+err.Error()))}, nil
	}

	owner, err := s.store.GetRoleByName(ctx, rbac.RoleOwner)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetUserRoles(ctx, user.ID, []uuid.UUID{owner.ID}); err != nil {
		return nil, err
	}

	// The app URL powers invite links and passkeys; default it to the origin
	// the admin reached the wizard on so both work with zero configuration.
	appURL := strings.TrimSpace(deref(req.Body.AppUrl))
	if appURL == "" {
		appURL = requestOrigin(httpctx.Request(ctx))
	}
	if err := s.settings.SetAppURL(ctx, appURL); err != nil {
		s.logger.Warn("could not store app url from setup", "url", appURL, "error", err)
	}

	r := httpctx.Request(ctx)
	ip := ClientIP(r)
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
