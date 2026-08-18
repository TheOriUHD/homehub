package telegram

import (
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
