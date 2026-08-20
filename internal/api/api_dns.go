package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/httpctx"
	"github.com/TheOriUHD/homehub/internal/integrations/adguard"
	dnscore "github.com/TheOriUHD/homehub/internal/integrations/dns"
	"github.com/TheOriUHD/homehub/internal/integrations/pihole"
	"github.com/TheOriUHD/homehub/internal/rbac"
	"github.com/TheOriUHD/homehub/internal/store"
)

func isDNSType(typ string) bool {
	return typ == pihole.TypeID || typ == adguard.TypeID
}

// dnsProvider resolves an integration id to a live normalized DNS provider.
func (s *Server) dnsProvider(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (dnscore.Provider, *store.Integration, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || !isDNSType(row.Type) {
		return nil, nil, "", store.ErrNotFound
	}
	resource := row.Type + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, row, resource, err
	}
	provider, ok := conn.(dnscore.Provider)
	if !ok {
		return nil, row, resource, errors.New("integration connection has unexpected type")
	}
	return provider, row, resource, nil
}

func toAPIDnsCapabilities(caps dnscore.Capabilities) DnsCapabilities {
	return DnsCapabilities{
		GroupCrud:   caps.GroupCRUD,
		ClientCrud:  caps.ClientCRUD,
		RegexLists:  caps.RegexLists,
		CustomRules: caps.CustomRules,
		ListComment: caps.ListComment,
	}
}

func toAPIDnsStats(stats *dnscore.Stats) DnsStats {
	return DnsStats{
		QueriesToday:   stats.QueriesToday,
		BlockedToday:   stats.BlockedToday,
		BlockedPct:     float32(stats.BlockedPct),
		ClientsSeen:    stats.ClientsSeen,
		DomainsOnLists: stats.DomainsOnLists,
		Enabled:        stats.Enabled,
		DisabledUntil:  stats.DisabledUntil,
		Version:        stats.Version,
	}
}

func toAPIDnsListEntry(entry dnscore.ListEntry) DnsListEntry {
	out := DnsListEntry{
		Id:      entry.ID,
		Type:    DnsListType(entry.Type),
		Domain:  entry.Domain,
		Enabled: entry.Enabled,
	}
	if entry.Comment != "" {
		comment := entry.Comment
		out.Comment = &comment
	}
	return out
}

// DnsListInstances aggregates every visible DNS instance with live stats.
func (s *Server) DnsListInstances(ctx context.Context, _ DnsListInstancesRequestObject) (DnsListInstancesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	rows, err := s.store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := []DnsInstance{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, row := range rows {
		if !isDNSType(row.Type) || !row.Enabled {
			continue
		}
		resource := row.Type + "/" + row.ID.String()
		if !s.visibleUnder(ctx, id, resource) {
			continue
		}
		wg.Add(1)
		go func(row store.Integration) {
			defer wg.Done()
			instance := DnsInstance{
				Id:            row.ID,
				Name:          row.Name,
				Type:          DnsInstanceType(row.Type),
				Status:        DnsInstanceStatus(row.Status),
				StatusMessage: row.StatusMessage,
			}
			if conn, cerr := s.integrations.Connection(row.ID); cerr == nil {
				if provider, ok := conn.(dnscore.Provider); ok {
					instance.Capabilities = toAPIDnsCapabilities(provider.Capabilities())
					statsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					if stats, serr := provider.Stats(statsCtx); serr == nil {
						apiStats := toAPIDnsStats(stats)
						instance.Stats = &apiStats
					}
					cancel()
				}
			}
			mu.Lock()
			out = append(out, instance)
			mu.Unlock()
		}(row)
	}
	wg.Wait()
	return DnsListInstances200JSONResponse(out), nil
}

func (s *Server) DnsQueryLog(ctx context.Context, req DnsQueryLogRequestObject) (DnsQueryLogResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsQueryLog404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DnsQueryLog403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsQueryLog502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	filter := dnscore.QueryFilter{}
	if req.Params.Search != nil {
		filter.Search = *req.Params.Search
	}
	if req.Params.Client != nil {
		filter.Client = *req.Params.Client
	}
	if req.Params.Status != nil {
		filter.Status = dnscore.QueryStatus(*req.Params.Status)
	}
	if req.Params.Limit != nil {
		filter.Limit = *req.Params.Limit
	}
	entries, err := provider.Queries(ctx, filter)
	if err != nil {
		return DnsQueryLog502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := make([]DnsQuery, 0, len(entries))
	for _, entry := range entries {
		item := DnsQuery{
			Time:   entry.Time,
			Domain: entry.Domain,
			Client: entry.Client,
			Type:   entry.Type,
			Status: DnsQueryStatus(entry.Status),
		}
		if entry.ClientName != "" {
			name := entry.ClientName
			item.ClientName = &name
		}
		if entry.Reason != "" {
			reason := entry.Reason
			item.Reason = &reason
		}
		out = append(out, item)
	}
	return DnsQueryLog200JSONResponse(out), nil
}

func (s *Server) DnsLists(ctx context.Context, req DnsListsRequestObject) (DnsListsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsLists404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DnsLists403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsLists502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	entries, err := provider.Lists(ctx)
	if err != nil {
		return DnsLists502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := make([]DnsListEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, toAPIDnsListEntry(entry))
	}
	return DnsLists200JSONResponse(out), nil
}

func (s *Server) DnsAddListEntry(ctx context.Context, req DnsAddListEntryRequestObject) (DnsAddListEntryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsAddListEntry404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.list_add", resource)
		return DnsAddListEntry403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsAddListEntry502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	entry := dnscore.ListEntry{Type: dnscore.ListType(req.Body.Type), Domain: req.Body.Domain, Enabled: true}
	if req.Body.Comment != nil {
		entry.Comment = *req.Body.Comment
	}
	if aerr := provider.AddListEntry(ctx, entry); aerr != nil {
		if errors.Is(aerr, dnscore.ErrUnsupported) {
			return DnsAddListEntry400JSONResponse{BadRequestJSONResponse(errBody("unsupported", dnscore.ErrUnsupported.Error()))}, nil
		}
		s.audit.Log(ctx, actorFor(id), "dns.list_add", resource, "failure", map[string]any{"error": aerr.Error()})
		return DnsAddListEntry502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", aerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.list_add", resource, "success",
		map[string]any{"type": req.Body.Type, "domain": req.Body.Domain})
	return DnsAddListEntry204Response{}, nil
}

func (s *Server) DnsRemoveListEntry(ctx context.Context, req DnsRemoveListEntryRequestObject) (DnsRemoveListEntryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsRemoveListEntry404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.list_remove", resource)
		return DnsRemoveListEntry403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsRemoveListEntry502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	entry := dnscore.ListEntry{Type: dnscore.ListType(req.Body.Type), Domain: req.Body.Domain}
	if req.Body.Id != nil {
		entry.ID = *req.Body.Id
	}
	if rerr := provider.RemoveListEntry(ctx, entry); rerr != nil {
		if errors.Is(rerr, dnscore.ErrUnsupported) {
			return DnsRemoveListEntry400JSONResponse{BadRequestJSONResponse(errBody("unsupported", dnscore.ErrUnsupported.Error()))}, nil
		}
		s.audit.Log(ctx, actorFor(id), "dns.list_remove", resource, "failure", map[string]any{"error": rerr.Error()})
		return DnsRemoveListEntry502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", rerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.list_remove", resource, "success",
		map[string]any{"type": req.Body.Type, "domain": req.Body.Domain})
	return DnsRemoveListEntry204Response{}, nil
}

func (s *Server) DnsSetBlocking(ctx context.Context, req DnsSetBlockingRequestObject) (DnsSetBlockingResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsSetBlocking404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "dns.blocking", resource)
		return DnsSetBlocking403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsSetBlocking502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	duration := time.Duration(0)
	if req.Body.Seconds != nil {
		duration = time.Duration(*req.Body.Seconds) * time.Second
	}
	if serr := provider.SetBlocking(ctx, req.Body.Enabled, duration); serr != nil {
		s.audit.Log(ctx, actorFor(id), "dns.blocking", resource, "failure", map[string]any{"error": serr.Error()})
		return DnsSetBlocking502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.blocking", resource, "success",
		map[string]any{"enabled": req.Body.Enabled, "seconds": req.Body.Seconds})

	state, serr := provider.Blocking(ctx)
	if serr != nil {
		return DnsSetBlocking502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", serr.Error()))}, nil
	}
	return DnsSetBlocking200JSONResponse(DnsBlocking{Enabled: state.Enabled, Until: state.Until}), nil
}

func (s *Server) DnsClients(ctx context.Context, req DnsClientsRequestObject) (DnsClientsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsClients404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DnsClients403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsClients502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	clients, cerr := provider.Clients(ctx)
	if cerr != nil {
		return DnsClients502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", cerr.Error()))}, nil
	}
	groups, gerr := provider.Groups(ctx)
	if gerr != nil {
		return DnsClients502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", gerr.Error()))}, nil
	}
	resp := DnsClients200JSONResponse{
		Capabilities: toAPIDnsCapabilities(provider.Capabilities()),
		Clients:      make([]DnsClient, 0, len(clients)),
		Groups:       make([]DnsGroup, 0, len(groups)),
	}
	for _, client := range clients {
		ids := client.Identifiers
		if ids == nil {
			ids = []string{}
		}
		groupIDs := client.GroupIDs
		if groupIDs == nil {
			groupIDs = []string{}
		}
		resp.Clients = append(resp.Clients, DnsClient{
			Id: client.ID, Name: client.Name, Identifiers: ids, GroupIds: groupIDs,
		})
	}
	for _, group := range groups {
		resp.Groups = append(resp.Groups, DnsGroup{Id: group.ID, Name: group.Name, Enabled: group.Enabled})
	}
	return resp, nil
}

func (s *Server) DnsUpsertClient(ctx context.Context, req DnsUpsertClientRequestObject) (DnsUpsertClientResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsUpsertClient404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.client_upsert", resource)
		return DnsUpsertClient403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsUpsertClient502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	client := dnscore.Client{
		ID:          req.Body.Id,
		Name:        req.Body.Name,
		Identifiers: req.Body.Identifiers,
		GroupIDs:    req.Body.GroupIds,
	}
	if uerr := provider.UpsertClient(ctx, client); uerr != nil {
		if errors.Is(uerr, dnscore.ErrUnsupported) {
			return DnsUpsertClient400JSONResponse{BadRequestJSONResponse(errBody("unsupported", dnscore.ErrUnsupported.Error()))}, nil
		}
		s.audit.Log(ctx, actorFor(id), "dns.client_upsert", resource, "failure", map[string]any{"error": uerr.Error()})
		return DnsUpsertClient502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", uerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.client_upsert", resource, "success", map[string]any{"client": req.Body.Name})
	return DnsUpsertClient204Response{}, nil
}

func (s *Server) DnsDeleteClient(ctx context.Context, req DnsDeleteClientRequestObject) (DnsDeleteClientResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsDeleteClient404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.client_delete", resource)
		return DnsDeleteClient403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsDeleteClient502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	if derr := provider.DeleteClient(ctx, req.Body.Id); derr != nil {
		if errors.Is(derr, dnscore.ErrUnsupported) {
			return DnsDeleteClient400JSONResponse{BadRequestJSONResponse(errBody("unsupported", dnscore.ErrUnsupported.Error()))}, nil
		}
		s.audit.Log(ctx, actorFor(id), "dns.client_delete", resource, "failure", map[string]any{"error": derr.Error()})
		return DnsDeleteClient502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", derr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.client_delete", resource, "success", map[string]any{"client": req.Body.Id})
	return DnsDeleteClient204Response{}, nil
}

func (s *Server) DnsUpsertGroup(ctx context.Context, req DnsUpsertGroupRequestObject) (DnsUpsertGroupResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsUpsertGroup404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.group_upsert", resource)
		return DnsUpsertGroup403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsUpsertGroup502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	group := dnscore.Group{ID: req.Body.Id, Name: req.Body.Name, Enabled: req.Body.Enabled}
	if uerr := provider.UpsertGroup(ctx, group); uerr != nil {
		if errors.Is(uerr, dnscore.ErrUnsupported) {
			return DnsUpsertGroup400JSONResponse{BadRequestJSONResponse(errBody("unsupported", dnscore.ErrUnsupported.Error()))}, nil
		}
		s.audit.Log(ctx, actorFor(id), "dns.group_upsert", resource, "failure", map[string]any{"error": uerr.Error()})
		return DnsUpsertGroup502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", uerr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.group_upsert", resource, "success", map[string]any{"group": req.Body.Name})
	return DnsUpsertGroup204Response{}, nil
}

func (s *Server) DnsDeleteGroup(ctx context.Context, req DnsDeleteGroupRequestObject) (DnsDeleteGroupResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsDeleteGroup404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.group_delete", resource)
		return DnsDeleteGroup403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DnsDeleteGroup502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	if derr := provider.DeleteGroup(ctx, req.Body.Id); derr != nil {
		if errors.Is(derr, dnscore.ErrUnsupported) {
			return DnsDeleteGroup400JSONResponse{BadRequestJSONResponse(errBody("unsupported", dnscore.ErrUnsupported.Error()))}, nil
		}
		s.audit.Log(ctx, actorFor(id), "dns.group_delete", resource, "failure", map[string]any{"error": derr.Error()})
		return DnsDeleteGroup502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", derr.Error()))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "dns.group_delete", resource, "success", map[string]any{"group": req.Body.Id})
	return DnsDeleteGroup204Response{}, nil
}

// syncKey identifies a normalized entry across instances.
func syncKey(entry dnscore.ListEntry) string {
	return string(entry.Type) + "\x00" + entry.Domain
}

func (s *Server) DnsCompare(ctx context.Context, req DnsCompareRequestObject) (DnsCompareResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	providerA, _, resourceA, errA := s.dnsProvider(ctx, id, req.Params.A)
	providerB, _, resourceB, errB := s.dnsProvider(ctx, id, req.Params.B)
	if errors.Is(errA, store.ErrNotFound) || errors.Is(errB, store.ErrNotFound) {
		return DnsCompare404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	if !s.can(ctx, id, resourceA, rbac.LevelView) || !s.can(ctx, id, resourceB, rbac.LevelView) {
		return DnsCompare403JSONResponse{forbidden()}, nil
	}
	if errA != nil {
		return DnsCompare502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", errA.Error()))}, nil
	}
	if errB != nil {
		return DnsCompare502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", errB.Error()))}, nil
	}
	listA, aerr := providerA.Lists(ctx)
	if aerr != nil {
		return DnsCompare502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", aerr.Error()))}, nil
	}
	listB, berr := providerB.Lists(ctx)
	if berr != nil {
		return DnsCompare502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", berr.Error()))}, nil
	}

	inB := map[string]bool{}
	for _, entry := range listB {
		if entry.Type != dnscore.ListCustom {
			inB[syncKey(entry)] = true
		}
	}
	inA := map[string]bool{}
	result := DnsCompareResult{OnlyA: []DnsListEntry{}, OnlyB: []DnsListEntry{}}
	for _, entry := range listA {
		if entry.Type == dnscore.ListCustom {
			continue
		}
		inA[syncKey(entry)] = true
		if inB[syncKey(entry)] {
			result.CommonCount++
		} else {
			result.OnlyA = append(result.OnlyA, toAPIDnsListEntry(entry))
		}
	}
	for _, entry := range listB {
		if entry.Type != dnscore.ListCustom && !inA[syncKey(entry)] {
			result.OnlyB = append(result.OnlyB, toAPIDnsListEntry(entry))
		}
	}
	return DnsCompare200JSONResponse(result), nil
}

func (s *Server) DnsSync(ctx context.Context, req DnsSyncRequestObject) (DnsSyncResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, _, resourceFrom, errFrom := s.dnsProvider(ctx, id, req.Body.From)
	providerTo, _, resourceTo, errTo := s.dnsProvider(ctx, id, req.Body.To)
	if errors.Is(errFrom, store.ErrNotFound) || errors.Is(errTo, store.ErrNotFound) {
		return DnsSync404JSONResponse{notFoundErr("DNS instance")}, nil
	}
	// Reading from the source needs view; writing to the target configure.
	if !s.can(ctx, id, resourceFrom, rbac.LevelView) || !s.can(ctx, id, resourceTo, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "dns.sync", resourceTo)
		return DnsSync403JSONResponse{forbidden()}, nil
	}
	if errTo != nil {
		return DnsSync404JSONResponse{NotFoundJSONResponse(errBody("unreachable", errTo.Error()))}, nil
	}

	type failRow = struct {
		Domain string `json:"domain"`
		Error  string `json:"error"`
	}
	result := DnsSync200JSONResponse{Failed: []failRow{}}
	for _, input := range req.Body.Entries {
		entry := dnscore.ListEntry{Type: dnscore.ListType(input.Type), Domain: input.Domain, Enabled: true}
		if input.Comment != nil {
			entry.Comment = *input.Comment
		}
		if err := providerTo.AddListEntry(ctx, entry); err != nil {
			result.Failed = append(result.Failed, failRow{Domain: input.Domain, Error: err.Error()})
			continue
		}
		result.Added++
	}
	s.audit.Log(ctx, actorFor(id), "dns.sync", resourceTo, "success",
		map[string]any{"from": req.Body.From, "added": result.Added, "failed": len(result.Failed)})
	return result, nil
}

// DnsOverview serves the deep single-instance dashboard: leaderboards,
// history, latency. Providers that can't supply it get an empty (not error)
// answer so the dashboard renders what exists.
func (s *Server) DnsOverview(ctx context.Context, req DnsOverviewRequestObject) (DnsOverviewResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	provider, _, resource, err := s.dnsProvider(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DnsOverview404JSONResponse{notFoundErr("instance")}, nil
	}
	// Same instance-view gate as every sibling DNS read endpoint.
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DnsOverview404JSONResponse{notFoundErr("instance")}, nil
	}
	if err != nil {
		return DnsOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", err.Error()))}, nil
	}
	out := DnsOverview{
		Series: []DnsSeriesPoint{}, TopDomains: []DnsTopItem{}, TopBlocked: []DnsTopItem{},
		TopClients: []DnsTopItem{}, TopUpstreams: []DnsTopItem{},
	}
	op, ok := provider.(dnscore.OverviewProvider)
	if !ok {
		return DnsOverview200JSONResponse(out), nil
	}
	ov, oerr := op.Overview(ctx)
	if errors.Is(oerr, dnscore.ErrUnsupported) {
		return DnsOverview200JSONResponse(out), nil
	}
	if oerr != nil {
		return DnsOverview502JSONResponse{UpstreamErrorJSONResponse(errBody("upstream", oerr.Error()))}, nil
	}
	toItems := func(rows []dnscore.TopItem) []DnsTopItem {
		items := make([]DnsTopItem, 0, len(rows))
		for _, r := range rows {
			items = append(items, DnsTopItem{Name: r.Name, Count: r.Count})
		}
		return items
	}
	out.TopDomains = toItems(ov.TopDomains)
	out.TopBlocked = toItems(ov.TopBlocked)
	out.TopClients = toItems(ov.TopClients)
	out.TopUpstreams = toItems(ov.TopUpstreams)
	if ov.AvgProcessingMs != nil {
		v := float32(*ov.AvgProcessingMs)
		out.AvgProcessingMs = &v
	}
	for _, p := range ov.Series {
		out.Series = append(out.Series, DnsSeriesPoint{Ts: p.TS, Queries: p.Queries, Blocked: p.Blocked})
	}
	return DnsOverview200JSONResponse(out), nil
}
