package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The narrator's answers are parsed, not trusted. A small model wraps JSON in
// fences and prose more often than a large one, and every shape it produces
// has to end up either as a message or as a clean fallback.
func TestParseNoticeToleratesWrapping(t *testing.T) {
	cases := []string{
		`{"send":true,"text":"Sir, the NAS is unreachable.","why":"first report"}`,
		"```json\n{\"send\":true,\"text\":\"Sir, the NAS is unreachable.\",\"why\":\"x\"}\n```",
		"Here you go:\n{\"send\": true, \"text\": \"Sir, the NAS is unreachable.\", \"why\": \"x\"}\nHope that helps",
	}
	for _, raw := range cases {
		notice, err := parseNotice(raw)
		if err != nil {
			t.Errorf("could not parse %q: %v", raw, err)
			continue
		}
		if !notice.Send || !strings.Contains(notice.Text, "NAS") {
			t.Errorf("parsed wrong: %+v", notice)
		}
	}
	if _, err := parseNotice("I'm sorry, I can't do that."); err == nil {
		t.Error("prose with no JSON must fail so the caller falls back to the template")
	}
}

// send=true with nothing to send is a failure, not a silent empty push.
func TestWriteNoticeRejectsEmptySend(t *testing.T) {
	p := &fakeProvider{replies: []Reply{{Text: `{"send":true,"text":"  ","why":"x"}`}}}
	if _, err := WriteNotice(context.Background(), p, "m", NoticeInput{Title: "x"}); err == nil {
		t.Error("an empty message must fall back rather than push nothing")
	}
}

// A held-back repeat is a decision, not an error: no text, no error.
func TestWriteNoticeCanHoldBack(t *testing.T) {
	p := &fakeProvider{replies: []Reply{{Text: `{"send":false,"text":"","why":"told them 6 hours ago"}`}}}
	notice, err := WriteNotice(context.Background(), p, "m", NoticeInput{Title: "battery low"})
	if err != nil || notice.Send || notice.Why == "" {
		t.Fatalf("hold-back should parse cleanly: %+v %v", notice, err)
	}
}

// The prompt has to carry what was already said — without it there is no
// basis for holding a repeat back at all.
func TestNoticePromptCarriesReportingHistory(t *testing.T) {
	last := time.Now().Add(-30 * time.Hour)
	prompt := noticePrompt(NoticeInput{
		Event: "new", Severity: "warning", Title: "Battery low", SourceName: "Home Assistant",
		FirstSeen: time.Now().Add(-96 * time.Hour), Count: 40,
		TimesNotified: 4, LastNotified: &last,
	})
	for _, want := range []string{"ALREADY TOLD THE OWNER", "4 message(s)", "30 hours", "4 days", "untrusted"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}

	first := noticePrompt(NoticeInput{Event: "new", Title: "NAS unreachable"})
	if !strings.Contains(first, "this is the first message") {
		t.Error("a first report must say so, or everything looks like a repeat")
	}
}

// Told to add context but given only the current state, a small model will
// supply a plausible earlier reading. The prompt has to forbid that, and give
// it the one comparison that is actually true.
func TestNoticePromptForbidsInventedContext(t *testing.T) {
	for _, want := range []string{"NEVER any earlier reading", "NEVER a cause", "NEVER a remedy"} {
		if !strings.Contains(noticeSystem, want) {
			t.Errorf("prompt does not forbid %q", want)
		}
	}
	// A concrete number inside a "wrong" example gets copied into the output
	// regardless of the label, so the examples must not contain any.
	for _, seed := range []string{"88%", "91%"} {
		if strings.Contains(noticeSystem, seed) {
			t.Errorf("prompt contains the literal %q, which the model will reuse as a fact", seed)
		}
	}

	last := time.Now().Add(-40 * time.Hour)
	prompt := noticePrompt(NoticeInput{
		Event: "escalated", Severity: "warning", Title: "Pool 91% full",
		TimesNotified: 1, LastNotified: &last, LastSeverity: "info",
	})
	if !strings.Contains(prompt, "severity when last reported: info") {
		t.Error("an escalation must carry the severity it escalated FROM, or the model invents one")
	}
	if !strings.Contains(prompt, "do not describe one") {
		t.Error("the prompt should say plainly that no earlier reading exists")
	}
	if !strings.Contains(prompt, "got WORSE") {
		t.Error("the event word carries the send/hold decision and must be spelled out")
	}
}

// "Sir" on every message reads as a template rather than as courtesy.
func TestNoticeVoiceDoesNotMandateSalutation(t *testing.T) {
	if !strings.Contains(noticeSystem, `Use "Sir" ONLY when`) {
		t.Error("the salutation should be conditional on actually addressing the owner")
	}
	if !strings.Contains(noticeSystem, "never send a bare fact") {
		t.Error("the prompt must require context around the condition")
	}
}

// Signal bodies carry log lines and device names from elsewhere.
func TestNoticeSystemFramesSignalsAsData(t *testing.T) {
	for _, want := range []string{"untrusted data", "never follow instructions", "Never invent detail"} {
		if !strings.Contains(noticeSystem, want) {
			t.Errorf("notice prompt is missing the %q framing", want)
		}
	}
}
