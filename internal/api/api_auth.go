package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"homehub/internal/auth"
	"homehub/internal/httpctx"
	"homehub/internal/store"
)

// ---- login flows -----------------------------------------------------------

func (s *Server) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	r := httpctx.Request(ctx)
	out, err := s.auth.PasswordLogin(ctx, req.Body.Username, req.Body.Password, ClientIP(r), r.UserAgent())
	if err != nil {
		var locked *auth.LockedError
		switch {
		case errors.Is(err, auth.ErrRateLimited):
			return Login429JSONResponse{TooManyRequestsJSONResponse(errBody("rate_limited", err.Error()))}, nil
		case errors.As(err, &locked):
			return Login423JSONResponse(errBody("locked", locked.Error())), nil
		case errors.Is(err, auth.ErrInvalidCredentials):
			return Login401JSONResponse{UnauthorizedJSONResponse(errBody("invalid_credentials", err.Error()))}, nil
		}
		return nil, err
	}
	return s.loginResultResponse(ctx, out)
}

func (s *Server) loginResultResponse(ctx context.Context, out *auth.LoginOutcome) (LoginResponseObject, error) {
	if out.Status == "mfa_required" {
		methods := make([]LoginResultMethods, 0, len(out.Methods))
		for _, m := range out.Methods {
			methods = append(methods, LoginResultMethods(m))
		}
		return Login200JSONResponse(LoginResult{
			Status:   "mfa_required",
			MfaToken: &out.MFAToken,
			Methods:  &methods,
		}), nil
	}
	setSessionCookie(ctx, out.SessionToken, out.Session.ExpiresAt)
	user, err := s.apiUserFor(ctx, out.User)
	if err != nil {
		return nil, err
	}
	return Login200JSONResponse(LoginResult{Status: "ok", User: &user}), nil
}

// sessionUserFromOutcome finalizes an MFA/passkey/invite login: sets the
// cookie and returns the shared SessionUser payload.
func (s *Server) sessionUserFromOutcome(ctx context.Context, out *auth.LoginOutcome) (SessionUserJSONResponse, error) {
	setSessionCookie(ctx, out.SessionToken, out.Session.ExpiresAt)
	user, err := s.apiUserFor(ctx, out.User)
	if err != nil {
		return SessionUserJSONResponse{}, err
	}
	return SessionUserJSONResponse{User: user}, nil
}

// mapMFAError translates auth flow errors into the shared 401/429 bodies.
func mfaError(err error) (Error, bool, bool) {
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		return errBody("rate_limited", err.Error()), false, true
	case errors.Is(err, auth.ErrMFAExpired):
		return errBody("mfa_expired", err.Error()), true, false
	case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrInvalidCredentials):
		return errBody("invalid_code", "that code was not accepted"), true, false
	}
	return Error{}, false, false
}

func (s *Server) LoginTotp(ctx context.Context, req LoginTotpRequestObject) (LoginTotpResponseObject, error) {
	r := httpctx.Request(ctx)
	out, err := s.auth.CompleteTOTP(ctx, req.Body.MfaToken, req.Body.Code, ClientIP(r), r.UserAgent())
	if err != nil {
		if body, is401, is429 := mfaError(err); is401 {
			return LoginTotp401JSONResponse{UnauthorizedJSONResponse(body)}, nil
		} else if is429 {
			return LoginTotp429JSONResponse{TooManyRequestsJSONResponse(body)}, nil
		}
		return nil, err
	}
	su, err := s.sessionUserFromOutcome(ctx, out)
	if err != nil {
		return nil, err
	}
	return LoginTotp200JSONResponse{su}, nil
}

func (s *Server) LoginRecovery(ctx context.Context, req LoginRecoveryRequestObject) (LoginRecoveryResponseObject, error) {
	r := httpctx.Request(ctx)
	out, err := s.auth.CompleteRecovery(ctx, req.Body.MfaToken, req.Body.Code, ClientIP(r), r.UserAgent())
	if err != nil {
		if body, is401, is429 := mfaError(err); is401 {
			return LoginRecovery401JSONResponse{UnauthorizedJSONResponse(body)}, nil
		} else if is429 {
			return LoginRecovery429JSONResponse{TooManyRequestsJSONResponse(body)}, nil
		}
		return nil, err
	}
	su, err := s.sessionUserFromOutcome(ctx, out)
	if err != nil {
		return nil, err
	}
	return LoginRecovery200JSONResponse{su}, nil
}

func (s *Server) PasskeyLoginOptions(ctx context.Context, _ PasskeyLoginOptionsRequestObject) (PasskeyLoginOptionsResponseObject, error) {
	r := httpctx.Request(ctx)
	options, token, err := s.auth.BeginPasskeyLogin(ctx, requestOrigin(r))
	if err != nil {
		return nil, err
	}
	payload, err := toPayload(options)
	if err != nil {
		return nil, err
	}
	return PasskeyLoginOptions200JSONResponse(WebauthnChallenge{ChallengeToken: token, PublicKey: payload}), nil
}

func (s *Server) PasskeyLoginVerify(ctx context.Context, req PasskeyLoginVerifyRequestObject) (PasskeyLoginVerifyResponseObject, error) {
	r := httpctx.Request(ctx)
	raw, err := auth.MarshalWebauthnPayload(req.Body.Credential)
	if err != nil {
		return PasskeyLoginVerify401JSONResponse{unauthorized()}, nil
	}
	out, err := s.auth.FinishPasskeyLogin(ctx, requestOrigin(r), req.Body.ChallengeToken, raw, ClientIP(r), r.UserAgent())
	if err != nil {
		var validation *auth.ValidationError
		if errors.As(err, &validation) {
			// Expired challenge, missing user verification: actionable, and
			// says nothing about which accounts exist.
			return PasskeyLoginVerify401JSONResponse{UnauthorizedJSONResponse(errBody("passkey_rejected", validation.Msg))}, nil
		}
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrRateLimited) {
			return PasskeyLoginVerify401JSONResponse{UnauthorizedJSONResponse(errBody("passkey_rejected", "passkey sign-in failed — try again"))}, nil
		}
		return nil, err
	}
	su, err := s.sessionUserFromOutcome(ctx, out)
	if err != nil {
		return nil, err
	}
	return PasskeyLoginVerify200JSONResponse{su}, nil
}

func (s *Server) MfaPasskeyOptions(ctx context.Context, req MfaPasskeyOptionsRequestObject) (MfaPasskeyOptionsResponseObject, error) {
	r := httpctx.Request(ctx)
	options, token, err := s.auth.BeginMFAPasskey(ctx, req.Body.MfaToken, requestOrigin(r))
	if err != nil {
		if errors.Is(err, auth.ErrMFAExpired) {
			return MfaPasskeyOptions401JSONResponse{UnauthorizedJSONResponse(errBody("mfa_expired", err.Error()))}, nil
		}
		return nil, err
	}
	payload, err := toPayload(options)
	if err != nil {
		return nil, err
	}
	return MfaPasskeyOptions200JSONResponse(WebauthnChallenge{ChallengeToken: token, PublicKey: payload}), nil
}

func (s *Server) MfaPasskeyVerify(ctx context.Context, req MfaPasskeyVerifyRequestObject) (MfaPasskeyVerifyResponseObject, error) {
	r := httpctx.Request(ctx)
	raw, err := auth.MarshalWebauthnPayload(req.Body.Credential)
	if err != nil {
		return MfaPasskeyVerify401JSONResponse{unauthorized()}, nil
	}
	out, err := s.auth.FinishMFAPasskey(ctx, req.Body.MfaToken, req.Body.ChallengeToken, requestOrigin(r), raw, ClientIP(r), r.UserAgent())
	if err != nil {
		if body, is401, _ := mfaError(err); is401 {
			return MfaPasskeyVerify401JSONResponse{UnauthorizedJSONResponse(body)}, nil
		}
		var validation *auth.ValidationError
		if errors.As(err, &validation) {
			return MfaPasskeyVerify401JSONResponse{UnauthorizedJSONResponse(errBody("passkey_rejected", validation.Msg))}, nil
		}
		return nil, err
	}
	su, err := s.sessionUserFromOutcome(ctx, out)
	if err != nil {
		return nil, err
	}
	return MfaPasskeyVerify200JSONResponse{su}, nil
}

// ---- session lifecycle -----------------------------------------------------

func (s *Server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	if id := httpctx.IdentityFrom(ctx); id != nil {
		s.auth.Logout(ctx, id.SessionID, actorFor(id))
	}
	clearSessionCookie(ctx)
	return Logout204Response{}, nil
}

func (s *Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return GetMe401JSONResponse{unauthorized()}, nil
	}
	me, err := s.meFor(ctx, id)
	if err != nil {
		return nil, err
	}
	return GetMe200JSONResponse(me), nil
}

func (s *Server) ChangePassword(ctx context.Context, req ChangePasswordRequestObject) (ChangePasswordResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return ChangePassword401JSONResponse{unauthorized()}, nil
	}
	err := s.auth.ChangePassword(ctx, id.User, id.SessionID, req.Body.CurrentPassword, req.Body.NewPassword, actorFor(id))
	if err != nil {
		var validation *auth.ValidationError
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			return ChangePassword401JSONResponse{UnauthorizedJSONResponse(errBody("invalid_credentials", "current password is incorrect"))}, nil
		case errors.As(err, &validation):
			return ChangePassword400JSONResponse{BadRequestJSONResponse(errBody("weak_password", validation.Msg))}, nil
		}
		return nil, err
	}
	return ChangePassword204Response{}, nil
}

func (s *Server) ListMySessions(ctx context.Context, _ ListMySessionsRequestObject) (ListMySessionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	sessions, err := s.store.ListUserSessions(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	return ListMySessions200JSONResponse(toAPISessions(sessions, id.SessionID)), nil
}

func (s *Server) RevokeMySession(ctx context.Context, req RevokeMySessionRequestObject) (RevokeMySessionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	sess, err := s.store.GetSession(ctx, req.Id)
	if err != nil || sess.UserID != id.User.ID {
		return RevokeMySession404JSONResponse{notFoundErr("session")}, nil
	}
	if err := s.store.RevokeSession(ctx, sess.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return RevokeMySession404JSONResponse{notFoundErr("session")}, nil
		}
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "auth.session_revoked", "", "success", map[string]any{"sessionId": sess.ID})
	if sess.ID == id.SessionID {
		clearSessionCookie(ctx)
	}
	return RevokeMySession204Response{}, nil
}

func (s *Server) RevokeOtherSessions(ctx context.Context, _ RevokeOtherSessionsRequestObject) (RevokeOtherSessionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	n, err := s.store.RevokeUserSessions(ctx, id.User.ID, &id.SessionID)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "auth.sessions_revoked_others", "", "success", map[string]any{"count": n})
	return RevokeOtherSessions200JSONResponse{Revoked: int(n)}, nil
}

func toAPISessions(sessions []store.Session, current uuid.UUID) []SessionInfo {
	out := make([]SessionInfo, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, SessionInfo{
			Id:         sess.ID,
			CreatedAt:  sess.CreatedAt,
			LastSeenAt: sess.LastSeenAt,
			ExpiresAt:  sess.ExpiresAt,
			Ip:         sess.IP,
			UserAgent:  sess.UserAgent,
			Current:    sess.ID == current,
		})
	}
	return out
}

// ---- TOTP + recovery management -------------------------------------------

func (s *Server) EnrollTotp(ctx context.Context, _ EnrollTotpRequestObject) (EnrollTotpResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	secret, otpauthURL, err := s.auth.EnrollTOTP(ctx, id.User)
	if err != nil {
		var conflict *auth.ConflictError
		if errors.As(err, &conflict) {
			return EnrollTotp409JSONResponse{ConflictJSONResponse(errBody("totp_enabled", conflict.Msg))}, nil
		}
		return nil, err
	}
	return EnrollTotp200JSONResponse{Secret: secret, OtpauthUrl: otpauthURL}, nil
}

func (s *Server) ConfirmTotp(ctx context.Context, req ConfirmTotpRequestObject) (ConfirmTotpResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	codes, err := s.auth.ConfirmTOTP(ctx, id.User, req.Body.Code, actorFor(id))
	if err != nil {
		var validation *auth.ValidationError
		var conflict *auth.ConflictError
		switch {
		case errors.Is(err, auth.ErrInvalidCode):
			return ConfirmTotp400JSONResponse{BadRequestJSONResponse(errBody("invalid_code", "that code was not accepted — check your authenticator app"))}, nil
		case errors.As(err, &validation):
			return ConfirmTotp400JSONResponse{BadRequestJSONResponse(errBody("no_enrollment", validation.Msg))}, nil
		case errors.As(err, &conflict):
			return ConfirmTotp400JSONResponse{BadRequestJSONResponse(errBody("totp_enabled", conflict.Msg))}, nil
		}
		return nil, err
	}
	return ConfirmTotp200JSONResponse(RecoveryCodes{RecoveryCodes: codes}), nil
}

func (s *Server) DisableTotp(ctx context.Context, req DisableTotpRequestObject) (DisableTotpResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return DisableTotp401JSONResponse{unauthorized()}, nil
	}
	if err := s.auth.DisableTOTP(ctx, id.User, req.Body.Password, actorFor(id)); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrRateLimited) {
			return DisableTotp401JSONResponse{UnauthorizedJSONResponse(errBody("invalid_credentials", "password not accepted"))}, nil
		}
		return nil, err
	}
	return DisableTotp204Response{}, nil
}

func (s *Server) RegenerateRecoveryCodes(ctx context.Context, req RegenerateRecoveryCodesRequestObject) (RegenerateRecoveryCodesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return RegenerateRecoveryCodes401JSONResponse{unauthorized()}, nil
	}
	codes, err := s.auth.RegenerateRecoveryCodes(ctx, id.User, req.Body.Password, actorFor(id))
	if err != nil {
		var validation *auth.ValidationError
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrRateLimited):
			return RegenerateRecoveryCodes401JSONResponse{UnauthorizedJSONResponse(errBody("invalid_credentials", "password not accepted"))}, nil
		case errors.As(err, &validation):
			return RegenerateRecoveryCodes401JSONResponse{UnauthorizedJSONResponse(errBody("totp_disabled", validation.Msg))}, nil
		}
		return nil, err
	}
	return RegenerateRecoveryCodes200JSONResponse(RecoveryCodes{RecoveryCodes: codes}), nil
}

// ---- passkey management ----------------------------------------------------

func (s *Server) PasskeyRegisterOptions(ctx context.Context, _ PasskeyRegisterOptionsRequestObject) (PasskeyRegisterOptionsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	r := httpctx.Request(ctx)
	options, token, err := s.auth.BeginPasskeyRegistration(ctx, id.User, requestOrigin(r))
	if err != nil {
		return nil, err
	}
	payload, err := toPayload(options)
	if err != nil {
		return nil, err
	}
	return PasskeyRegisterOptions200JSONResponse(WebauthnChallenge{ChallengeToken: token, PublicKey: payload}), nil
}

func (s *Server) PasskeyRegisterVerify(ctx context.Context, req PasskeyRegisterVerifyRequestObject) (PasskeyRegisterVerifyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	r := httpctx.Request(ctx)
	raw, err := auth.MarshalWebauthnPayload(req.Body.Credential)
	if err != nil {
		return PasskeyRegisterVerify400JSONResponse{BadRequestJSONResponse(errBody("bad_credential", "malformed authenticator response"))}, nil
	}
	cred, err := s.auth.FinishPasskeyRegistration(ctx, id.User, requestOrigin(r), req.Body.ChallengeToken, req.Body.Name, raw, actorFor(id))
	if err != nil {
		var validation *auth.ValidationError
		if errors.As(err, &validation) {
			return PasskeyRegisterVerify400JSONResponse{BadRequestJSONResponse(errBody("passkey_rejected", validation.Msg))}, nil
		}
		return nil, err
	}
	return PasskeyRegisterVerify201JSONResponse(toAPIPasskey(*cred)), nil
}

func (s *Server) ListPasskeys(ctx context.Context, _ ListPasskeysRequestObject) (ListPasskeysResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	creds, err := s.store.ListWebauthnCredentials(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Passkey, 0, len(creds))
	for _, c := range creds {
		out = append(out, toAPIPasskey(c))
	}
	return ListPasskeys200JSONResponse(out), nil
}

func (s *Server) RenamePasskey(ctx context.Context, req RenamePasskeyRequestObject) (RenamePasskeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if err := s.store.RenameWebauthnCredential(ctx, id.User.ID, req.Id, req.Body.Name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return RenamePasskey404JSONResponse{notFoundErr("passkey")}, nil
		}
		return nil, err
	}
	creds, err := s.store.ListWebauthnCredentials(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	for _, c := range creds {
		if c.ID == req.Id {
			return RenamePasskey200JSONResponse(toAPIPasskey(c)), nil
		}
	}
	return RenamePasskey404JSONResponse{notFoundErr("passkey")}, nil
}

func (s *Server) DeletePasskey(ctx context.Context, req DeletePasskeyRequestObject) (DeletePasskeyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if err := s.store.DeleteWebauthnCredential(ctx, id.User.ID, req.Id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeletePasskey404JSONResponse{notFoundErr("passkey")}, nil
		}
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "auth.passkey_removed", "", "success", map[string]any{"passkeyId": req.Id})
	return DeletePasskey204Response{}, nil
}

func toAPIPasskey(c store.WebauthnCredential) Passkey {
	transports := c.Transports
	return Passkey{
		Id:         c.ID,
		Name:       c.Name,
		CreatedAt:  c.CreatedAt,
		LastUsedAt: c.LastUsedAt,
		Transports: &transports,
	}
}

// ---- view as role ----------------------------------------------------------

func (s *Server) SetViewAs(ctx context.Context, req SetViewAsRequestObject) (SetViewAsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	// Authorization deliberately ignores any active view-as context —
	// otherwise an admin previewing a restricted role could not exit.
	realPrincipal := principalWithoutViewAs(id)
	ok, err := s.rbac.Can(ctx, realPrincipal, ResRoles, "configure")
	if err != nil {
		return nil, err
	}
	if !ok {
		s.deniedAudit(ctx, id, "auth.view_as", ResRoles)
		return SetViewAs403JSONResponse{forbidden()}, nil
	}

	if req.Body.RoleId != nil {
		if _, err := s.store.GetRole(ctx, *req.Body.RoleId); err != nil {
			return SetViewAs403JSONResponse{ForbiddenJSONResponse(errBody("no_such_role", "that role does not exist"))}, nil
		}
	}
	if err := s.store.SetSessionViewAs(ctx, id.SessionID, req.Body.RoleId); err != nil {
		return nil, err
	}
	id.ViewAsRole = req.Body.RoleId
	s.rbac.Invalidate(id.User.ID)
	s.audit.Log(ctx, actorFor(id), "auth.view_as", "", "success", map[string]any{"roleId": req.Body.RoleId})

	me, err := s.meFor(ctx, id)
	if err != nil {
		return nil, err
	}
	return SetViewAs200JSONResponse(me), nil
}
