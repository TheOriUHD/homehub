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
	called   string
	code     string
	reply    string
	proposal *Proposal
	err      error
}

func (f *fakeRunner) RunAssistantTurnFor(context.Context, uuid.UUID, string) (string, *Proposal, error) {
	f.called = "turn"
	return f.reply, f.proposal, f.err
}

func (f *fakeRunner) ConfirmPendingFor(_ context.Context, _ uuid.UUID, code string) (string, *Proposal, error) {
	f.called, f.code = "confirm", code
	return f.reply, f.proposal, f.err
}

func (f *fakeRunner) DeclinePendingFor(_ context.Context, _ uuid.UUID, code string) (string, *Proposal, error) {
	f.called, f.code = "decline", code
	return f.reply, f.proposal, f.err
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
// because the person still has the problem they asked about. A bare answer
// names the proposal the chat was last shown.
func TestProposalAnswersReachTheAgent(t *testing.T) {
	for _, tc := range []struct {
		approve bool
		want    string
	}{{true, "confirm"}, {false, "decline"}} {
		runner := &fakeRunner{reply: "Done, Sir."}
		var sent []string
		b := &Bot{Runner: runner}
		recorder(b, &sent)
		b.setAwaiting(4242, "K7Q2")

		b.answerProposal(context.Background(), "tok", 4242, uuid.New(), tc.approve, "")

		if runner.called != tc.want {
			t.Errorf("approve=%v called %q, want %q", tc.approve, runner.called, tc.want)
		}
		if runner.code != "K7Q2" {
			t.Errorf("approve=%v answered proposal %q, want the one shown (K7Q2)", tc.approve, runner.code)
		}
		if len(sent) == 0 || !strings.Contains(sent[0], "Done, Sir.") {
			t.Errorf("approve=%v did not relay the agent's answer: %q", tc.approve, sent)
		}
		if b.awaitingCode(4242) != "" {
			t.Errorf("approve=%v left the chat still waiting on an answered proposal", tc.approve)
		}
	}
}

// A code typed with the command wins over the remembered one: it is the
// person saying exactly which proposal they mean.
func TestExplicitCodeIsTheOneAnswered(t *testing.T) {
	runner := &fakeRunner{reply: "ok"}
	var sent []string
	b := &Bot{Runner: runner}
	recorder(b, &sent)
	b.setAwaiting(5, "AAAA")

	b.answerProposal(context.Background(), "tok", 5, uuid.New(), true, "k7q2")

	if runner.code != "K7Q2" {
		t.Fatalf("answered %q, want the typed code K7Q2", runner.code)
	}
	if commandArg("/yes K7Q2") != "K7Q2" || commandArg("/yes") != "" {
		t.Error("commandArg does not read the code after the command")
	}
}

// With no proposal on record (the bot restarted, or never showed one) a bare
// yes cannot say what it approves, so nothing is sent to HomeHub at all.
func TestBareYesWithoutAShownProposalAsksForTheCode(t *testing.T) {
	runner := &fakeRunner{}
	var sent []string
	b := &Bot{Runner: runner}
	recorder(b, &sent)

	b.answerProposal(context.Background(), "tok", 8, uuid.New(), true, "")

	if runner.called != "" {
		t.Fatalf("a bare yes with nothing shown reached HomeHub (%s)", runner.called)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "/yes K7Q2") {
		t.Fatalf("reply = %q, want a prompt for the code", sent)
	}
}

// A yes for a proposal that was replaced runs nothing, says so, and leaves
// the newer proposal — and the chat's memory of it — in place.
func TestApprovingAReplacedProposalSaysSo(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{err: ErrSuperseded}}
	recorder(b, &sent)
	b.setAwaiting(3, "NEWR")

	b.answerProposal(context.Background(), "tok", 3, uuid.New(), true, "OLDR")

	if len(sent) != 1 || !strings.Contains(sent[0], "replaced by a newer one") || !strings.Contains(sent[0], "OLDR") {
		t.Fatalf("reply = %q, want a plain 'replaced' notice naming the code", sent)
	}
	if b.awaitingCode(3) != "NEWR" {
		t.Error("a stale answer cleared the newer proposal from the chat")
	}
}

// A /yes that finds nothing must say so. Silence would read as "it ran".
func TestApprovingNothingSaysSo(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{err: ErrNoPending}}
	recorder(b, &sent)
	b.setAwaiting(7, "K7Q2")

	b.answerProposal(context.Background(), "tok", 7, uuid.New(), true, "")

	if len(sent) != 1 || !strings.Contains(strings.ToLower(sent[0]), "nothing is waiting") {
		t.Fatalf("reply = %q, want a plain 'nothing is waiting'", sent)
	}
	if b.awaitingCode(7) != "" {
		t.Error("the chat is still marked as waiting after an empty approval")
	}
}

// The proposal arrives as its own message: a shell command survives it intact,
// the code that names it is on it, and the two replies that answer it are the
// last thing on the screen.
func TestProposalIsSentSeparatelyAndNamesBothReplies(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{}}
	recorder(b, &sent)

	b.deliverTurn(context.Background(), "tok", 9, "I'd restart nginx to pick the config up.",
		&Proposal{Code: "K7Q2", Summaries: []string{"Run on WebHost LXC: systemctl restart nginx"}})

	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want the answer and the proposal separately: %q", len(sent), sent)
	}
	proposal := sent[1]
	for _, want := range []string{"systemctl restart nginx", "/yes", "/no", "K7Q2", "15 minutes"} {
		if !strings.Contains(proposal, want) {
			t.Errorf("proposal message is missing %q:\n%s", want, proposal)
		}
	}
	if b.awaitingCode(9) != "K7Q2" {
		t.Error("a delivered proposal did not mark the chat as waiting on its code")
	}
}

// An answer with nothing queued must clear the flag, or a later bare "yes"
// would be swallowed instead of reaching the agent as a message.
func TestAnswerWithoutProposalStopsWaiting(t *testing.T) {
	var sent []string
	b := &Bot{Runner: &fakeRunner{}}
	recorder(b, &sent)
	b.setAwaiting(11, "K7Q2")

	b.deliverTurn(context.Background(), "tok", 11, "Everything's quiet.", nil)

	if b.awaitingCode(11) != "" {
		t.Error("still waiting after a turn that proposed nothing")
	}
}

// Every message the bot sends turns link previews off: a preview is Telegram
// fetching the URL, which is a request nobody chose to make.
func TestMessagesDisableLinkPreviews(t *testing.T) {
	var form url.Values
	b := &Bot{}
	b.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		form, _ = url.ParseQuery(string(body))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
	})
	_ = b.sendParsed(context.Background(), "tok", 1, "see https://example.invalid/?q=secret", "Markdown")
	if got := form.Get("link_preview_options"); got != `{"is_disabled":true}` {
		t.Fatalf("link_preview_options = %q", got)
	}
}
