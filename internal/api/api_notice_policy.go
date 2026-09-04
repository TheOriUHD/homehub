package api

import (
	"context"
	"time"

	"homehub/internal/httpctx"
	"homehub/internal/notices"
	"homehub/internal/rbac"
)

// The owner's notification policy: what interrupts, what waits for the
// digest, when digests go out, when the house is quiet.

func toAPINoticePolicy(p notices.Policy) NoticePolicy {
	days := map[string][]string{}
	for k, v := range p.Digest.Days {
		days[k] = append([]string{}, v...)
	}
	return NoticePolicy{
		Immediate: struct {
			Critical  bool `json:"critical"`
			Info      bool `json:"info"`
			Recovered bool `json:"recovered"`
			Warning   bool `json:"warning"`
		}{Critical: p.Immediate["critical"], Info: p.Immediate["info"], Recovered: p.Immediate["recovered"], Warning: p.Immediate["warning"]},
		QuietHours: struct {
			CriticalDuring bool   `json:"criticalDuring"`
			Enabled        bool   `json:"enabled"`
			From           string `json:"from"`
			To             string `json:"to"`
		}{CriticalDuring: p.Quiet.CriticalDuring, Enabled: p.Quiet.Enabled, From: p.Quiet.From, To: p.Quiet.To},
		Digest: struct {
			Days     map[string][]string    `json:"days"`
			Mode     NoticePolicyDigestMode `json:"mode"`
			Weekdays []string               `json:"weekdays"`
			Weekends []string               `json:"weekends"`
		}{Days: days, Mode: NoticePolicyDigestMode(p.Digest.Mode), Weekdays: append([]string{}, p.Digest.Weekdays...), Weekends: append([]string{}, p.Digest.Weekends...)},
		Timezone: p.Timezone,
	}
}

func fromAPINoticePolicy(in NoticePolicy) notices.Policy {
	p := notices.Policy{
		Immediate: map[string]bool{"critical": true, "warning": in.Immediate.Warning, "info": in.Immediate.Info, "recovered": in.Immediate.Recovered},
		Quiet:     notices.Quiet{Enabled: in.QuietHours.Enabled, From: in.QuietHours.From, To: in.QuietHours.To, CriticalDuring: in.QuietHours.CriticalDuring},
		Digest:    notices.Digest{Mode: string(in.Digest.Mode), Weekdays: in.Digest.Weekdays, Weekends: in.Digest.Weekends, Days: in.Digest.Days},
		Timezone:  in.Timezone,
	}
	return p.Normalized()
}

func (s *Server) noticePolicyState(ctx context.Context, id *httpctx.Identity) NoticePolicyState {
	p := s.settings.NoticePolicy(ctx)
	now := time.Now()
	out := NoticePolicyState{
		Policy:          toAPINoticePolicy(p),
		QuietNow:        p.InQuietHours(now),
		CanConfigure:    s.can(ctx, id, ResSettings, rbac.LevelConfigure),
		TimezoneDefault: notices.HostTimezone(),
	}
	if n, err := s.store.CountQueuedNotices(ctx); err == nil {
		out.Queued = n
	}
	if next, ok := p.NextDigest(now); ok {
		out.NextDigestAt = &next
	}
	return out
}

func (s *Server) GetNoticePolicy(ctx context.Context, _ GetNoticePolicyRequestObject) (GetNoticePolicyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	return GetNoticePolicy200JSONResponse(s.noticePolicyState(ctx, id)), nil
}

func (s *Server) PutNoticePolicy(ctx context.Context, req PutNoticePolicyRequestObject) (PutNoticePolicyResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "notices.policy_update", ResSettings)
		return PutNoticePolicy403JSONResponse{forbidden()}, nil
	}
	p := fromAPINoticePolicy(*req.Body)
	if err := s.settings.SetNoticePolicy(ctx, p); err != nil {
		return PutNoticePolicy400JSONResponse{BadRequestJSONResponse(errBody("bad_policy", err.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "notices.policy_update", ResSettings, "success", map[string]any{
		"immediate": p.Immediate, "quiet": p.Quiet.Enabled, "mode": p.Digest.Mode, "timezone": p.Timezone,
	})
	return PutNoticePolicy200JSONResponse(s.noticePolicyState(ctx, id)), nil
}

func (s *Server) SendNoticeDigest(ctx context.Context, _ SendNoticeDigestRequestObject) (SendNoticeDigestResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResSettings, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "notices.digest_send", ResSettings)
		return SendNoticeDigest403JSONResponse{forbidden()}, nil
	}
	sent, n := s.SendDigestNow(ctx)
	s.audit.Log(ctx, actorFor(id), "notices.digest_send", ResSettings, "success", map[string]any{"sent": sent, "items": n})
	return SendNoticeDigest200JSONResponse{Sent: sent, Items: n}, nil
}
