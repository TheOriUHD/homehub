package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/audit"
	"github.com/TheOriUHD/homehub/internal/store"
)

const webauthnChallengeTTL = 5 * time.Minute

// rpFor builds a WebAuthn provider for the origin serving the current
// request. The configured app URL wins when set; otherwise the requesting
// origin is used, which keeps passkeys working out of the box on whatever
// address the user reaches the app through.
func (s *Service) rpFor(ctx context.Context, requestOrigin string) (*webauthn.WebAuthn, error) {
	origin := s.settings.AppURL(ctx)
	if origin == "" {
		origin = requestOrigin
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return nil, &ValidationError{Msg: "passkeys need a valid app URL — set it in Settings"}
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "HomeHub",
		RPID:          u.Hostname(),
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
}

// webauthnUser adapts a stored user (and credentials) to the webauthn.User
// interface.
type webauthnUser struct {
	user  *store.User
	creds []store.WebauthnCredential
}

func (w *webauthnUser) WebAuthnID() []byte          { return w.user.ID[:] }
func (w *webauthnUser) WebAuthnName() string        { return w.user.Username }
func (w *webauthnUser) WebAuthnDisplayName() string { return w.user.DisplayName }
func (w *webauthnUser) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(w.creds))
	for _, c := range w.creds {
		out = append(out, storedToWebauthn(c))
	}
	return out
}

func storedToWebauthn(c store.WebauthnCredential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(c.Transports))
	for _, t := range c.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	return webauthn.Credential{
		ID:              c.CredentialID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       transports,
		Flags: webauthn.CredentialFlags{
			BackupEligible: c.BackupEligible,
			BackupState:    c.BackupState,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    c.AAGUID,
			SignCount: uint32(c.SignCount),
		},
	}
}

func (s *Service) loadWebauthnUser(ctx context.Context, userID uuid.UUID) (*webauthnUser, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	creds, err := s.store.ListWebauthnCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &webauthnUser{user: user, creds: creds}, nil
}

type webauthnSession struct {
	Data   webauthn.SessionData
	UserID uuid.UUID
}

// BeginPasskeyRegistration starts registering a new passkey for a signed-in
// user. Returns the browser options payload and the challenge token binding
// this challenge to the finish call.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, user *store.User, requestOrigin string) (any, string, error) {
	rp, err := s.rpFor(ctx, requestOrigin)
	if err != nil {
		return nil, "", err
	}
	wu, err := s.loadWebauthnUser(ctx, user.ID)
	if err != nil {
		return nil, "", err
	}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(wu.creds))
	for _, c := range wu.creds {
		exclusions = append(exclusions, protocol.CredentialDescriptor{
			Type: protocol.PublicKeyCredentialType, CredentialID: c.CredentialID,
		})
	}
	creation, sessionData, err := rp.BeginRegistration(wu,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExclusions(exclusions),
	)
	if err != nil {
		return nil, "", fmt.Errorf("begin passkey registration: %w", err)
	}
	token, _, err := NewToken()
	if err != nil {
		return nil, "", err
	}
	s.state.put("wa-reg:"+token, &webauthnSession{Data: *sessionData, UserID: user.ID}, webauthnChallengeTTL)
	return creation.Response, token, nil
}

// FinishPasskeyRegistration verifies the authenticator response and stores
// the credential.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, user *store.User, requestOrigin, challengeToken, name string, credentialJSON []byte, actor audit.Actor) (*store.WebauthnCredential, error) {
	v, ok := s.state.take("wa-reg:" + challengeToken)
	if !ok {
		return nil, &ValidationError{Msg: "registration attempt expired — try again"}
	}
	sess := v.(*webauthnSession)
	if sess.UserID != user.ID {
		return nil, &ValidationError{Msg: "registration attempt does not belong to this user"}
	}
	rp, err := s.rpFor(ctx, requestOrigin)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(credentialJSON))
	if err != nil {
		return nil, &ValidationError{Msg: "malformed authenticator response"}
	}
	wu, err := s.loadWebauthnUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	cred, err := rp.CreateCredential(wu, sess.Data, parsed)
	if err != nil {
		return nil, &ValidationError{Msg: "passkey verification failed: " + err.Error()}
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	stored, err := s.store.CreateWebauthnCredential(ctx, &store.WebauthnCredential{
		UserID:          user.ID,
		Name:            name,
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		Transports:      transports,
		AAGUID:          cred.Authenticator.AAGUID,
		SignCount:       int64(cred.Authenticator.SignCount),
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actor, "auth.passkey_registered", "", audit.OutcomeSuccess, map[string]any{"name": name})
	return stored, nil
}

// BeginPasskeyLogin starts a passwordless (discoverable) login.
func (s *Service) BeginPasskeyLogin(ctx context.Context, requestOrigin string) (any, string, error) {
	rp, err := s.rpFor(ctx, requestOrigin)
	if err != nil {
		return nil, "", err
	}
	assertion, sessionData, err := rp.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	)
	if err != nil {
		return nil, "", fmt.Errorf("begin passkey login: %w", err)
	}
	token, _, err := NewToken()
	if err != nil {
		return nil, "", err
	}
	s.state.put("wa-login:"+token, &webauthnSession{Data: *sessionData}, webauthnChallengeTTL)
	return assertion.Response, token, nil
}

// FinishPasskeyLogin validates a discoverable assertion and issues a session.
// A verified passkey is a strong credential; it does not additionally prompt
// for TOTP.
func (s *Service) FinishPasskeyLogin(ctx context.Context, requestOrigin, challengeToken string, credentialJSON []byte, ip, userAgent string) (*LoginOutcome, error) {
	if !s.ipLimiter.Allow("ip:" + ip) {
		return nil, ErrRateLimited
	}
	v, ok := s.state.take("wa-login:" + challengeToken)
	if !ok {
		return nil, &ValidationError{Msg: "sign-in attempt expired — try again"}
	}
	sess := v.(*webauthnSession)
	rp, err := s.rpFor(ctx, requestOrigin)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(credentialJSON))
	if err != nil {
		return nil, &ValidationError{Msg: "malformed authenticator response"}
	}

	var loginUser *store.User
	cred, err := rp.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		stored, err := s.store.WebauthnCredentialByCredID(ctx, rawID)
		if err != nil {
			return nil, fmt.Errorf("unknown credential")
		}
		wu, err := s.loadWebauthnUser(ctx, stored.UserID)
		if err != nil {
			return nil, err
		}
		if wu.user.Status != "active" {
			return nil, fmt.Errorf("account disabled")
		}
		loginUser = wu.user
		return wu, nil
	}, sess.Data, parsed)
	if err != nil || loginUser == nil {
		s.audit.Log(ctx, audit.Actor{Kind: "user", Name: "unknown", IP: ip},
			"auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "bad_passkey"})
		return nil, ErrInvalidCredentials
	}
	s.recordPasskeyUse(ctx, cred)
	return s.finishLogin(ctx, loginUser, ip, userAgent, "passkey")
}

// BeginMFAPasskey starts passkey verification as the second factor of a
// pending password login.
func (s *Service) BeginMFAPasskey(ctx context.Context, mfaToken, requestOrigin string) (any, string, error) {
	v, ok := s.state.peek("mfa:" + mfaToken)
	if !ok {
		return nil, "", ErrMFAExpired
	}
	pending := v.(*pendingMFA)
	rp, err := s.rpFor(ctx, requestOrigin)
	if err != nil {
		return nil, "", err
	}
	wu, err := s.loadWebauthnUser(ctx, pending.UserID)
	if err != nil {
		return nil, "", err
	}
	assertion, sessionData, err := rp.BeginLogin(wu)
	if err != nil {
		return nil, "", fmt.Errorf("begin passkey verification: %w", err)
	}
	token, _, err := NewToken()
	if err != nil {
		return nil, "", err
	}
	s.state.put("wa-mfa:"+token, &webauthnSession{Data: *sessionData, UserID: pending.UserID}, webauthnChallengeTTL)
	return assertion.Response, token, nil
}

// FinishMFAPasskey completes a pending login with a passkey assertion.
func (s *Service) FinishMFAPasskey(ctx context.Context, mfaToken, challengeToken, requestOrigin string, credentialJSON []byte, ip, userAgent string) (*LoginOutcome, error) {
	pending, err := s.takeMFAAttempt(mfaToken)
	if err != nil {
		return nil, err
	}
	v, ok := s.state.take("wa-mfa:" + challengeToken)
	if !ok {
		return nil, ErrMFAExpired
	}
	sess := v.(*webauthnSession)
	if sess.UserID != pending.UserID {
		return nil, ErrMFAExpired
	}
	rp, err := s.rpFor(ctx, requestOrigin)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(credentialJSON))
	if err != nil {
		return nil, &ValidationError{Msg: "malformed authenticator response"}
	}
	wu, err := s.loadWebauthnUser(ctx, pending.UserID)
	if err != nil {
		return nil, err
	}
	cred, err := rp.ValidateLogin(wu, sess.Data, parsed)
	if err != nil {
		s.audit.Log(ctx, audit.Actor{Kind: "user", ID: &wu.user.ID, Name: wu.user.Username, IP: ip},
			"auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "bad_passkey_mfa"})
		return nil, ErrInvalidCode
	}
	s.state.delete("mfa:" + mfaToken)
	s.recordPasskeyUse(ctx, cred)
	return s.finishLogin(ctx, wu.user, ip, userAgent, "passkey-mfa")
}

func (s *Service) recordPasskeyUse(ctx context.Context, cred *webauthn.Credential) {
	stored, err := s.store.WebauthnCredentialByCredID(ctx, cred.ID)
	if err != nil {
		return
	}
	if err := s.store.UpdateWebauthnUsage(ctx, stored.ID, int64(cred.Authenticator.SignCount), cred.Flags.BackupState); err != nil {
		s.logger.Warn("update passkey usage", "error", err)
	}
}

// MarshalWebauthnPayload converts the map-typed credential payload from the
// API layer back to raw JSON for protocol parsing.
func MarshalWebauthnPayload(payload map[string]any) ([]byte, error) {
	return json.Marshal(payload)
}
