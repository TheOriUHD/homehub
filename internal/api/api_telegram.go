package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/assistant"
	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/telegram"
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

// SystemStatusFor renders the /status summary: every system's health, DB and
// jobs, under the linked user's own permissions (health/view). Problems are
// listed first, matching the app's lead-with-the-bad-news ethos.
func (s *Server) SystemStatusFor(ctx context.Context, userID uuid.UUID) (string, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("the linked user no longer exists")
	}
	id := &httpctx.Identity{User: user, IP: "telegram"}

	status, raw := s.SelfCall(ctx, id, http.MethodGet, "/health", nil)
	if status == http.StatusForbidden {
		return "", fmt.Errorf("you do not have permission to view system health")
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("health check failed (%d)", status)
	}
	var rep HealthReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return "", err
	}

	icon := map[string]string{"connected": "🟢", "degraded": "🟡", "unreachable": "🔴"}
	rank := map[string]int{"unreachable": 0, "degraded": 1, "connected": 2}
	sort.SliceStable(rep.Integrations, func(i, j int) bool {
		si, sj := string(rep.Integrations[i].Status), string(rep.Integrations[j].Status)
		if rank[si] != rank[sj] {
			return rank[si] < rank[sj]
		}
		return rep.Integrations[i].Name < rep.Integrations[j].Name
	})

	var b strings.Builder
	healthy := 0
	for _, in := range rep.Integrations {
		if in.Status == "connected" {
			healthy++
		}
	}
	fmt.Fprintf(&b, "HomeHub status — %d/%d systems healthy\n", healthy, len(rep.Integrations))
	if len(rep.Integrations) == 0 {
		b.WriteString("\nNo systems configured yet.\n")
	}
	for _, in := range rep.Integrations {
		mark := icon[string(in.Status)]
		if mark == "" {
			mark = "⚪️"
		}
		fmt.Fprintf(&b, "%s %s (%s)", mark, in.Name, in.Type)
		if in.Status != "connected" {
			fmt.Fprintf(&b, " — %s", in.Status)
			if in.LastError != nil && *in.LastError != "" {
				fmt.Fprintf(&b, ": %s", trimLine(*in.LastError, 80))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if rep.Database.Reachable {
		fmt.Fprintf(&b, "🟢 Database — %.0f ms\n", rep.Database.LatencyMs)
	} else {
		b.WriteString("🔴 Database — unreachable\n")
	}
	jobsMark := "🟢"
	if rep.Jobs.FailedRecent > 0 {
		jobsMark = "🟡"
	}
	fmt.Fprintf(&b, "%s Jobs — %d running, %d pending, %d failed recently\n",
		jobsMark, rep.Jobs.Running, rep.Jobs.Pending, rep.Jobs.FailedRecent)
	fmt.Fprintf(&b, "⏱ Uptime %s · v%s", humanizeUptime(rep.UptimeSeconds), rep.Version)
	return b.String(), nil
}

func trimLine(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func humanizeUptime(sec int64) string {
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
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
