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

	"homehub/internal/store"
)

const (
	apiBase      = "https://api.telegram.org/bot"
	linkCodeTTL  = 10 * time.Minute
	pollTimeout  = 50 // seconds, Telegram-side long-poll hold
	replyLimit   = 4000
	turnDeadline = 3 * time.Minute
)

// TurnRunner executes one assistant turn for a given HomeHub user. The API
// server implements it; the bot stays free of provider plumbing.
type TurnRunner interface {
	RunAssistantTurnFor(ctx context.Context, userID uuid.UUID, message string) (string, error)
}

type linkCode struct {
	userID  uuid.UUID
	userTag string
	expires time.Time
}

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
	codes    map[string]linkCode

	client http.Client
}

// Status reports what the settings panel shows: the bot identity once the
// token validated, or the last error trying.
func (b *Bot) Status() (username, lastErr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.username, b.lastErr
}

// MintLinkCode creates a short-lived code that links the chat which sends
// "/link <code>" to the given user.
func (b *Bot) MintLinkCode(userID uuid.UUID, userTag string) (code string, expires time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.codes == nil {
		b.codes = map[string]linkCode{}
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	code = fmt.Sprintf("%06d", n.Int64())
	expires = time.Now().Add(linkCodeTTL)
	b.codes[code] = linkCode{userID: userID, userTag: userTag, expires: expires}
	return code, expires
}

func (b *Bot) takeLinkCode(code string) (linkCode, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	lc, ok := b.codes[code]
	if ok {
		delete(b.codes, code)
	}
	if !ok || time.Now().After(lc.expires) {
		return linkCode{}, false
	}
	return lc, true
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

	// "/link 123456" pairs this chat with the user who minted the code.
	if code, ok := strings.CutPrefix(text, "/link"); ok {
		code = strings.TrimSpace(code)
		lc, valid := b.takeLinkCode(code)
		if !valid {
			b.send(ctx, token, chatID, "That link code is unknown or expired. Mint a fresh one in HomeHub → Assistant → Telegram.")
			return
		}
		label := msg.Chat.Title
		if label == "" {
			label = strings.TrimSpace(msg.From.FirstName)
			if msg.From.Username != "" {
				label += " (@" + msg.From.Username + ")"
			}
		}
		if err := b.Store.AddTelegramChat(ctx, chatID, lc.userID, label); err != nil {
			b.send(ctx, token, chatID, "Linking failed on the HomeHub side — check the server logs.")
			return
		}
		b.send(ctx, token, chatID, fmt.Sprintf("Linked to HomeHub as %s. Plain messages talk to the assistant; start one with ! or / to trigger commands.", lc.userTag))
		return
	}

	chat, err := b.Store.GetTelegramChat(ctx, chatID)
	if err != nil {
		// Unlinked chat: answer only in private chats so the bot cannot be
		// spammed into groups it was dragged into.
		if msg.Chat.Type == "private" {
			b.send(ctx, token, chatID, "This bot is private. Link this chat from HomeHub → Assistant → Telegram, then send /link <code> here.")
		}
		return
	}

	if strings.HasPrefix(text, "!") || strings.HasPrefix(text, "/") {
		trigger := strings.Fields(text)[0]
		if trigger == "/start" || trigger == "/help" {
			b.send(ctx, token, chatID, "Linked to HomeHub. Plain messages go to the assistant, which answers here and can act on your homelab. Messages starting with ! or / are command triggers — command routing is not configured yet.")
			return
		}
		b.send(ctx, token, chatID, fmt.Sprintf("Command trigger %q noted — command routing is not configured yet. Plain messages talk to the assistant.", trigger))
		return
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
