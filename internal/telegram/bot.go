// Package telegram bridges Telegram chats to the HomeHub assistant. One bot
// (token from @BotFather, stored in the vault) long-polls getUpdates — no
// inbound exposure needed. Chats link to a HomeHub user via a short-lived
// code minted in the UI; linked chats talk to the assistant under that
// user's own permissions. Messages starting with '!' or '/' are command
// triggers; everything else is a plain assistant turn answered right back
// into the chat.
//
// The agent can do things here, not only answer — including running commands
// over SSH. The dangerous half of that is gated exactly as it is in the app:
// read-only commands run unattended, anything that changes a machine is held
// as a proposal, and catastrophic ones are refused outright. A chat has no
// approve button, so the proposal waits for /yes or /no. Either answer goes
// back to the agent: a refusal is a turn of the conversation, not a dead end,
// so it can offer something narrower instead.
package telegram

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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

// ErrNoPending is what an approval answers with when there is nothing left to
// approve — the proposal expired, was already answered, or never existed. The
// bridge says so rather than leaving the person wondering what their /yes did.
var ErrNoPending = errors.New("nothing is waiting for approval")

// TurnRunner is the bot's window into HomeHub, always scoped to one user's
// permissions. The API server implements it; the bot stays free of provider
// and RBAC plumbing.
//
// The three turn methods share a shape: the agent's reply, plus a summary line
// for anything it now wants approved. A non-empty second return means the
// chat is holding a proposal and the next /yes or /no belongs to it.
type TurnRunner interface {
	// RunAssistantTurnFor answers a plain message via the LLM tool loop.
	RunAssistantTurnFor(ctx context.Context, userID uuid.UUID, message string) (string, []string, error)
	// ConfirmPendingFor runs the held proposal and returns what the agent
	// makes of the result. ErrNoPending when there was nothing to run.
	ConfirmPendingFor(ctx context.Context, userID uuid.UUID) (string, []string, error)
	// DeclinePendingFor discards the held proposal and lets the agent answer
	// the refusal. ErrNoPending when there was nothing to decline.
	DeclinePendingFor(ctx context.Context, userID uuid.UUID) (string, []string, error)
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
	// awaiting marks chats currently holding a proposal, so a bare "yes" can
	// mean what it obviously means. Only a hint for that shortcut: HomeHub
	// holds the proposal itself, so /yes and /no work regardless of what this
	// map remembers across a restart.
	awaiting map[int64]bool

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
		"commands": {`[{"command":"status","description":"Health of every system"},{"command":"yes","description":"Run what the assistant proposed"},{"command":"no","description":"Turn down what the assistant proposed"},{"command":"link","description":"Get a code to link this chat to HomeHub"},{"command":"unlink","description":"Disconnect this chat from HomeHub"},{"command":"help","description":"How the bridge works"}]`},
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
		// Linking is 1:1 with a single Telegram account, so it is only allowed
		// in a PRIVATE chat. A group binding would let every current or later-
		// added member act under the linking user's HomeHub identity and RBAC —
		// so the whole group is never bound.
		if msg.Chat.Type != "private" {
			b.send(ctx, token, chatID, "For security, linking only works in a private chat with the bot — a group binding would let every member act as you. Open a direct chat with me and send /link there.")
			return
		}
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

	// A linked chat must be private. Group chats can no longer be linked (see
	// /link above); this also neutralizes any binding created before that
	// restriction — it never runs /status or an assistant turn under the linked
	// user's identity for a group. Only /unlink is honored, so the group can
	// clean itself up; plain messages are ignored to avoid spamming the group.
	if msg.Chat.Type != "private" {
		switch {
		case strings.HasPrefix(text, "/unlink"), strings.HasPrefix(text, "!unlink"):
			_ = b.Store.DeleteTelegramChat(ctx, chatID)
			b.send(ctx, token, chatID, "This chat is now unlinked. HomeHub only works in a private chat with the bot — message me directly and send /link there.")
		case strings.HasPrefix(text, "/"), strings.HasPrefix(text, "!"):
			b.send(ctx, token, chatID, "For security, HomeHub only works in a private chat with the bot, not a group. Send /unlink to disconnect this group.")
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
		case "yes", "y", "approve":
			b.answerProposal(ctx, token, chatID, chat.UserID, true)
			return
		case "no", "n", "deny", "cancel":
			b.answerProposal(ctx, token, chatID, chat.UserID, false)
			return
		case "start", "help":
			b.send(ctx, token, chatID, "Linked to HomeHub. Plain messages go to the assistant, which answers here and can act on your homelab — including running commands on your machines over SSH.\n\nAnything that only reads runs straight away. Anything that changes a machine is proposed first and waits for you; commands that could destroy a system are refused outright, even if you ask.\n\nCommands:\n/status — health of every system\n/yes — run what was proposed\n/no — turn it down (I'll suggest something else)\n/unlink — disconnect this chat from HomeHub\n/help — show this")
			return
		default:
			b.send(ctx, token, chatID, fmt.Sprintf("Unknown command %q. Try /help. Plain messages talk to the assistant.", "/"+trigger))
			return
		}
	}

	// A bare "yes" while something is waiting means what it looks like.
	if b.isAwaiting(chatID) {
		if approve, ok := bareAnswer(text); ok {
			b.answerProposal(ctx, token, chatID, chat.UserID, approve)
			return
		}
	}

	// Assistant turn. Show "typing…" while the agent works.
	stop := b.typing(ctx, token, chatID)
	defer stop()
	turnCtx, cancel := context.WithTimeout(ctx, turnDeadline)
	defer cancel()
	reply, pending, err := b.Runner.RunAssistantTurnFor(turnCtx, chat.UserID, text)
	if err != nil {
		b.send(ctx, token, chatID, "The assistant could not answer: "+err.Error())
		return
	}
	b.deliverTurn(ctx, token, chatID, reply, pending)
}

// answerProposal handles /yes and /no. Both go back to the agent rather than
// being settled here: an approval so it can report what the command actually
// did, a refusal so it can say what else there is. Either way it may propose
// something new, which is why this ends the same way a plain turn does.
func (b *Bot) answerProposal(ctx context.Context, token string, chatID int64, userID uuid.UUID, approve bool) {
	stop := b.typing(ctx, token, chatID)
	defer stop()
	turnCtx, cancel := context.WithTimeout(ctx, turnDeadline)
	defer cancel()

	run := b.Runner.DeclinePendingFor
	if approve {
		run = b.Runner.ConfirmPendingFor
	}
	reply, pending, err := run(turnCtx, userID)
	if err != nil {
		b.setAwaiting(chatID, false)
		if errors.Is(err, ErrNoPending) {
			b.send(ctx, token, chatID, "Nothing is waiting for approval right now.")
			return
		}
		b.send(ctx, token, chatID, "That did not go through: "+err.Error())
		return
	}
	b.deliverTurn(ctx, token, chatID, reply, pending)
}

// deliverTurn sends one answer, and the proposal behind it when there is one.
// The proposal goes as its own plain-text message: a shell command is full of
// characters Markdown would mangle, and what it is about to do deserves to be
// the last thing on the screen rather than a line buried in prose.
func (b *Bot) deliverTurn(ctx context.Context, token string, chatID int64, reply string, pending []string) {
	if reply = strings.TrimSpace(reply); reply != "" {
		// Try Markdown first (the assistant answers in it); Telegram rejects
		// malformed markup with a 400, in which case plain text still lands.
		if err := b.sendParsed(ctx, token, chatID, clip(reply), "Markdown"); err != nil {
			b.send(ctx, token, chatID, clip(reply))
		}
	}
	b.setAwaiting(chatID, len(pending) > 0)
	if len(pending) == 0 {
		return
	}
	var msg strings.Builder
	msg.WriteString("Waiting on you:\n")
	for _, line := range pending {
		msg.WriteString("• " + line + "\n")
	}
	msg.WriteString("\n/yes to run it, /no to skip.")
	b.send(ctx, token, chatID, clip(msg.String()))
}

// bareAnswer reads a one-word reply as an answer to a waiting proposal. Only
// an exact match counts: "yes, and check the disk too" is a new instruction,
// not an approval, and has to reach the agent as one.
func bareAnswer(text string) (approve bool, ok bool) {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(text), " .!")) {
	case "yes", "y", "yep", "yeah", "ok", "okay", "do it", "go ahead", "ja":
		return true, true
	case "no", "n", "nope", "cancel", "stop", "nein":
		return false, true
	}
	return false, false
}

// setAwaiting records whether this chat is holding a proposal.
func (b *Bot) setAwaiting(chatID int64, waiting bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.awaiting == nil {
		b.awaiting = map[int64]bool{}
	}
	if waiting {
		b.awaiting[chatID] = true
	} else {
		delete(b.awaiting, chatID)
	}
}

func (b *Bot) isAwaiting(chatID int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.awaiting[chatID]
}

// clip keeps a message inside Telegram's size limit.
func clip(text string) string {
	if len(text) <= replyLimit {
		return text
	}
	return text[:replyLimit] + "…"
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

// Broadcast sends a message to every linked chat whose user passes the allow
// check. Used for pushed notifications (e.g. firing signals); a no-op while
// the bridge is disabled or unconfigured. Failures are logged per chat and
// never abort the rest.
// Broadcast sends one message to every linked chat the allow function
// accepts, and returns the users it actually reached — the caller records the
// message in each of their assistant threads so it can be discussed later.
func (b *Bot) Broadcast(ctx context.Context, text string, allow func(userID uuid.UUID) bool) []uuid.UUID {
	if b.EnabledFn != nil && !b.EnabledFn(ctx) {
		return nil
	}
	token := ""
	if b.TokenFn != nil {
		token = b.TokenFn(ctx)
	}
	if token == "" {
		return nil
	}
	chats, err := b.Store.ListTelegramChats(ctx)
	if err != nil {
		b.Logger.Warn("telegram broadcast: list chats", "error", err)
		return nil
	}
	var reached []uuid.UUID
	for _, chat := range chats {
		if allow != nil && !allow(chat.UserID) {
			continue
		}
		if err := b.sendParsed(ctx, token, chat.ChatID, text, ""); err != nil {
			b.Logger.Warn("telegram broadcast: send", "chat", chat.ChatID, "error", err)
			continue
		}
		reached = append(reached, chat.UserID)
	}
	return reached
}
