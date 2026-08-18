package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"homehub/internal/assistant"
	"homehub/internal/httpctx"
	"homehub/internal/rbac"
	"homehub/internal/telegram"
)

const telegramVaultScope = "telegram"
const telegramTokenName = "bot-token"

// telegramBot is injected after construction (the bot needs the server as
// its TurnRunner, so the two meet in app wiring).
func (s *Server) SetTelegramBot(b *telegram.Bot) { s.telegram = b }

// RunAssistantTurnFor executes one assistant turn for a user reached over a
// messaging bridge: same provider config, same tools, same permissions as
// the in-app chat — the identity is just built from the stored user row.
func (s *Server) RunAssistantTurnFor(ctx context.Context, userID uuid.UUID, message string) (string, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("the linked user no longer exists")
	}
	if user.Status == "disabled" {
		return "", fmt.Errorf("the linked user is disabled")
	}
	id := &httpctx.Identity{User: user, IP: "telegram"}

	enabled, providerName, model, baseURL := s.settings.Assistant(ctx)
	if !enabled {
		return "", fmt.Errorf("the assistant is not enabled in HomeHub")
	}
	info := assistant.ProviderByID(providerName)
	if info == nil {
		return "", fmt.Errorf("the configured AI provider no longer exists")
	}
	apiKey, _ := s.assistantKeyFor(ctx, providerName)
	if info.RequiresKey && apiKey == "" {
		return "", fmt.Errorf("the assistant has no API key configured")
	}
	cfg := assistant.Config{Provider: providerName, Model: model, APIKey: apiKey, BaseURL: baseURL}
	provider, err := assistant.NewProviderFor(cfg)
	if err != nil {
		return "", err
	}
	svc := &assistant.Service{
		Store: s.store,
		Toolbox: &assistant.Toolbox{
			Store:   s.store,
			Metrics: s.metrics,
			Call: func(cctx context.Context, method, path string, body any) (int, []byte) {
				return s.SelfCall(cctx, id, method, path, body)
			},
		},
	}
	reply, _, err := svc.RunTurn(ctx, provider, cfg, s.assistantAccess(ctx, id), message)
	return reply, err
}

func (s *Server) telegramToken(ctx context.Context) string {
	raw, err := s.vault.Open(ctx, telegramVaultScope, telegramTokenName)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (s *Server) GetTelegramSettings(ctx context.Context, _ GetTelegramSettingsRequestObject) (GetTelegramSettingsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	out := GetTelegramSettings200JSONResponse{
		Enabled:      s.settings.Telegram(ctx),
		TokenSet:     s.telegramToken(ctx) != "",
		CanConfigure: s.can(ctx, id, ResSettings, rbac.LevelConfigure),
		Chats:        []TelegramChat{},
	}
	if s.telegram != nil {
		username, lastErr := s.telegram.Status()
		if username != "" {
			out.BotUsername = &username
		}
		if lastErr != "" {
			out.BotError = &lastErr
		}
	}
	chats, err := s.store.ListTelegramChats(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range chats {
		row := TelegramChat{ChatId: c.ChatID, Label: c.Label, UserId: c.UserID, LinkedAt: c.CreatedAt}
		if u, err := s.store.GetUser(ctx, c.UserID); err == nil {
			row.UserName = u.DisplayName
		}
		out.Chats = append(out.Chats, row)
	}
	return out, nil
}

func (s *Server) PutTelegramSettings(ctx context.Context, req PutTelegramSettingsRequestObject) (PutTelegramSettingsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "telegram.configure", ResSettings)
		return PutTelegramSettings403JSONResponse{forbidden()}, nil
	}
	if req.Body.Token != nil {
		token := strings.TrimSpace(*req.Body.Token)
		if token != "" {
			if err := s.vault.Seal(ctx, telegramVaultScope, telegramTokenName, []byte(token)); err != nil {
				return nil, err
			}
		}
	}
	if err := s.settings.SetTelegram(ctx, req.Body.Enabled); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "telegram.settings", "settings/telegram", "ok", map[string]any{"enabled": req.Body.Enabled})
	if s.telegram != nil {
		s.telegram.Reload(ctx)
	}
	return PutTelegramSettings204Response{}, nil
}

// TelegramLinkCode confirms a code the BOT issued to a chat. The direction
// matters for safety: the secret flows from Telegram into an authenticated
// web session, and the chat binds to the CONFIRMING user — so possession of
// a HomeHub login is required to link anything, and a guessed code could
// only ever link a chat to the guesser's own account.
func (s *Server) TelegramLinkCode(ctx context.Context, req TelegramLinkCodeRequestObject) (TelegramLinkCodeResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if s.telegram == nil {
		return TelegramLinkCode409JSONResponse(errBody("telegram_off", "the Telegram bridge is not running")), nil
	}
	chatID, label, err := s.telegram.Claim(id.User.ID, req.Body.Code)
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "telegram.link", "settings/telegram", "denied", map[string]any{"error": err.Error()})
		return TelegramLinkCode409JSONResponse(errBody("link_failed", err.Error())), nil
	}
	if err := s.store.AddTelegramChat(ctx, chatID, id.User.ID, label); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "telegram.link", "settings/telegram", "ok", map[string]any{"chatId": chatID})
	s.telegram.NotifyLinked(ctx, chatID, id.User.DisplayName)
	return TelegramLinkCode200JSONResponse{Label: label}, nil
}

func (s *Server) TelegramUnlinkChat(ctx context.Context, req TelegramUnlinkChatRequestObject) (TelegramUnlinkChatResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "telegram.unlink", ResSettings)
		return TelegramUnlinkChat403JSONResponse{forbidden()}, nil
	}
	if err := s.store.DeleteTelegramChat(ctx, req.ChatId); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "telegram.unlink", "settings/telegram", "ok", map[string]any{"chatId": req.ChatId})
	return TelegramUnlinkChat204Response{}, nil
}
