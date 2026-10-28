// Package auth implements authentication: password login with lockout and
// rate limiting, session issuance, TOTP and recovery codes, passkeys, and
// password lifecycle.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"homehub/internal/audit"
	"homehub/internal/settings"
	"homehub/internal/store"
	"homehub/internal/vault"
)

const (
	lockoutThreshold = 5
	lockoutBase      = time.Minute
	lockoutMax       = 15 * time.Minute

	mfaPendingTTL  = 5 * time.Minute
	mfaMaxAttempts = 5

	totpIssuer = "HomeHub"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidCode        = errors.New("invalid or expired code")
	ErrRateLimited        = errors.New("too many attempts, slow down")
	ErrMFAExpired         = errors.New("sign-in attempt expired, start over")
)

// LockedError reports a temporarily locked account.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	return fmt.Sprintf("account locked until %s after repeated failures", e.Until.Format(time.RFC3339))
}

// ValidationError carries a user-facing input problem.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ConflictError reports a state conflict (e.g. TOTP already enabled).
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

type Service struct {
	store    *store.Store
	vault    *vault.Vault
	audit    *audit.Service
	settings *settings.Service
	logger   *slog.Logger

	state       *stateStore
	ipLimiter   *keyedLimiter
	userLimiter *keyedLimiter

	// dummyHash equalizes timing between "unknown user" and "wrong password".
	dummyHash string

	setup *SetupGate
}

// UseSetupGate installs the first-run setup token gate. Called once during
// wiring.
func (s *Service) UseSetupGate(g *SetupGate) { s.setup = g }

// SetupGate returns the first-run setup token gate.
func (s *Service) SetupGate() *SetupGate { return s.setup }

func New(st *store.Store, v *vault.Vault, aud *audit.Service, set *settings.Service, logger *slog.Logger) *Service {
	dummy, err := HashPassword(uuid.NewString())
	if err != nil {
		panic(fmt.Sprintf("argon2 unavailable: %v", err))
	}
	return &Service{
		store:       st,
		vault:       v,
		audit:       aud,
		settings:    set,
		logger:      logger.With("component", "auth"),
		state:       newStateStore(),
		ipLimiter:   newKeyedLimiter(30, 15),
		userLimiter: newKeyedLimiter(10, 5),
		dummyHash:   dummy,
	}
}

// LoginOutcome is the result of a successful first factor.
type LoginOutcome struct {
	// Status is "ok" (SessionToken and User set) or "mfa_required"
	// (MFAToken and Methods set).
	Status       string
	User         *store.User
	SessionToken string
	Session      *store.Session
	MFAToken     string
	Methods      []string
}

type pendingMFA struct {
	UserID   uuid.UUID
	Methods  []string
	Attempts int
	IP       string
}

// PasswordLogin verifies the first factor and either issues a session or
// opens a pending MFA challenge.
func (s *Service) PasswordLogin(ctx context.Context, username, password, ip, userAgent string) (*LoginOutcome, error) {
	if !s.ipLimiter.Allow("ip:"+ip) || !s.userLimiter.Allow("user:"+username) {
		return nil, ErrRateLimited
	}

	actor := audit.Actor{Kind: "user", Name: username, IP: ip}
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		if err == store.ErrNotFound {
			_, _ = VerifyPassword(password, s.dummyHash)
			s.audit.Log(ctx, actor, "auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "unknown_user"})
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	actor.ID = &user.ID
	actor.Name = user.Username

	if user.Status != "active" || user.PasswordHash == nil {
		_, _ = VerifyPassword(password, s.dummyHash)
		s.audit.Log(ctx, actor, "auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "inactive"})
		return nil, ErrInvalidCredentials
	}
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		s.audit.Log(ctx, actor, "auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "locked"})
		return nil, &LockedError{Until: *user.LockedUntil}
	}

	ok, err := VerifyPassword(password, *user.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		count, lockedUntil, ferr := s.store.RecordLoginFailure(ctx, user.ID, lockoutThreshold, lockoutBase, lockoutMax)
		if ferr != nil {
			s.logger.Error("record login failure", "error", ferr)
		}
		s.audit.Log(ctx, actor, "auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "bad_password", "failures": count})
		if lockedUntil != nil && lockedUntil.After(time.Now()) {
			return nil, &LockedError{Until: *lockedUntil}
		}
		return nil, ErrInvalidCredentials
	}

	methods, mfaRequired, err := s.mfaMethods(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if mfaRequired {
		token, _, err := NewToken()
		if err != nil {
			return nil, err
		}
		s.state.put("mfa:"+token, &pendingMFA{UserID: user.ID, Methods: methods, IP: ip}, mfaPendingTTL)
		s.audit.Log(ctx, actor, "auth.login", "", audit.OutcomeSuccess, map[string]any{"stage": "first_factor", "mfa": true})
		return &LoginOutcome{Status: "mfa_required", MFAToken: token, Methods: methods}, nil
	}

	return s.finishLogin(ctx, user, ip, userAgent, "password")
}

// mfaMethods reports the second-factor methods available to a user and
// whether a second factor is required (it is, whenever TOTP is enabled).
func (s *Service) mfaMethods(ctx context.Context, userID uuid.UUID) ([]string, bool, error) {
	cred, err := s.store.GetTOTP(ctx, userID)
	totpEnabled := err == nil && cred.ConfirmedAt != nil
	if err != nil && err != store.ErrNotFound {
		return nil, false, err
	}
	if !totpEnabled {
		return nil, false, nil
	}
	methods := []string{"totp"}
	passkeys, err := s.store.ListWebauthnCredentials(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	if len(passkeys) > 0 {
		methods = append(methods, "passkey")
	}
	if n, err := s.store.CountUnusedRecoveryCodes(ctx, userID); err == nil && n > 0 {
		methods = append(methods, "recovery")
	}
	return methods, true, nil
}

// takeMFAAttempt validates and updates pending-MFA state for one attempt.
func (s *Service) takeMFAAttempt(mfaToken string) (*pendingMFA, error) {
	v, ok := s.state.peek("mfa:" + mfaToken)
	if !ok {
		return nil, ErrMFAExpired
	}
	pending := v.(*pendingMFA)
	pending.Attempts++
	if pending.Attempts > mfaMaxAttempts {
		s.state.delete("mfa:" + mfaToken)
		return nil, ErrMFAExpired
	}
	return pending, nil
}

// CompleteTOTP finishes login with a TOTP code.
func (s *Service) CompleteTOTP(ctx context.Context, mfaToken, code, ip, userAgent string) (*LoginOutcome, error) {
	if !s.ipLimiter.Allow("ip:" + ip) {
		return nil, ErrRateLimited
	}
	pending, err := s.takeMFAAttempt(mfaToken)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(ctx, pending.UserID)
	if err != nil {
		return nil, ErrMFAExpired
	}
	if err := s.verifyTOTPCode(ctx, user.ID, code); err != nil {
		s.audit.Log(ctx, audit.Actor{Kind: "user", ID: &user.ID, Name: user.Username, IP: ip},
			"auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "bad_totp"})
		return nil, err
	}
	s.state.delete("mfa:" + mfaToken)
	return s.finishLogin(ctx, user, ip, userAgent, "totp")
}

// verifyTOTPCode checks a code against the user's confirmed secret, with a
// short replay guard so an intercepted code cannot be reused in its window.
func (s *Service) verifyTOTPCode(ctx context.Context, userID uuid.UUID, code string) error {
	cred, err := s.store.GetTOTP(ctx, userID)
	if err != nil || cred.ConfirmedAt == nil {
		return ErrInvalidCode
	}
	secret, err := s.vault.Decrypt(cred.SecretCiphertext)
	if err != nil {
		s.logger.Error("decrypt totp secret", "error", err)
		return ErrInvalidCode
	}
	replayKey := fmt.Sprintf("totp-used:%s:%s", userID, code)
	if _, used := s.state.peek(replayKey); used {
		return ErrInvalidCode
	}
	if !totp.Validate(code, string(secret)) {
		return ErrInvalidCode
	}
	s.state.put(replayKey, true, 95*time.Second)
	return nil
}

// CompleteRecovery finishes login with a single-use recovery code.
func (s *Service) CompleteRecovery(ctx context.Context, mfaToken, code, ip, userAgent string) (*LoginOutcome, error) {
	if !s.ipLimiter.Allow("ip:" + ip) {
		return nil, ErrRateLimited
	}
	pending, err := s.takeMFAAttempt(mfaToken)
	if err != nil {
		return nil, err
	}
	user, err := s.store.GetUser(ctx, pending.UserID)
	if err != nil {
		return nil, ErrMFAExpired
	}
	used, err := s.store.ConsumeRecoveryCode(ctx, user.ID, HashToken(normalizeRecoveryCode(code)))
	if err != nil {
		return nil, err
	}
	if !used {
		s.audit.Log(ctx, audit.Actor{Kind: "user", ID: &user.ID, Name: user.Username, IP: ip},
			"auth.login", "", audit.OutcomeFailure, map[string]any{"reason": "bad_recovery_code"})
		return nil, ErrInvalidCode
	}
	s.state.delete("mfa:" + mfaToken)

	remaining, _ := s.store.CountUnusedRecoveryCodes(ctx, user.ID)
	s.audit.Log(ctx, audit.Actor{Kind: "user", ID: &user.ID, Name: user.Username, IP: ip},
		"auth.recovery_code_used", "", audit.OutcomeSuccess, map[string]any{"remaining": remaining})
	return s.finishLogin(ctx, user, ip, userAgent, "recovery")
}

// finishLogin records success and issues a session.
func (s *Service) finishLogin(ctx context.Context, user *store.User, ip, userAgent, method string) (*LoginOutcome, error) {
	if err := s.store.RecordLoginSuccess(ctx, user.ID); err != nil {
		return nil, err
	}
	token, sess, err := s.IssueSession(ctx, user.ID, ip, userAgent)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, audit.Actor{Kind: "user", ID: &user.ID, Name: user.Username, SessionID: &sess.ID, IP: ip},
		"auth.login", "", audit.OutcomeSuccess, map[string]any{"method": method})
	return &LoginOutcome{Status: "ok", User: user, SessionToken: token, Session: sess}, nil
}

// IssueSession creates a session and returns the opaque bearer token.
func (s *Service) IssueSession(ctx context.Context, userID uuid.UUID, ip, userAgent string) (string, *store.Session, error) {
	token, hash, err := NewToken()
	if err != nil {
		return "", nil, err
	}
	sess, err := s.store.CreateSession(ctx, userID, hash, time.Now().Add(s.settings.SessionTTL(ctx)), ip, userAgent)
	if err != nil {
		return "", nil, err
	}
	return token, sess, nil
}

// ValidateSession resolves a presented token to a live session and user,
// refreshing the rolling expiry at most once a minute.
func (s *Service) ValidateSession(ctx context.Context, token, ip string) (*store.Session, *store.User, error) {
	sess, user, err := s.store.SessionByTokenHash(ctx, HashToken(token))
	if err != nil {
		return nil, nil, err
	}
	if time.Since(sess.LastSeenAt) > time.Minute {
		if terr := s.store.TouchSession(ctx, sess.ID, ip, time.Now().Add(s.settings.SessionTTL(ctx))); terr != nil {
			s.logger.Warn("touch session", "error", terr)
		}
	}
	return sess, user, nil
}

// Logout revokes the current session.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID, actor audit.Actor) {
	if err := s.store.RevokeSession(ctx, sessionID); err != nil && err != store.ErrNotFound {
		s.logger.Error("revoke session", "error", err)
	}
	s.audit.Log(ctx, actor, "auth.logout", "", audit.OutcomeSuccess, nil)
}

// ChangePassword verifies the current password, sets the new one, and revokes
// every other session.
func (s *Service) ChangePassword(ctx context.Context, user *store.User, currentSessionID uuid.UUID, current, newPassword string, actor audit.Actor) error {
	if user.PasswordHash == nil {
		return &ValidationError{Msg: "this account has no password set"}
	}
	ok, err := VerifyPassword(current, *user.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		s.audit.Log(ctx, actor, "auth.password_change", "", audit.OutcomeFailure, map[string]any{"reason": "bad_current_password"})
		return ErrInvalidCredentials
	}
	if err := ValidatePassword(newPassword); err != nil {
		return &ValidationError{Msg: err.Error()}
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetUserPassword(ctx, user.ID, hash, false); err != nil {
		return err
	}
	if _, err := s.store.RevokeUserSessions(ctx, user.ID, &currentSessionID); err != nil {
		s.logger.Error("revoke other sessions after password change", "error", err)
	}
	s.audit.Log(ctx, actor, "auth.password_change", "", audit.OutcomeSuccess, nil)
	return nil
}

// EnrollTOTP begins TOTP setup, returning the secret and provisioning URI.
func (s *Service) EnrollTOTP(ctx context.Context, user *store.User) (secret, otpauthURL string, err error) {
	existing, err := s.store.GetTOTP(ctx, user.ID)
	if err == nil && existing.ConfirmedAt != nil {
		return "", "", &ConflictError{Msg: "two-factor authentication is already enabled; disable it first"}
	}
	if err != nil && err != store.ErrNotFound {
		return "", "", err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: user.Username})
	if err != nil {
		return "", "", err
	}
	ciphertext, err := s.vault.Encrypt([]byte(key.Secret()))
	if err != nil {
		return "", "", err
	}
	if err := s.store.UpsertTOTP(ctx, user.ID, ciphertext); err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ConfirmTOTP activates a pending enrollment and returns fresh recovery codes.
func (s *Service) ConfirmTOTP(ctx context.Context, user *store.User, code string, actor audit.Actor) ([]string, error) {
	cred, err := s.store.GetTOTP(ctx, user.ID)
	if err != nil {
		return nil, &ValidationError{Msg: "no enrollment in progress — start again"}
	}
	if cred.ConfirmedAt != nil {
		return nil, &ConflictError{Msg: "two-factor authentication is already enabled"}
	}
	secret, err := s.vault.Decrypt(cred.SecretCiphertext)
	if err != nil {
		return nil, err
	}
	if !totp.Validate(code, string(secret)) {
		return nil, ErrInvalidCode
	}
	if err := s.store.ConfirmTOTP(ctx, user.ID); err != nil {
		return nil, err
	}
	codes, err := s.issueRecoveryCodes(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actor, "auth.totp_enabled", "", audit.OutcomeSuccess, nil)
	return codes, nil
}

// DisableTOTP removes TOTP and recovery codes after password confirmation.
func (s *Service) DisableTOTP(ctx context.Context, user *store.User, password string, actor audit.Actor) error {
	if err := s.confirmPassword(user, password); err != nil {
		return err
	}
	if err := s.store.DeleteTOTP(ctx, user.ID); err != nil {
		return err
	}
	if err := s.store.DeleteRecoveryCodes(ctx, user.ID); err != nil {
		return err
	}
	s.audit.Log(ctx, actor, "auth.totp_disabled", "", audit.OutcomeSuccess, nil)
	return nil
}

// RegenerateRecoveryCodes replaces all recovery codes after password
// confirmation.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, user *store.User, password string, actor audit.Actor) ([]string, error) {
	if err := s.confirmPassword(user, password); err != nil {
		return nil, err
	}
	cred, err := s.store.GetTOTP(ctx, user.ID)
	if err != nil || cred.ConfirmedAt == nil {
		return nil, &ValidationError{Msg: "two-factor authentication is not enabled"}
	}
	codes, err := s.issueRecoveryCodes(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actor, "auth.recovery_codes_regenerated", "", audit.OutcomeSuccess, nil)
	return codes, nil
}

func (s *Service) issueRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	codes := make([]string, 0, 10)
	hashes := make([][]byte, 0, 10)
	for range 10 {
		code, err := NewRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
		hashes = append(hashes, HashToken(normalizeRecoveryCode(code)))
	}
	if err := s.store.ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// confirmPassword re-authenticates a sensitive action with the account
// password, rate-limited per user.
func (s *Service) confirmPassword(user *store.User, password string) error {
	if !s.userLimiter.Allow("confirm:" + user.ID.String()) {
		return ErrRateLimited
	}
	if user.PasswordHash == nil {
		return ErrInvalidCredentials
	}
	ok, err := VerifyPassword(password, *user.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	return nil
}

func normalizeRecoveryCode(code string) string {
	out := make([]byte, 0, len(code))
	for _, c := range code {
		if c >= '0' && c <= '9' {
			out = append(out, byte(c))
		}
	}
	return string(out)
}

// TOTPEnabled reports whether a user has confirmed TOTP.
func (s *Service) TOTPEnabled(ctx context.Context, userID uuid.UUID) bool {
	cred, err := s.store.GetTOTP(ctx, userID)
	return err == nil && cred.ConfirmedAt != nil
}
