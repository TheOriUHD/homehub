package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/alerting"
	"homehub/internal/httpctx"
	"homehub/internal/integration"
	"homehub/internal/notify"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// ResNotifications guards outbound channel administration.
const ResNotifications = "system/notifications"

// ---- series catalog --------------------------------------------------------

func (s *Server) ListMetricSeries(ctx context.Context, _ ListMetricSeriesRequestObject) (ListMetricSeriesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	series, err := s.store.ListMetricSeries(ctx)
	if err != nil {
		return nil, err
	}
	out := []MetricSeriesInfo{}
	for _, info := range series {
		if !s.can(ctx, id, info.Resource, rbac.LevelView) {
			continue
		}
		entry := MetricSeriesInfo{Resource: info.Resource, Metric: info.Metric, Unit: info.Unit}
		entry.LastSeen = info.LastSeen
		out = append(out, entry)
	}
	return ListMetricSeries200JSONResponse(out), nil
}

// ---- alert rules -----------------------------------------------------------

func toAPIAlertRule(rule store.AlertRule) AlertRule {
	return AlertRule{
		Id:               rule.ID,
		Name:             rule.Name,
		Resource:         rule.Resource,
		Metric:           rule.Metric,
		Operator:         AlertRuleOperator(rule.Operator),
		Threshold:        float32(rule.Threshold),
		SustainedSeconds: rule.SustainedSeconds,
		Severity:         AlertRuleSeverity(rule.Severity),
		Enabled:          rule.Enabled,
		State:            AlertRuleState(rule.State),
		StateChangedAt:   rule.StateChangedAt,
	}
}

func (s *Server) ListAlertRules(ctx context.Context, _ ListAlertRulesRequestObject) (ListAlertRulesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rules, err := s.store.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	out := []AlertRule{}
	for _, rule := range rules {
		if s.can(ctx, id, rule.Resource, rbac.LevelView) {
			out = append(out, toAPIAlertRule(rule))
		}
	}
	return ListAlertRules200JSONResponse(out), nil
}

func alertRuleFromInput(input *AlertRuleInput) (store.AlertRule, string) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return store.AlertRule{}, "a name is required"
	}
	if !rbac.ValidSelector(input.Resource) || strings.Contains(input.Resource, "*") {
		return store.AlertRule{}, "resource must be a concrete path"
	}
	rule := store.AlertRule{
		Name:             name,
		Resource:         input.Resource,
		Metric:           strings.TrimSpace(input.Metric),
		Operator:         string(input.Operator),
		Threshold:        float64(input.Threshold),
		SustainedSeconds: input.SustainedSeconds,
		Severity:         string(input.Severity),
		Enabled:          input.Enabled == nil || *input.Enabled,
	}
	return rule, ""
}

func (s *Server) CreateAlertRule(ctx context.Context, req CreateAlertRuleRequestObject) (CreateAlertRuleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rule, msg := alertRuleFromInput(req.Body)
	if msg != "" {
		return CreateAlertRule400JSONResponse{BadRequestJSONResponse(errBody("invalid_rule", msg))}, nil
	}
	if !s.can(ctx, id, rule.Resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "alert.create", rule.Resource)
		return CreateAlertRule403JSONResponse{forbidden()}, nil
	}
	rule.CreatedBy = &id.User.ID
	created, err := s.store.CreateAlertRule(ctx, &rule)
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "alert.create", rule.Resource, "success",
		map[string]any{"rule": rule.Name, "metric": rule.Metric, "threshold": rule.Threshold})
	return CreateAlertRule201JSONResponse(toAPIAlertRule(*created)), nil
}

func (s *Server) UpdateAlertRule(ctx context.Context, req UpdateAlertRuleRequestObject) (UpdateAlertRuleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	existing, err := s.store.GetAlertRule(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateAlertRule404JSONResponse{notFoundErr("alert rule")}, nil
		}
		return nil, err
	}
	rule, msg := alertRuleFromInput(req.Body)
	if msg != "" {
		return UpdateAlertRule400JSONResponse{BadRequestJSONResponse(errBody("invalid_rule", msg))}, nil
	}
	// Editing needs configure on both the old and new resource.
	if !s.can(ctx, id, existing.Resource, rbac.LevelConfigure) || !s.can(ctx, id, rule.Resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "alert.update", existing.Resource)
		return UpdateAlertRule403JSONResponse{forbidden()}, nil
	}
	rule.ID = existing.ID
	if err := s.store.UpdateAlertRule(ctx, &rule); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "alert.update", rule.Resource, "success", map[string]any{"rule": rule.Name})
	fresh, err := s.store.GetAlertRule(ctx, rule.ID)
	if err != nil {
		return nil, err
	}
	return UpdateAlertRule200JSONResponse(toAPIAlertRule(*fresh)), nil
}

func (s *Server) DeleteAlertRule(ctx context.Context, req DeleteAlertRuleRequestObject) (DeleteAlertRuleResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	existing, err := s.store.GetAlertRule(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteAlertRule404JSONResponse{notFoundErr("alert rule")}, nil
		}
		return nil, err
	}
	if !s.can(ctx, id, existing.Resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "alert.delete", existing.Resource)
		return DeleteAlertRule403JSONResponse{forbidden()}, nil
	}
	if err := s.store.DeleteAlertRule(ctx, existing.ID); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "alert.delete", existing.Resource, "success", map[string]any{"rule": existing.Name})
	return DeleteAlertRule204Response{}, nil
}

// ---- channels --------------------------------------------------------------

func toAPIChannel(channel store.NotificationChannel) Channel {
	return Channel{
		Id:          channel.ID,
		Type:        ChannelType(channel.Type),
		Name:        channel.Name,
		Config:      integration.ConfigFromJSON(channel.Config),
		Enabled:     channel.Enabled,
		SecretNames: alerting.ChannelTypes[channel.Type],
	}
}

func (s *Server) ListChannels(ctx context.Context, _ ListChannelsRequestObject) (ListChannelsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResNotifications, rbac.LevelView) {
		return ListChannels403JSONResponse{forbidden()}, nil
	}
	channels, err := s.store.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	out := []Channel{}
	for _, channel := range channels {
		out = append(out, toAPIChannel(channel))
	}
	return ListChannels200JSONResponse(out), nil
}

func (s *Server) CreateChannel(ctx context.Context, req CreateChannelRequestObject) (CreateChannelResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResNotifications, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "channel.create", ResNotifications)
		return CreateChannel403JSONResponse{forbidden()}, nil
	}
	if _, known := alerting.ChannelTypes[string(req.Body.Type)]; !known {
		return CreateChannel400JSONResponse{BadRequestJSONResponse(errBody("unknown_type", "unknown channel type"))}, nil
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return CreateChannel400JSONResponse{BadRequestJSONResponse(errBody("invalid_name", "a name is required"))}, nil
	}
	channel, err := s.store.CreateChannel(ctx, string(req.Body.Type), name, req.Body.Config)
	if err != nil {
		if store.IsUniqueViolation(err) {
			return CreateChannel409JSONResponse{ConflictJSONResponse(errBody("name_taken", "a channel with that name already exists"))}, nil
		}
		return nil, err
	}
	if req.Body.Secrets != nil {
		for secretName, value := range *req.Body.Secrets {
			if value == "" {
				continue
			}
			if err := s.vault.Seal(ctx, "channel:"+channel.ID.String(), secretName, []byte(value)); err != nil {
				return nil, err
			}
		}
	}
	s.audit.Log(ctx, actorFor(id), "channel.create", ResNotifications, "success",
		map[string]any{"name": name, "type": req.Body.Type})
	return CreateChannel201JSONResponse(toAPIChannel(*channel)), nil
}

func (s *Server) UpdateChannel(ctx context.Context, req UpdateChannelRequestObject) (UpdateChannelResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResNotifications, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "channel.update", ResNotifications)
		return UpdateChannel403JSONResponse{forbidden()}, nil
	}
	channel, err := s.store.GetChannel(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return UpdateChannel404JSONResponse{notFoundErr("channel")}, nil
		}
		return nil, err
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		name = channel.Name
	}
	enabled := channel.Enabled
	if req.Body.Enabled != nil {
		enabled = *req.Body.Enabled
	}
	if err := s.store.UpdateChannel(ctx, channel.ID, name, req.Body.Config, enabled); err != nil {
		return nil, err
	}
	if req.Body.Secrets != nil {
		for secretName, value := range *req.Body.Secrets {
			if value == "" {
				continue
			}
			if err := s.vault.Seal(ctx, "channel:"+channel.ID.String(), secretName, []byte(value)); err != nil {
				return nil, err
			}
		}
	}
	s.audit.Log(ctx, actorFor(id), "channel.update", ResNotifications, "success", map[string]any{"name": name})
	fresh, err := s.store.GetChannel(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	return UpdateChannel200JSONResponse(toAPIChannel(*fresh)), nil
}

func (s *Server) DeleteChannel(ctx context.Context, req DeleteChannelRequestObject) (DeleteChannelResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResNotifications, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "channel.delete", ResNotifications)
		return DeleteChannel403JSONResponse{forbidden()}, nil
	}
	channel, err := s.store.GetChannel(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteChannel404JSONResponse{notFoundErr("channel")}, nil
		}
		return nil, err
	}
	if err := s.store.DeleteChannel(ctx, channel.ID); err != nil {
		return nil, err
	}
	if err := s.vault.DeleteScope(ctx, "channel:"+channel.ID.String()); err != nil {
		s.logger.Error("delete channel secrets", "error", err)
	}
	s.audit.Log(ctx, actorFor(id), "channel.delete", ResNotifications, "success", map[string]any{"name": channel.Name})
	return DeleteChannel204Response{}, nil
}

func (s *Server) TestChannel(ctx context.Context, req TestChannelRequestObject) (TestChannelResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	if !s.can(ctx, id, ResNotifications, rbac.LevelConfigure) {
		return TestChannel403JSONResponse{forbidden()}, nil
	}
	channel, err := s.store.GetChannel(ctx, req.Id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return TestChannel404JSONResponse{notFoundErr("channel")}, nil
		}
		return nil, err
	}
	msg := alerting.Message{
		Title:     "HomeHub test notification",
		Body:      "If you can read this, the channel \"" + channel.Name + "\" works.",
		Severity:  "info",
		EventType: "test",
		Time:      time.Now(),
	}
	if id.User.Email != nil {
		msg.RecipientEmail = *id.User.Email
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err = alerting.Send(sendCtx, channel, func(name string) string {
		raw, serr := s.vault.Open(ctx, "channel:"+channel.ID.String(), name)
		if serr != nil {
			return ""
		}
		return string(raw)
	}, msg)
	if err != nil {
		return TestChannel200JSONResponse(TestConnectionResult{Ok: false, Message: err.Error()}), nil
	}
	return TestChannel200JSONResponse(TestConnectionResult{Ok: true, Message: "test notification delivered"}), nil
}

// ---- per-user preferences --------------------------------------------------

func (s *Server) GetNotificationPrefs(ctx context.Context, _ GetNotificationPrefsRequestObject) (GetNotificationPrefsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	prefs, err := s.store.ListNotificationPrefs(ctx, id.User.ID)
	if err != nil {
		return nil, err
	}
	channels, err := s.store.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	type prefRow = struct {
		ChannelIds []uuid.UUID `json:"channelIds"`
		EventType  string      `json:"eventType"`
		Inapp      bool        `json:"inapp"`
	}
	out := GetNotificationPrefs200JSONResponse{
		EventTypes: notify.EventTypes,
		Prefs:      []prefRow{},
		Channels:   []ChannelRef{},
	}
	for _, pref := range prefs {
		out.Prefs = append(out.Prefs, prefRow{
			EventType: pref.EventType, Inapp: pref.InApp, ChannelIds: pref.ChannelIDs,
		})
	}
	for _, channel := range channels {
		if channel.Enabled {
			out.Channels = append(out.Channels, ChannelRef{Id: channel.ID, Name: channel.Name, Type: channel.Type})
		}
	}
	return out, nil
}

func (s *Server) PutNotificationPref(ctx context.Context, req PutNotificationPrefRequestObject) (PutNotificationPrefResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	known := false
	for _, eventType := range notify.EventTypes {
		if eventType == req.Body.EventType {
			known = true
		}
	}
	if !known {
		return PutNotificationPref400JSONResponse{BadRequestJSONResponse(errBody("unknown_event", "unknown event type"))}, nil
	}
	// Only enabled, existing channels may be subscribed.
	channels, err := s.store.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	valid := map[uuid.UUID]bool{}
	for _, channel := range channels {
		if channel.Enabled {
			valid[channel.ID] = true
		}
	}
	for _, channelID := range req.Body.ChannelIds {
		if !valid[channelID] {
			return PutNotificationPref400JSONResponse{BadRequestJSONResponse(errBody("unknown_channel", "one of the channels does not exist or is disabled"))}, nil
		}
	}
	if err := s.store.UpsertNotificationPref(ctx, &store.NotificationPref{
		UserID:     id.User.ID,
		EventType:  req.Body.EventType,
		InApp:      req.Body.Inapp,
		ChannelIDs: req.Body.ChannelIds,
	}); err != nil {
		return nil, err
	}
	return PutNotificationPref204Response{}, nil
}
