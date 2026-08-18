// Package telegram bridges Telegram chats to the HomeHub assistant. One bot
// (token from @BotFather, stored in the vault) long-polls getUpdates — no
// inbound exposure needed. Chats link to a HomeHub user via a short-lived
// code minted in the UI; linked chats talk to the assistant under that
// user's own permissions. Messages starting with '!' or '/' are command
// triggers (routing configured separately); everything else is a plain
// assistant turn answered right back into the chat.
package telegram

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

const (
	apiBase      = "https://api.telegram.org/bot"
	linkCodeTTL  = 10 * time.Minute
	pollTimeout  = 50 // seconds, Telegram-side long-poll hold
	replyLimit   = 4000
	turnDeadline = 3 * time.Minute
)

// TurnRunner is the bot's window into HomeHub, always scoped to one user's
// permissions. The API server implements it; the bot stays free of provider
// and RBAC plumbing.
type TurnRunner interface {
	// RunAssistantTurnFor answers a plain message via the LLM tool loop.
	RunAssistantTurnFor(ctx context.Context, userID uuid.UUID, message string) (string, error)
	// SystemStatusFor returns a formatted health summary of every system the
	// user may see (the /status command — deterministic, no LLM).
	SystemStatusFor(ctx context.Context, userID uuid.UUID) (string, error)
}

// pendingLink is a chat that has asked to be linked and is waiting for the
// code to be confirmed inside an authenticated HomeHub session. The code
// binds nothing on its own — it only names which chat a signed-in user is
// claiming, so guessing it is worthless without a HomeHub login.
type pendingLink struct {
	chatID  int64
	label   string
	expires time.Time
}

// claimAttempts rate-limits code confirmation per HomeHub user, so the code
// space cannot be brute-forced even from a valid (low-privilege) session.
type claimAttempts struct {
	count   int
	resetAt time.Time
}

const (
	maxClaimAttempts = 6
	claimWindow      = 10 * time.Minute
	// Unambiguous alphabet (no O/0/I/1); 8 chars ≈ 32^8 ≈ 1.1e12 space.
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLength   = 8
)

// Bot is the bridge. Start it once; Reload after settings change.
type Bot struct {
	Store  *store.Store
	Runner TurnRunner
	Logger *slog.Logger

	// Token and Enabled are read through these callbacks so a Reload picks
	// up fresh values without plumbing settings/vault types in here.
	TokenFn   func(ctx context.Context) string
	EnabledFn func(ctx context.Context) bool

	mu       sync.Mutex
	cancel   context.CancelFunc
	username string
	lastErr  string
	pending  map[string]pendingLink
	attempts map[uuid.UUID]*claimAttempts

	client http.Client
}

// Status reports what the settings panel shows: the bot identity once the
// token validated, or the last error trying.
func (b *Bot) Status() (username, lastErr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.username, b.lastErr
}

func randomCode() string {
	buf := make([]byte, codeLength)
	for i := range buf {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		buf[i] = codeAlphabet[n.Int64()]
	}
	return string(buf)
}

func (b *Bot) purgeExpiredLocked() {
	now := time.Now()
	for code, p := range b.pending {
		if now.After(p.expires) {
			delete(b.pending, code)
		}
	}
}

// mintChatCode issues the code a chat must have entered on the web side to
// be linked. Shown ONLY in the requesting chat; one pending per chat.
func (b *Bot) mintChatCode(chatID int64, label string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[string]pendingLink{}
	}
	b.purgeExpiredLocked()
	for code, p := range b.pending {
		if p.chatID == chatID {
			delete(b.pending, code)
		}
	}
	code := randomCode()
	b.pending[code] = pendingLink{chatID: chatID, label: label, expires: time.Now().Add(linkCodeTTL)}
	return code
}

// Claim resolves a code entered by a signed-in HomeHub user to the chat that
// requested it. The API layer writes the binding to that user. Per-user
// rate-limited; the code is single-use and expires. Because the confirming
// user becomes the owner, an attacker's chat can never attach to someone
// else's account — that would require the victim to type the attacker's
// chat code into the victim's own session.
func (b *Bot) Claim(userID uuid.UUID, code string) (chatID int64, label string, err error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.attempts == nil {
		b.attempts = map[uuid.UUID]*claimAttempts{}
	}
	now := time.Now()
	a := b.attempts[userID]
	if a == nil || now.After(a.resetAt) {
		a = &claimAttempts{resetAt: now.Add(claimWindow)}
		b.attempts[userID] = a
	}
	if a.count >= maxClaimAttempts {
		return 0, "", fmt.Errorf("too many attempts — wait a few minutes, then send /link to the bot for a fresh code")
	}
	b.purgeExpiredLocked()
	p, ok := b.pending[code]
	if !ok {
		a.count++
		return 0, "", fmt.Errorf("that code is unknown or expired — send /link to the bot for a fresh one")
	}
	delete(b.pending, code)
	delete(b.attempts, userID)
	return p.chatID, p.label, nil
}

// Reload applies the current settings: stops any running poller, then starts
// a new one when the bridge is enabled and a token is present.
func (b *Bot) Reload(ctx context.Context) {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.username, b.lastErr = "", ""
	token := ""
	if b.TokenFn != nil {
		token = b.TokenFn(ctx)
	}
	enabled := b.EnabledFn != nil && b.EnabledFn(ctx)
	if token == "" || !enabled {
		if enabled && token == "" {
			b.lastErr = "no bot token stored"
		}
		b.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.mu.Unlock()

	go b.run(runCtx, token)
}

func (b *Bot) run(ctx context.Context, token string) {
	// Validate the token and learn the bot's identity first.
	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, token, "getMe", nil, &me); err != nil {
		b.mu.Lock()
		b.lastErr = "getMe failed: " + err.Error()
		b.mu.Unlock()
		b.Logger.Warn("telegram bot token rejected", "error", err)
		return
	}
	b.mu.Lock()
	b.username, b.lastErr = me.Username, ""
	b.mu.Unlock()
	b.Logger.Info("telegram bridge up", "bot", "@"+me.Username)

	// Advertise the commands so they appear in Telegram's command menu.
	_ = b.call(ctx, token, "setMyCommands", url.Values{
		"commands": {`[{"command":"status","description":"Health of every system"},{"command":"link","description":"Get a code to link this chat to HomeHub"},{"command":"unlink","description":"Disconnect this chat from HomeHub"},{"command":"help","description":"How the bridge works"}]`},
	}, nil)

	offset := int64(0)
	for ctx.Err() == nil {
		updates, err := b.getUpdates(ctx, token, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.mu.Lock()
			b.lastErr = err.Error()
			b.mu.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}
		b.mu.Lock()
		b.lastErr = ""
		b.mu.Unlock()
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message != nil && u.Message.Text != "" {
				go b.handleMessage(ctx, token, u.Message)
			}
		}
	}
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	Chat struct {
		ID    int64  `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"chat"`
	From struct {
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	} `json:"from"`
	Text string `json:"text"`
}

func (b *Bot) getUpdates(ctx context.Context, token string, offset int64) ([]tgUpdate, error) {
	var out []tgUpdate
	err := b.call(ctx, token, "getUpdates", url.Values{
		"timeout":         {fmt.Sprint(pollTimeout)},
		"offset":          {fmt.Sprint(offset)},
		"allowed_updates": {`["message"]`},
	}, &out)
	return out, err
}

func (b *Bot) handleMessage(ctx context.Context, token string, msg *tgMessage) {
	text := strings.TrimSpace(msg.Text)
	chatID := msg.Chat.ID

	// "/link": the chat asks to be linked and receives a code. The binding
	// only happens when a signed-in HomeHub user confirms that code in the
	// web UI — the secret flows Telegram → web, never the other way, so
	// nothing an attacker sends the bot can ever attach a chat to an
	// account. Guessing is pointless: confirming a code links the chat to
	// the CONFIRMING user's own account.
	if strings.HasPrefix(text, "/link") {
		if _, err := b.Store.GetTelegramChat(ctx, chatID); err == nil {
			b.send(ctx, token, chatID, "This chat is already linked. An administrator can unlink it in HomeHub → Assistant → Telegram.")
			return
		}
		label := msg.Chat.Title
		if label == "" {
			label = strings.TrimSpace(msg.From.FirstName)
			if msg.From.Username != "" {
				label += " (@" + msg.From.Username + ")"
			}
		}
		code := b.mintChatCode(chatID, label)
		b.send(ctx, token, chatID, fmt.Sprintf("Link code: %s\n\nEnter it in HomeHub → Assistant → Telegram while signed in there. It expires in 10 minutes. If you did not request this, ignore it — the code cannot do anything on its own.", code))
		return
	}

	chat, err := b.Store.GetTelegramChat(ctx, chatID)
	if err != nil {
		// Unlinked chat: answer only in private chats so the bot cannot be
		// spammed into groups it was dragged into.
		if msg.Chat.Type == "private" {
			b.send(ctx, token, chatID, "This bot is private. To link this chat, send /link here and enter the code it gives you in HomeHub → Assistant → Telegram.")
		}
		return
	}

	if strings.HasPrefix(text, "!") || strings.HasPrefix(text, "/") {
		trigger := strings.ToLower(strings.TrimPrefix(strings.Fields(text)[0], "!"))
		trigger = strings.TrimPrefix(trigger, "/")
		switch trigger {
		case "unlink":
			// Self-service: you control this chat, so you can sever its link
			// to HomeHub from here (revokes this chat's assistant access).
			if err := b.Store.DeleteTelegramChat(ctx, chatID); err != nil {
				b.send(ctx, token, chatID, "Unlinking failed on the HomeHub side — check the server logs.")
				return
			}
			b.send(ctx, token, chatID, "This chat is now unlinked. It can no longer talk to HomeHub. Send /link to connect it again.")
			return
		case "status":
			stop := b.typing(ctx, token, chatID)
			out, err := b.Runner.SystemStatusFor(ctx, chat.UserID)
			stop()
			if err != nil {
				b.send(ctx, token, chatID, "Could not read system status: "+err.Error())
				return
			}
			b.send(ctx, token, chatID, out)
			return
		case "start", "help":
			b.send(ctx, token, chatID, "Linked to HomeHub. Plain messages go to the assistant, which answers here and can act on your homelab.\n\nCommands:\n/status — health of every system\n/unlink — disconnect this chat from HomeHub\n/help — show this\n\nMore command triggers (! or /) are coming.")
			return
		default:
			b.send(ctx, token, chatID, fmt.Sprintf("Unknown command %q. Try /help. Plain messages talk to the assistant.", "/"+trigger))
			return
		}
	}

	// Assistant turn. Show "typing…" while the agent works.
	stop := b.typing(ctx, token, chatID)
	defer stop()
	turnCtx, cancel := context.WithTimeout(ctx, turnDeadline)
	defer cancel()
	reply, err := b.Runner.RunAssistantTurnFor(turnCtx, chat.UserID, text)
	if err != nil {
		b.send(ctx, token, chatID, "The assistant could not answer: "+err.Error())
		return
	}
	if len(reply) > replyLimit {
		reply = reply[:replyLimit] + "…"
	}
	// Try Markdown first (the assistant answers in it); Telegram rejects
	// malformed markup with a 400, in which case plain text still lands.
	if err := b.sendParsed(ctx, token, chatID, reply, "Markdown"); err != nil {
		b.send(ctx, token, chatID, reply)
	}
}

// typing keeps the "typing…" chat action alive until stopped.
func (b *Bot) typing(ctx context.Context, token string, chatID int64) func() {
	tctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			_ = b.call(tctx, token, "sendChatAction", url.Values{
				"chat_id": {fmt.Sprint(chatID)},
				"action":  {"typing"},
			}, nil)
			select {
			case <-tctx.Done():
				return
			case <-time.After(4 * time.Second):
			}
		}
	}()
	return cancel
}

func (b *Bot) send(ctx context.Context, token string, chatID int64, text string) {
	_ = b.sendParsed(ctx, token, chatID, text, "")
}

func (b *Bot) sendParsed(ctx context.Context, token string, chatID int64, text, parseMode string) error {
	v := url.Values{"chat_id": {fmt.Sprint(chatID)}, "text": {text}}
	if parseMode != "" {
		v.Set("parse_mode", parseMode)
	}
	return b.call(ctx, token, "sendMessage", v, nil)
}

// call POSTs one Bot API method and decodes result into out when non-nil.
func (b *Bot) call(ctx context.Context, token, method string, params url.Values, out any) error {
	body := strings.NewReader(params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+token+"/"+method, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("telegram: unparsable response (http %d)", res.StatusCode)
	}
	if !envelope.OK {
		if envelope.Description != "" {
			return fmt.Errorf("telegram: %s", envelope.Description)
		}
		return fmt.Errorf("telegram: http %d", res.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}

// NotifyLinked tells a freshly linked chat who it now belongs to.
func (b *Bot) NotifyLinked(ctx context.Context, chatID int64, userTag string) {
	token := ""
	if b.TokenFn != nil {
		token = b.TokenFn(ctx)
	}
	if token == "" {
		return
	}
	b.send(ctx, token, chatID, fmt.Sprintf("Linked to HomeHub as %s. Plain messages talk to the assistant; start one with ! or / to trigger commands.", userTag))
}
