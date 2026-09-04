package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/assistant"
	"github.com/TheOriUHD/homehub/internal/notices"
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

// flapWindow is how long after an all-clear a fault's return still counts as
// the same trouble. Inside it the return is judged against the fault that was
// reported — not against "recovered", which ranks below everything and made
// every return an "escalation" that skipped the rate floor — and each such
// return counts as a flap. A key that has flapped stops interrupting: its
// returns and all-clears are left for the digest.
const flapWindow = 6 * time.Hour

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

// gateDecision is what the deterministic gate makes of a transition before
// any policy or model is consulted.
type gateDecision int

const (
	gateDrop  gateDecision = iota // already told, or nothing to tell
	gateQueue                     // real, but not worth an interruption: the digest
	gateSend                      // may go on to the policy
)

// gateNotice decides whether a transition may go on to become a message right
// now, may wait for the digest, or is nothing new. It returns the reason for
// the log.
func gateNotice(ctx context.Context, st noticeStore, sig store.Signal, event string, now time.Time) (gateDecision, string) {
	reason := ""
	notice, _ := st.GetSignalNotice(ctx, sig.Key)
	flapping := notice != nil && notice.Flaps > 0 && notice.FlapWindowStart != nil && now.Sub(*notice.FlapWindowStart) < flapWindow
	if event == "recovered" {
		// Nothing to recover from, as far as the owner knows.
		if notice == nil || notice.LastSeverity == "recovered" {
			return gateDrop, "never reported"
		}
		if flapping {
			return gateQueue, "flapping — the all-clear is left for the digest"
		}
		if ok, err := st.ScheduleRecovery(ctx, sig.Key, sig.ID, now.Add(recoveryHold)); err != nil || !ok {
			return gateDrop, "could not park the recovery"
		}
		return gateDrop, "parked until the recovery has held"
	}
	// The fault is back. If its all-clear was still waiting, the owner never
	// heard it — from their side this is the same outage continuing, and the
	// repeat rules below apply as if nothing had cleared.
	if cancelled, _ := st.CancelRecovery(ctx, sig.Key); cancelled {
		reason = "recovery withdrawn — the fault returned inside the hold; "
	}
	if notice == nil {
		return gateSend, ""
	}
	// Judge against the last fault reported while it is still recent trouble;
	// only after a quiet stretch longer than the window is a return news.
	reported := notice.LastSeverity
	recentFault := notice.LastFaultAt != nil && now.Sub(*notice.LastFaultAt) < flapWindow
	if recentFault && notice.LastFaultSeverity != "" {
		reported = notice.LastFaultSeverity
	}
	worse := severityRank(sig.Severity) > severityRank(reported)
	if flapping && !worse {
		return gateQueue, reason + fmt.Sprintf("flapping — %d returns inside %s, left for the digest", notice.Flaps, flapWindow)
	}
	since := now.Sub(notice.LastSentAt)
	if notice.LastSeverity == "recovered" && recentFault && !worse {
		return gateQueue, reason + fmt.Sprintf("back %s after its all-clear, no worse than reported — left for the digest", since.Truncate(time.Second))
	}
	if since < minNoticeInterval && !worse {
		return gateDrop, reason + fmt.Sprintf("repeat %s after the last message", since.Truncate(time.Second))
	}
	return gateSend, ""
}

// HandleSignalEvent is the signals service's notifier. Runs detached so a
// slow Telegram API or model never stalls a collector sweep.
func (s *Server) HandleSignalEvent(sig store.Signal, event string) {
	go func() {
		// The narrator gets its own budget: it is one small model call, and
		// a slow provider must not hold a notification hostage.
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		now := time.Now()
		// An all-clear for something waiting in the digest is answered there:
		// the item turns into "came and went" rather than a second line.
		if event == "recovered" {
			if resolved, _ := s.store.ResolveQueuedNotice(ctx, sig.Key); resolved {
				s.logger.Info("notification queued", "signal", sig.Key, "event", event, "why", "cleared while waiting for the digest")
			}
		}
		decision, reason := gateNotice(ctx, s.store, sig, event, now)
		switch decision {
		case gateDrop:
			s.logger.Info("notification gated", "signal", sig.Key, "event", event, "why", reason)
			return
		case gateQueue:
			s.logger.Info("notification queued", "signal", sig.Key, "event", event, "why", reason)
			s.enqueue(ctx, sig, event)
			return
		}
		s.route(ctx, sig, event, now)
	}()
}

// route sends a transition the gate let through where the owner's policy
// says: to the phone now, or into the digest.
func (s *Server) route(ctx context.Context, sig store.Signal, event string, now time.Time) {
	policy := s.settings.NoticePolicy(ctx)
	severity := sig.Severity
	if event == "recovered" {
		severity = "recovered"
	}
	if policy.RouteFor(severity, now) == notices.RouteImmediate {
		s.narrateAndDeliver(ctx, sig, event)
		return
	}
	why := "the policy sends " + severity + " with the digest"
	if policy.InQuietHours(now) {
		why = "quiet hours"
	}
	s.logger.Info("notification queued", "signal", sig.Key, "event", event, "why", why)
	s.enqueue(ctx, sig, event)
}

// enqueue puts a transition into the digest. An all-clear with nothing
// waiting for its key still earns a line — "it went down and came back" is
// news the owner never heard — recorded as already cleared.
func (s *Server) enqueue(ctx context.Context, sig store.Signal, event string) {
	item := store.QueuedNotice{
		Key: sig.Key, SignalID: &sig.ID, Event: event, Severity: sig.Severity, Title: sig.Title, Body: sig.Body,
		SourceName: sig.SourceName, SourceType: sig.SourceType, Detail: sig.Detail,
	}
	if event == "recovered" {
		if resolved, _ := s.store.ResolveQueuedNotice(ctx, sig.Key); resolved {
			return
		}
		item.Severity = "recovered"
	}
	if _, err := s.store.EnqueueNotice(ctx, item); err != nil {
		s.logger.Warn("could not queue a notification", "signal", sig.Key, "error", err)
		return
	}
	if event == "recovered" {
		_, _ = s.store.ResolveQueuedNotice(ctx, sig.Key)
	}
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
		s.route(ctx, *sig, "recovered", time.Now())
	}
}

// FlushDigest sends the digest when the schedule says so. Wired to the
// once-a-minute signals sweep beside FlushRecoveries. The last check lives in
// memory between ticks; the last digest actually sent is persisted, so after
// a restart a slot that fell inside the gap is still honoured — bounded to
// the last hour and a half, so an old timestamp cannot trigger a stale one.
func (s *Server) FlushDigest(ctx context.Context) {
	now := time.Now()
	s.digestMu.Lock()
	last := s.digestLastCheck
	if last.IsZero() {
		last = s.settings.NoticeDigestLast(ctx)
		if floor := now.Add(-90 * time.Minute); last.Before(floor) {
			last = floor
		}
	}
	s.digestLastCheck = now
	s.digestMu.Unlock()
	if !s.settings.NoticePolicy(ctx).DigestDue(last, now) {
		return
	}
	if sent, n := s.sendDigest(ctx, "digest"); sent {
		s.logger.Info("digest sent", "items", n)
	}
}

// SendDigestNow flushes the queue regardless of the schedule. Reports
// whether anything went out and how many items it carried.
func (s *Server) SendDigestNow(ctx context.Context) (bool, int) {
	return s.sendDigest(ctx, "manual")
}

// sendDigest turns everything waiting into one message.
func (s *Server) sendDigest(ctx context.Context, kind string) (bool, int) {
	items, err := s.store.ListQueuedNotices(ctx)
	if err != nil || len(items) == 0 {
		return false, 0
	}
	in := assistant.DigestInput{Now: time.Now()}
	ids := make([]int64, 0, len(items))
	for _, q := range items {
		ids = append(ids, q.ID)
		in.Items = append(in.Items, queuedLine(q))
	}
	text := ""
	if provider, model := s.noticeProvider(ctx); provider != nil {
		if t, err := assistant.WriteDigest(ctx, provider, model, in); err == nil {
			text = t
		} else {
			s.logger.Warn("digest narrator failed, sending the plain text", "error", err)
		}
	}
	if text == "" {
		text = assistant.TemplateDigest(in)
	}
	digest := store.Signal{Key: "digest", Severity: "info", Title: fmt.Sprintf("Digest of %d conditions", len(items)), SourceName: "HomeHub"}
	s.deliverNotice(ctx, digest, "digest", text)
	if err := s.store.FlushQueuedNotices(ctx, ids, kind); err != nil {
		s.logger.Warn("could not mark the digest as sent", "error", err)
	}
	_ = s.settings.SetNoticeDigestLast(ctx, time.Now())
	return true, len(items)
}

func queuedLine(q store.QueuedNotice) assistant.QueuedLine {
	return assistant.QueuedLine{Severity: q.Severity, Title: q.Title, SourceName: q.SourceName,
		QueuedAt: q.QueuedAt, Resolved: q.ResolvedAt != nil, Bounces: q.Bounces}
}

// narrateAndDeliver writes the message — by the model, or the template when
// it cannot — and sends it. The gate has already been passed.
func (s *Server) narrateAndDeliver(ctx context.Context, sig store.Signal, event string) {
	// Whatever was waiting for the digest rides along: an interruption is
	// the moment to mention the lesser things too, after the main news.
	var queued []store.QueuedNotice
	if items, err := s.store.ListQueuedNotices(ctx); err == nil {
		for _, q := range items {
			if q.Key != sig.Key {
				queued = append(queued, q)
			}
		}
	}
	text, decided := s.narrateSignal(ctx, sig, event, queued)
	if decided && text == "" {
		return // held back deliberately
	}
	if !decided {
		text = templateSignalText(sig, event)
		if len(queued) > 0 {
			in := assistant.DigestInput{Now: time.Now()}
			for _, q := range queued {
				in.Items = append(in.Items, queuedLine(q))
			}
			text += "\n\nAlso waiting:\n" + assistant.TemplateDigest(in)
		}
	}
	s.deliverNotice(ctx, sig, event, text)
	if len(queued) > 0 {
		ids := make([]int64, 0, len(queued))
		for _, q := range queued {
			ids = append(ids, q.ID)
		}
		_ = s.store.FlushQueuedNotices(ctx, ids, "immediate")
	}
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
	_ = s.store.RecordSignalNotice(ctx, sig.Key, severity, flapWindow)
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
func (s *Server) narrateSignal(ctx context.Context, sig store.Signal, event string, queued []store.QueuedNotice) (string, bool) {
	provider, model := s.noticeProvider(ctx)
	if provider == nil {
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
		if notice.Flaps > 0 && notice.FlapWindowStart != nil && time.Since(*notice.FlapWindowStart) < flapWindow {
			in.Flaps = notice.Flaps
			in.FlapWindowStart = notice.FlapWindowStart
		}
	}
	if recent, rerr := s.store.ListSentNotices(ctx, time.Now().Add(-recentWindow), "", 8); rerr == nil {
		for _, r := range recent {
			if r.Key == sig.Key {
				continue
			}
			in.RecentlySent = append(in.RecentlySent, assistant.SentLine{At: r.SentAt, Text: r.Text})
		}
	}

	for _, q := range queued {
		in.Queued = append(in.Queued, queuedLine(q))
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

// noticeProvider is the small model that writes notifications: the
// configured notify model, else the provider's basic lane. Nil when no model
// can answer, and the caller uses the templated text.
func (s *Server) noticeProvider(ctx context.Context) (assistant.Provider, string) {
	enabled, providerName, _, baseURL := s.settings.Assistant(ctx)
	if !enabled {
		return nil, ""
	}
	model := s.settings.AssistantNotifyModel(ctx)
	if model == "" {
		basic, advanced := s.settings.AssistantAutoLanes(ctx)
		model = assistant.LaneModel(providerName, assistant.LaneBasic, basic, advanced)
	}
	if model == "" {
		return nil, ""
	}
	apiKey, _ := s.assistantKeyFor(ctx, providerName)
	provider, err := assistant.NewProviderFor(assistant.Config{Provider: providerName, Model: model, APIKey: apiKey, BaseURL: baseURL})
	if err != nil {
		return nil, ""
	}
	return provider, model
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

// recentNoticeLines is what the Telegram assistant is told it sent lately,
// in the system context of every turn: the thread keeps only the newest few
// alerts in view, and "that printer alert" from this morning must still mean
// something without a tool call.
func (s *Server) recentNoticeLines(ctx context.Context) []string {
	recent, err := s.store.ListSentNotices(ctx, time.Now().Add(-24*time.Hour), "", 12)
	if err != nil || len(recent) == 0 {
		return nil
	}
	lines := []string{"Alerts you pushed to this person in the last 24 hours, newest first (call sent_notifications for each one's standing now):"}
	for _, r := range recent {
		text := strings.TrimSpace(r.Text)
		if len(text) > 140 {
			text = text[:137] + "…"
		}
		lines = append(lines, "- "+r.SentAt.Local().Format("Mon 15:04")+" — "+text)
	}
	return lines
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
