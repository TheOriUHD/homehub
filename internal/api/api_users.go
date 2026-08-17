package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/auth"
	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

const inviteTTL = 72 * time.Hour

func (s *Server) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelView) {
		return ListUsers403JSONResponse{forbidden()}, nil
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	info, err := s.store.LoadUserSecurityInfo(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(users))
	for i := range users {
		u := &users[i]
		out = append(out, toAPIUser(u, info.RoleIDs[u.ID], info.TOTPEnabled[u.ID], info.PasskeyCount[u.ID]))
	}
	return ListUsers200JSONResponse(out), nil
}

func (s *Server) CreateUser(ctx context.Context, req CreateUserRequestObject) (CreateUserResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "user.create", ResUsers)
		return CreateUser403JSONResponse{forbidden()}, nil
	}

	username := strings.TrimSpace(req.Body.Username)
	displayName := strings.TrimSpace(req.Body.DisplayName)
	var email *string
	if req.Body.Email != nil {
		e := strings.TrimSpace(string(*req.Body.Email))
		if e != "" {
			email = &e
		}
	}

	roleIDs := []uuid.UUID{}
	if req.Body.RoleIds != nil {
		roleIDs = *req.Body.RoleIds
	}
	for _, rid := range roleIDs {
		if _, err := s.store.GetRole(ctx, rid); err != nil {
			return CreateUser400JSONResponse{BadRequestJSONResponse(errBody("no_such_role", "one of the selected roles does not exist"))}, nil
		}
	}

	result, apiErr, err := s.createUserWithOnboarding(ctx, id, username, displayName, email, roleIDs, string(req.Body.Onboarding))
	if err != nil {
		return nil, err
	}
	if apiErr != nil {
		if apiErr.Code == "username_taken" {
			return CreateUser409JSONResponse{ConflictJSONResponse(*apiErr)}, nil
		}
		return CreateUser400JSONResponse{BadRequestJSONResponse(*apiErr)}, nil
	}
	return CreateUser201JSONResponse(*result), nil
}

// createUserWithOnboarding creates the account and issues either an invite
// link or a one-time temporary password.
func (s *Server) createUserWithOnboarding(ctx context.Context, id *httpctx.Identity,
	username, displayName string, email *string, roleIDs []uuid.UUID, onboarding string) (*CreateUserResult, *Error, error) {

	var tempPassword *string
	var passwordHash *string
	mustChange := false

	if onboarding == "temp_password" {
		tp, err := auth.NewTempPassword()
		if err != nil {
			return nil, nil, err
		}
		hash, err := auth.HashPassword(tp)
		if err != nil {
			return nil, nil, err
		}
		tempPassword = &tp
		passwordHash = &hash
		mustChange = true
	}

	user, err := s.store.CreateUser(ctx, username, displayName, email, passwordHash, mustChange)
	if err != nil {
		if store.IsUniqueViolation(err) {
			e := errBody("username_taken", "that username or email is already in use")
			return nil, &e, nil
		}
		e := errBody("invalid_user", err.Error())
		return nil, &e, nil
	}
	if err := s.store.SetUserRoles(ctx, user.ID, roleIDs); err != nil {
		return nil, nil, err
	}

	result := &CreateUserResult{TempPassword: tempPassword}

	if onboarding == "invite" {
		token, hash, err := auth.NewToken()
		if err != nil {
			return nil, nil, err
		}
		expires := time.Now().Add(inviteTTL)
		if err := s.store.CreateInvite(ctx, user.ID, hash, id.User.ID, expires); err != nil {
			return nil, nil, err
		}
		inviteURL := s.baseURL(ctx) + "/invite/" + token
		result.InviteUrl = &inviteURL
		result.InviteExpiresAt = &expires
	}

	s.audit.Log(ctx, actorFor(id), "user.create", ResUsers, "success",
		map[string]any{"username": user.Username, "onboarding": onboarding, "roles": roleIDs})

	apiUser, err := s.apiUserFor(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	result.User = apiUser
	return result, nil, nil
}

func (s *Server) GetUser(ctx context.Context, req GetUserRequestObject) (GetUserResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelView) {
		return GetUser403JSONResponse{forbidden()}, nil
	}
	user, err := s.store.GetUser(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return GetUser404JSONResponse{notFoundErr("user")}, nil
		}
		return nil, err
	}
	detail, err := s.userDetail(ctx, user)
	if err != nil {
		return nil, err
	}
	return GetUser200JSONResponse(*detail), nil
}

func (s *Server) userDetail(ctx context.Context, user *store.User) (*UserDetail, error) {
	apiUser, err := s.apiUserFor(ctx, user)
	if err != nil {
		return nil, err
	}
	overrides, err := s.store.UserOverrideGrants(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &UserDetail{
		Id:                 apiUser.Id,
		Username:           apiUser.Username,
		DisplayName:        apiUser.DisplayName,
		Email:              apiUser.Email,
		Status:             UserDetailStatus(apiUser.Status),
		MustChangePassword: apiUser.MustChangePassword,
		TotpEnabled:        apiUser.TotpEnabled,
		PasskeyCount:       apiUser.PasskeyCount,
		RoleIds:            apiUser.RoleIds,
		LastLoginAt:        apiUser.LastLoginAt,
		CreatedAt:          apiUser.CreatedAt,
		Overrides:          toAPIGrants(overrides),
	}, nil
}

func (s *Server) UpdateUser(ctx context.Context, req UpdateUserRequestObject) (UpdateUserResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "user.update", ResUsers)
		return UpdateUser403JSONResponse{forbidden()}, nil
	}
	user, err := s.store.GetUser(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateUser404JSONResponse{notFoundErr("user")}, nil
		}
		return nil, err
	}

	changes := map[string]any{}

	if req.Body.DisplayName != nil || req.Body.Email != nil {
		displayName := user.DisplayName
		if req.Body.DisplayName != nil {
			displayName = strings.TrimSpace(*req.Body.DisplayName)
		}
		email := user.Email
		if req.Body.Email != nil {
			e := strings.TrimSpace(*req.Body.Email)
			if e == "" {
				email = nil
			} else {
				email = &e
			}
		}
		if err := s.store.SetUserProfile(ctx, user.ID, displayName, email); err != nil {
			if store.IsUniqueViolation(err) {
				return UpdateUser400JSONResponse{BadRequestJSONResponse(errBody("email_taken", "that email is already in use"))}, nil
			}
			return nil, err
		}
		changes["profile"] = true
	}

	if req.Body.Status != nil {
		newStatus := string(*req.Body.Status)
		if user.ID == id.User.ID && newStatus == "disabled" {
			return UpdateUser400JSONResponse{BadRequestJSONResponse(errBody("cannot_disable_self", "you cannot disable your own account"))}, nil
		}
		if err := s.store.SetUserStatus(ctx, user.ID, newStatus); err != nil {
			return nil, err
		}
		if newStatus == "disabled" {
			if _, err := s.store.RevokeUserSessions(ctx, user.ID, nil); err != nil {
				return nil, err
			}
		}
		changes["status"] = newStatus
	}

	if req.Body.RoleIds != nil {
		if err := s.guardOwnerLockout(ctx, user.ID, *req.Body.RoleIds); err != nil {
			return UpdateUser400JSONResponse{BadRequestJSONResponse(errBody("last_owner", err.Error()))}, nil
		}
		for _, rid := range *req.Body.RoleIds {
			if _, err := s.store.GetRole(ctx, rid); err != nil {
				return UpdateUser400JSONResponse{BadRequestJSONResponse(errBody("no_such_role", "one of the selected roles does not exist"))}, nil
			}
		}
		if err := s.store.SetUserRoles(ctx, user.ID, *req.Body.RoleIds); err != nil {
			return nil, err
		}
		changes["roles"] = *req.Body.RoleIds
	}

	if req.Body.Overrides != nil {
		specs, msg := grantSpecsFromInput(*req.Body.Overrides)
		if msg != "" {
			return UpdateUser400JSONResponse{BadRequestJSONResponse(errBody("invalid_grant", msg))}, nil
		}
		if err := s.store.ReplaceUserGrants(ctx, user.ID, specs); err != nil {
			return nil, err
		}
		changes["overrides"] = len(specs)
	}

	s.rbac.Invalidate(user.ID)
	s.audit.Log(ctx, actorFor(id), "user.update", ResUsers, "success",
		map[string]any{"username": user.Username, "changes": changes})

	fresh, err := s.store.GetUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	detail, err := s.userDetail(ctx, fresh)
	if err != nil {
		return nil, err
	}
	return UpdateUser200JSONResponse(*detail), nil
}

// guardOwnerLockout refuses a role change that would leave the system with no
// Owner.
func (s *Server) guardOwnerLockout(ctx context.Context, userID uuid.UUID, newRoles []uuid.UUID) error {
	owner, err := s.store.GetRoleByName(ctx, rbac.RoleOwner)
	if err != nil {
		return nil // no owner role means nothing to guard
	}
	keepsOwner := false
	for _, rid := range newRoles {
		if rid == owner.ID {
			keepsOwner = true
		}
	}
	if keepsOwner {
		return nil
	}
	counts, err := s.store.RoleMemberCounts(ctx)
	if err != nil {
		return err
	}
	current, err := s.store.UserRoleIDs(ctx, userID)
	if err != nil {
		return err
	}
	hadOwner := false
	for _, rid := range current {
		if rid == owner.ID {
			hadOwner = true
		}
	}
	if hadOwner && counts[owner.ID] <= 1 {
		return errors.New("this is the last Owner — assign Owner to someone else first")
	}
	return nil
}

func (s *Server) DeleteUser(ctx context.Context, req DeleteUserRequestObject) (DeleteUserResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "user.delete", ResUsers)
		return DeleteUser403JSONResponse{forbidden()}, nil
	}
	if req.Id == id.User.ID {
		return DeleteUser400JSONResponse{BadRequestJSONResponse(errBody("cannot_delete_self", "you cannot delete your own account"))}, nil
	}
	user, err := s.store.GetUser(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteUser404JSONResponse{notFoundErr("user")}, nil
		}
		return nil, err
	}
	if err := s.guardOwnerLockout(ctx, user.ID, nil); err != nil {
		return DeleteUser400JSONResponse{BadRequestJSONResponse(errBody("last_owner", err.Error()))}, nil
	}
	if err := s.store.DeleteUser(ctx, user.ID); err != nil {
		return nil, err
	}
	s.rbac.Invalidate(user.ID)
	s.audit.Log(ctx, actorFor(id), "user.delete", ResUsers, "success", map[string]any{"username": user.Username})
	return DeleteUser204Response{}, nil
}

func (s *Server) ResetUserCredentials(ctx context.Context, req ResetUserCredentialsRequestObject) (ResetUserCredentialsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "user.reset_credentials", ResUsers)
		return ResetUserCredentials403JSONResponse{forbidden()}, nil
	}
	user, err := s.store.GetUser(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ResetUserCredentials404JSONResponse{notFoundErr("user")}, nil
		}
		return nil, err
	}

	result := &CreateUserResult{}

	switch req.Body.Mode {
	case "temp_password":
		tp, err := auth.NewTempPassword()
		if err != nil {
			return nil, err
		}
		hash, err := auth.HashPassword(tp)
		if err != nil {
			return nil, err
		}
		if err := s.store.SetUserPassword(ctx, user.ID, hash, true); err != nil {
			return nil, err
		}
		result.TempPassword = &tp
	default: // invite
		token, hash, err := auth.NewToken()
		if err != nil {
			return nil, err
		}
		expires := time.Now().Add(inviteTTL)
		if err := s.store.CreateInvite(ctx, user.ID, hash, id.User.ID, expires); err != nil {
			return nil, err
		}
		inviteURL := s.baseURL(ctx) + "/invite/" + token
		result.InviteUrl = &inviteURL
		result.InviteExpiresAt = &expires
	}

	revoke := req.Body.RevokeSessions == nil || *req.Body.RevokeSessions
	if revoke {
		if _, err := s.store.RevokeUserSessions(ctx, user.ID, nil); err != nil {
			return nil, err
		}
	}

	s.audit.Log(ctx, actorFor(id), "user.reset_credentials", ResUsers, "success",
		map[string]any{"username": user.Username, "mode": req.Body.Mode, "revokedSessions": revoke})

	apiUser, err := s.apiUserFor(ctx, user)
	if err != nil {
		return nil, err
	}
	result.User = apiUser
	return ResetUserCredentials200JSONResponse(*result), nil
}

func (s *Server) ListUserSessions(ctx context.Context, req ListUserSessionsRequestObject) (ListUserSessionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelView) {
		return ListUserSessions403JSONResponse{forbidden()}, nil
	}
	if _, err := s.store.GetUser(ctx, req.Id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ListUserSessions404JSONResponse{notFoundErr("user")}, nil
		}
		return nil, err
	}
	sessions, err := s.store.ListUserSessions(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return ListUserSessions200JSONResponse(toAPISessions(sessions, id.SessionID)), nil
}

func (s *Server) RevokeUserSessions(ctx context.Context, req RevokeUserSessionsRequestObject) (RevokeUserSessionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResUsers, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "user.revoke_sessions", ResUsers)
		return RevokeUserSessions403JSONResponse{forbidden()}, nil
	}
	user, err := s.store.GetUser(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return RevokeUserSessions404JSONResponse{notFoundErr("user")}, nil
		}
		return nil, err
	}
	n, err := s.store.RevokeUserSessions(ctx, user.ID, nil)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "user.revoke_sessions", ResUsers, "success",
		map[string]any{"username": user.Username, "count": n})
	return RevokeUserSessions200JSONResponse{Revoked: int(n)}, nil
}

// ---- invite redemption (public) -------------------------------------------

func (s *Server) GetInvite(ctx context.Context, req GetInviteRequestObject) (GetInviteResponseObject, error) {
	_, user, err := s.store.InviteByTokenHash(ctx, auth.HashToken(req.Token))
	if err != nil {
		return GetInvite404JSONResponse{NotFoundJSONResponse(errBody("invalid_invite", "this invite link is invalid or has expired"))}, nil
	}
	return GetInvite200JSONResponse{Username: user.Username, DisplayName: user.DisplayName}, nil
}

func (s *Server) AcceptInvite(ctx context.Context, req AcceptInviteRequestObject) (AcceptInviteResponseObject, error) {
	invite, user, err := s.store.InviteByTokenHash(ctx, auth.HashToken(req.Token))
	if err != nil {
		return AcceptInvite404JSONResponse{NotFoundJSONResponse(errBody("invalid_invite", "this invite link is invalid or has expired"))}, nil
	}
	if err := auth.ValidatePassword(req.Body.Password); err != nil {
		return AcceptInvite400JSONResponse{BadRequestJSONResponse(errBody("weak_password", err.Error()))}, nil
	}
	hash, err := auth.HashPassword(req.Body.Password)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetUserPassword(ctx, user.ID, hash, false); err != nil {
		return nil, err
	}
	if err := s.store.MarkInviteUsed(ctx, invite.ID); err != nil {
		// Someone raced this invite; do not sign in.
		return AcceptInvite404JSONResponse{NotFoundJSONResponse(errBody("invalid_invite", "this invite link is invalid or has expired"))}, nil
	}

	r := httpctx.Request(ctx)
	ip := ClientIP(r)
	token, sess, err := s.auth.IssueSession(ctx, user.ID, ip, r.UserAgent())
	if err != nil {
		return nil, err
	}
	setSessionCookie(ctx, token, sess.ExpiresAt)
	s.audit.Log(ctx, actorFor(&httpctx.Identity{User: user, SessionID: sess.ID, IP: ip}),
		"user.invite_accepted", "", "success", nil)

	apiUser, err := s.apiUserFor(ctx, user)
	if err != nil {
		return nil, err
	}
	return AcceptInvite200JSONResponse{SessionUserJSONResponse{User: apiUser}}, nil
}
