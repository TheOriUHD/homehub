package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/assistant"
	"github.com/TheOriUHD/homehub/internal/store"
)

// The path from a signal transition to a message on the owner's phone.
//
// Every transition passes a deterministic gate before any model is asked:
// repeats inside the rate floor are dropped, and an all-clear is parked for
// recoveryHold rather than announced — the fault returning inside the hold
// cancels it, so the owner never reads "it's back" followed two minutes later
// by "it's down again". Only after the gate does the narrator get to write,
// and the templated text stands in when it cannot.

// minNoticeInterval is the rate floor for repeats of one condition. The
// narrator's judgement is not a rate limiter: anything that flaps — a bouncing
// connection, a sensor on a threshold — could otherwise produce a message
// every sweep. This cap is deterministic and runs before the model is asked.
const minNoticeInterval = 30 * time.Minute

// recoveryHold is how long a condition must stay clear before its all-clear
// is announced. Collectors keep their own dwell for the signal itself, but
// that state lives in memory and a restart forgets it — which is exactly
// when a flapping fault looks healed. The hold here is persisted.
const recoveryHold = 10 * time.Minute

// recentWindow is how far back the narrator is shown what else went out, so
// one fault reported under two names (a network degraded because one of its
// servers is offline, which was reported on its own) is not said twice.
const recentWindow = 30 * time.Minute

// noticeStore is the slice of the store the gate needs; an interface so the
// decision can be tested without a database.
type noticeStore interface {
	GetSignalNotice(ctx context.Context, key string) (*store.SignalNotice, error)
	ScheduleRecovery(ctx context.Context, key string, signalID int64, due time.Time) (bool, error)
	CancelRecovery(ctx context.Context, key string) (bool, error)
}

// gateNotice decides whether a transition may go on to become a message right
// now. It returns the reason when it may not, for the log.
func gateNotice(ctx context.Context, st noticeStore, sig store.Signal, event string, now time.Time) (proceed bool, reason string) {
	notice, _ := st.GetSignalNotice(ctx, sig.Key)
	if event == "recovered" {
		// Nothing to recover from, as far as the owner knows.
		if notice == nil || notice.LastSeverity == "recovered" {
			return false, "never reported"
		}
		if ok, err := st.ScheduleRecovery(ctx, sig.Key, sig.ID, now.Add(recoveryHold)); err != nil || !ok {
			return false, "could not park the recovery"
		}
		return false, "parked until the recovery has held"
	}
	// The fault is back. If its all-clear was still waiting, the owner never
	// heard it — from their side this is the same outage continuing, and the
	// repeat rules below apply as if nothing had cleared.
	if cancelled, _ := st.CancelRecovery(ctx, sig.Key); cancelled {
		reason = "recovery withdrawn — the fault returned inside the hold; "
	}
	if notice == nil {
		return true, ""
	}
	since := now.Sub(notice.LastSentAt)
	worse := severityRank(sig.Severity) > severityRank(notice.LastSeverity)
	if since < minNoticeInterval && !worse {
		return false, reason + fmt.Sprintf("repeat %s after the last message", since.Truncate(time.Second))
	}
	return true, ""
}

// HandleSignalEvent is the signals service's notifier. Runs detached so a
// slow Telegram API or model never stalls a collector sweep.
func (s *Server) HandleSignalEvent(sig store.Signal, event string) {
	go func() {
		// The narrator gets its own budget: it is one small model call, and
		// a slow provider must not hold a notification hostage.
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		proceed, reason := gateNotice(ctx, s.store, sig, event, time.Now())
		if !proceed {
			s.logger.Info("notification gated", "signal", sig.Key, "event", event, "why", reason)
			return
		}
		s.narrateAndDeliver(ctx, sig, event)
	}()
}

// FlushRecoveries announces the all-clears whose hold is over. Wired to the
// once-a-minute signals sweep.
func (s *Server) FlushRecoveries(ctx context.Context) {
	due, err := s.store.DueRecoveries(ctx)
	if err != nil {
		s.logger.Warn("reading due recoveries", "error", err)
		return
	}
	for _, notice := range due {
		// The fault came back and cleared again inside the hold, or is live
		// right now: either way "it's back" would be false.
		if live, err := s.store.HasLiveSignal(ctx, notice.Key); err != nil || live {
			s.logger.Info("recovery dropped at flush — the condition is live again", "signal", notice.Key)
			continue
		}
		if notice.RecoverySignalID == nil {
			continue
		}
		sig, err := s.store.GetSignalByID(ctx, *notice.RecoverySignalID)
		if err != nil || sig == nil {
			continue
		}
		s.narrateAndDeliver(ctx, *sig, "recovered")
	}
}

// narrateAndDeliver writes the message — by the model, or the template when
// it cannot — and sends it. The gate has already been passed.
func (s *Server) narrateAndDeliver(ctx context.Context, sig store.Signal, event string) {
	text, decided := s.narrateSignal(ctx, sig, event)
	if decided && text == "" {
		return // held back deliberately
	}
	if !decided {
		text = templateSignalText(sig, event)
	}
	s.deliverNotice(ctx, sig, event, text)
}

// deliverNotice broadcasts to every linked chat whose user has the signals
// surface visible, appends the message to each recipient's Telegram thread,
// and records what was said.
func (s *Server) deliverNotice(ctx context.Context, sig store.Signal, event, text string) {
	if s.telegram == nil {
		return
	}
	reached := s.telegram.Broadcast(ctx, text, func(userID uuid.UUID) bool {
		hidden, err := s.store.SignalsHiddenForUser(ctx, userID)
		return err == nil && !hidden
	})
	for _, userID := range reached {
		s.rememberSentNotice(ctx, userID, sig, event, text)
	}
	severity := sig.Severity
	if event == "recovered" {
		severity = "recovered"
	}
	// Recoveries are recorded rather than clearing the record. Clearing was
	// the flaw behind a day of alternating "it is down" / "it is back"
	// messages: every all-clear reset the history, so the next firing looked
	// like a first report. The record ages out on its own instead.
	_ = s.store.RecordSignalNotice(ctx, sig.Key, severity)
	if err := s.store.LogSentNotice(ctx, store.SentNotice{
		Key: sig.Key, Event: event, Severity: severity, Title: sig.Title,
		SourceName: sig.SourceName, Text: text,
	}); err != nil {
		s.logger.Warn("could not log a sent notification", "signal", sig.Key, "error", err)
	}
}

// narrateSignal asks the small model to write the message. It reports (text,
// decided): decided is false when no model could answer, and the caller falls
// back to the templated text; an empty text with decided true means the
// narrator judged this repeat not worth an interruption.
//
// Deliberately never fails loudly: a notification that reads awkwardly beats
// one that never arrives.
func (s *Server) narrateSignal(ctx context.Context, sig store.Signal, event string) (string, bool) {
	enabled, providerName, _, baseURL := s.settings.Assistant(ctx)
	if !enabled {
		return "", false
	}
	model := s.settings.AssistantNotifyModel(ctx)
	if model == "" {
		// Default to the provider's small lane rather than whatever the chat
		// is pinned to — this runs unattended, once per condition.
		basic, advanced := s.settings.AssistantAutoLanes(ctx)
		model = assistant.LaneModel(providerName, assistant.LaneBasic, basic, advanced)
	}
	if model == "" {
		return "", false
	}
	apiKey, _ := s.assistantKeyFor(ctx, providerName)
	provider, err := assistant.NewProviderFor(assistant.Config{
		Provider: providerName, Model: model, APIKey: apiKey, BaseURL: baseURL,
	})
	if err != nil {
		return "", false
	}

	in := assistant.NoticeInput{
		Event: event, Severity: sig.Severity, Title: sig.Title, Body: sig.Body,
		SourceName: sig.SourceName, SourceType: sig.SourceType,
		Count: sig.Count, FirstSeen: sig.FirstSeen, LastSeen: sig.LastSeen, ResolvedAt: sig.ResolvedAt,
	}
	if notice, nerr := s.store.GetSignalNotice(ctx, sig.Key); nerr == nil && notice != nil {
		in.TimesNotified = notice.SentCount
		in.LastNotified = &notice.LastSentAt
		in.LastSeverity = notice.LastSeverity
	}
	if recent, rerr := s.store.ListSentNotices(ctx, time.Now().Add(-recentWindow), "", 8); rerr == nil {
		for _, r := range recent {
			if r.Key == sig.Key {
				continue
			}
			in.RecentlySent = append(in.RecentlySent, assistant.SentLine{At: r.SentAt, Text: r.Text})
		}
	}

	notice, err := assistant.WriteNotice(ctx, provider, model, in)
	if err != nil {
		s.logger.Warn("notification narrator failed, sending the plain text",
			"signal", sig.Key, "error", err)
		return "", false
	}
	if !notice.Send {
		s.logger.Info("notification held back", "signal", sig.Key, "why", notice.Why)
		return "", true
	}
	return notice.Text, true
}

// severityRank orders severities so a genuine escalation can pass the rate
// limit that an unchanged repeat cannot.
func severityRank(sev string) int {
	switch sev {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// templateSignalText renders one signal transition as a plain message — the
// fallback when no model can write it.
func templateSignalText(sig store.Signal, event string) string {
	icon := "⚠️"
	switch {
	case event == "recovered":
		icon = "✅"
	case sig.Severity == "critical":
		icon = "🚨"
	}
	head := sig.Title
	if event == "recovered" {
		head = "Recovered: " + sig.Title
	}
	text := icon + " " + head
	if sig.SourceName != "" {
		text += "\n" + sig.SourceName
	}
	if sig.Body != "" && event != "recovered" {
		text += "\n" + sig.Body
	}
	return text
}

// rememberSentNotice appends a delivered notification to a user's Telegram
// assistant thread. Without this the bot has no idea what "that alert" refers
// to when they reply to one — the message went out over a different path than
// the conversation, and left no trace in it. The marker in tool_trace is what
// lets the thread show the message as an alert with the time it went out,
// rather than as something the assistant said in reply.
func (s *Server) rememberSentNotice(ctx context.Context, userID uuid.UUID, sig store.Signal, event, text string) {
	history, err := s.store.ListAssistantMessages(ctx, userID, store.AssistantChannelTelegram, "", 1)
	if err != nil {
		return
	}
	var parent *int64
	if len(history) > 0 {
		parent = &history[len(history)-1].ID
	}
	trace, _ := json.Marshal(assistant.NoticeMarker{Key: sig.Key, Event: event, Severity: sig.Severity, Title: sig.Title})
	if _, err := s.store.AddAssistantMessage(ctx, userID, store.AssistantChannelTelegram, "",
		"assistant", text, trace, parent, nil); err != nil {
		s.logger.Warn("could not record a sent notification", "user", userID, "error", err)
	}
}
