package telegram

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The security model: a chat's code is issued by the bot and only ever
// resolved (once) by a signed-in HomeHub user, who becomes the owner.
func TestClaimBindsCodeOnceToRequestingChat(t *testing.T) {
	b := &Bot{}
	const chatID = int64(4242)
	code := b.mintChatCode(chatID, "Test Chat")
	if len(code) != codeLength {
		t.Fatalf("code length = %d, want %d", len(code), codeLength)
	}

	user := uuid.New()
	// Wrong code never resolves.
	if _, _, err := b.Claim(user, "WRONGONE"); err == nil {
		t.Fatal("wrong code claimed successfully")
	}
	// Right code resolves to the requesting chat.
	got, label, err := b.Claim(user, code)
	if err != nil {
		t.Fatalf("valid claim failed: %v", err)
	}
	if got != chatID || label != "Test Chat" {
		t.Fatalf("claim returned (%d,%q), want (%d,%q)", got, label, chatID, "Test Chat")
	}
	// Single use: the same code cannot be replayed.
	if _, _, err := b.Claim(uuid.New(), code); err == nil {
		t.Fatal("code was replayable after a successful claim")
	}
}

// A single account cannot grind the code space: attempts are capped.
func TestClaimRateLimitedPerUser(t *testing.T) {
	b := &Bot{}
	user := uuid.New()
	for i := 0; i < maxClaimAttempts; i++ {
		if _, _, err := b.Claim(user, "NOPENOPE"); err == nil {
			t.Fatalf("attempt %d unexpectedly succeeded", i)
		}
	}
	// One over the cap: even a *correct* code is refused while locked out.
	code := b.mintChatCode(99, "x")
	_, _, err := b.Claim(user, code)
	if err == nil {
		t.Fatal("claim succeeded past the rate limit")
	}

	// A different user is unaffected by the first user's lockout.
	if _, _, err := b.Claim(uuid.New(), code); err != nil {
		t.Fatalf("second user was wrongly rate-limited: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A group chat can never mint a link code: binding a group would let every
// member act under the linking user's HomeHub identity (Finding 7).
func TestLinkRejectedInGroupChat(t *testing.T) {
	var sent string
	b := &Bot{}
	b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		sent = vals.Get("text")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)),
			Header:     make(http.Header),
		}, nil
	})

	// no Store: a group /link must return before any DB access
	for _, chatType := range []string{"group", "supergroup", "channel"} {
		var msg tgMessage
		msg.Text = "/link"
		msg.Chat.ID = 4242
		msg.Chat.Type = chatType
		msg.Chat.Title = "Family Group"
		b.handleMessage(context.Background(), "tok", &msg)

		if len(b.pending) != 0 {
			t.Fatalf("%s /link minted %d code(s); want 0", chatType, len(b.pending))
		}
		if !strings.Contains(strings.ToLower(sent), "private chat") {
			t.Fatalf("%s /link reply = %q, want a private-chat notice", chatType, sent)
		}
	}
}
