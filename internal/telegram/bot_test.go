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

// fakeRunner stands in for the API server. Each turn method reports what it
// was asked and answers with whatever the test set.
type fakeRunner struct {
	called  string
	reply   string
	pending []string
	err     error
}

func (f *fakeRunner) RunAssistantTurnFor(context.Context, uuid.UUID, string) (string, []string, error) {
	f.called = "turn"
	return f.reply, f.pending, f.err
}

func (f *fakeRunner) ConfirmPendingFor(context.Context, uuid.UUID) (string, []string, error) {
	f.called = "confirm"
	return f.reply, f.pending, f.err
}

func (f *fakeRunner) DeclinePendingFor(context.Context, uuid.UUID) (string, []string, error) {
	f.called = "decline"
	return f.reply, f.pending, f.err
}

func (f *fakeRunner) SystemStatusFor(context.Context, uuid.UUID) (string, error) { return "", nil }

// recorder captures every message the bot sends.
func recorder(b *Bot, sent *[]string) {
	b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		vals, _ := url.ParseQuery(string(body))
		if text := vals.Get("text"); text != "" {
			*sent = append(*sent, text)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)),
			Header:     make(http.Header),
		}, nil
	})
}

// A one-word reply answers a waiting proposal; anything longer is a fresh
// instruction. "yes, and restart it too" approving the old command — while the
// new half of the sentence is never heard — is the failure this guards.
func TestBareAnswerOnlyTakesExactWords(t *testing.T) {
	for _, word := range []string{"yes", "Yes", "y", "OK", "do it", "ja", " yes. "} {
		if approve, ok := bareAnswer(word); !ok || !approve {
			t.Errorf("%q should read as approval", word)
		}
	}
	for _, word := range []string{"no", "N", "nope", "cancel", "nein"} {
		if approve, ok := bareAnswer(word); !ok || approve {
			t.Errorf("%q should read as a refusal", word)
		}
	}
	for _, sentence := range []string{
		"yes, and restart nginx too",
		"no idea what that means",
		"yes please run it on the other host as well",
		"is the answer yes",
		"",
	} {
		if _, ok := bareAnswer(sentence); ok {
			t.Errorf("%q was taken as a bare answer; it is a new instruction", sentence)
		}
	}
}

// Approving and declining both go back to the agent — the refusal especially,
// because the person still has the problem they asked about.
func TestProposalAnswersReachTheAgent(t *testing.T) {
	for _, tc := range []struct {
		approve bool
		want    string
	}{{true, "confirm"}, {false, "decline"}} {
		runner := &fakeRunner{reply: "Done, Sir."}
		var sent []string
		b := &Bot{Runner: runner}
		recorder(b, &sent)
		b.setAwaiting(4242, true)

		b.answerProposal(context.Background(), "tok", 4242, uuid.New(), tc.approve)

		if runner.called != tc.want {
			t.Errorf("approve=%v called %q, want %q", tc.approve, runner.called, tc.want)
		}
		if len(sent) == 0 || !strings.Contains(sent[0], "Done, Sir.") {
			t.Errorf("approve=%v did not relay the agent's answer: %q", tc.approve, sent)
		}
		if b.isAwaiting(4242) {
			t.Errorf("approve=%v left the chat still waiting on an answered proposal", tc.approve)
		}
	}
}

// A /yes that finds nothing must say so. Silence would read as "it ran".
func TestApprovingNothingSaysSo(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{err: ErrNoPending}}
	recorder(b, &sent)
	b.setAwaiting(7, true)

	b.answerProposal(context.Background(), "tok", 7, uuid.New(), true)

	if len(sent) != 1 || !strings.Contains(strings.ToLower(sent[0]), "nothing is waiting") {
		t.Fatalf("reply = %q, want a plain 'nothing is waiting'", sent)
	}
	if b.isAwaiting(7) {
		t.Error("the chat is still marked as waiting after an empty approval")
	}
}

// The proposal arrives as its own message: a shell command survives it intact,
// and the two replies that answer it are the last thing on the screen.
func TestProposalIsSentSeparatelyAndNamesBothReplies(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{}}
	recorder(b, &sent)

	b.deliverTurn(context.Background(), "tok", 9, "I'd restart nginx to pick the config up.",
		[]string{"Run on WebHost LXC: systemctl restart nginx"})

	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want the answer and the proposal separately: %q", len(sent), sent)
	}
	proposal := sent[1]
	for _, want := range []string{"systemctl restart nginx", "/yes", "/no"} {
		if !strings.Contains(proposal, want) {
			t.Errorf("proposal message is missing %q:\n%s", want, proposal)
		}
	}
	if !b.isAwaiting(9) {
		t.Error("a delivered proposal did not mark the chat as waiting")
	}
}

// An answer with nothing queued must clear the flag, or a later bare "yes"
// would be swallowed instead of reaching the agent as a message.
func TestAnswerWithoutProposalStopsWaiting(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{}}
	recorder(b, &sent)
	b.setAwaiting(11, true)

	b.deliverTurn(context.Background(), "tok", 11, "Everything's quiet.", nil)

	if b.isAwaiting(11) {
		t.Error("still waiting after a turn that proposed nothing")
	}
}
