package api

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	networktypes "github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	"github.com/google/uuid"

	"homehub/internal/httpctx"
	dockerint "homehub/internal/integrations/docker"
	"homehub/internal/rbac"
	"homehub/internal/store"
)

// ---- images ----------------------------------------------------------------

func (s *Server) DockerListImages(ctx context.Context, req DockerListImagesRequestObject) (DockerListImagesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerListImages404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DockerListImages403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerListImages502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	images, err := conn.Client.ImageList(ctx, imagetypes.ListOptions{All: false})
	if err != nil {
		return DockerListImages502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	inUse := map[string]bool{}
	if containers, cerr := conn.Client.ContainerList(ctx, containerListAll()); cerr == nil {
		for _, c := range containers {
			inUse[c.ImageID] = true
		}
	}
	out := []DockerImage{}
	for _, img := range images {
		tags := img.RepoTags
		if tags == nil {
			tags = []string{}
		}
		dangling := len(tags) == 0 || (len(tags) == 1 && tags[0] == "<none>:<none>")
		if dangling {
			tags = []string{}
		}
		out = append(out, DockerImage{
			Id:        img.ID,
			Tags:      tags,
			SizeBytes: img.Size,
			Created:   time.Unix(img.Created, 0),
			Dangling:  dangling,
			InUse:     inUse[img.ID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return DockerListImages200JSONResponse(out), nil
}

func (s *Server) DockerPullImage(ctx context.Context, req DockerPullImageRequestObject) (DockerPullImageResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerPullImage404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "docker.image_pull", resource)
		return DockerPullImage403JSONResponse{forbidden()}, nil
	}
	job, err := s.jobs.Enqueue(ctx, dockerint.JobPull, dockerint.JobPayload{
		IntegrationID: req.Id,
		UserID:        id.User.ID,
		Reference:     req.Body.Reference,
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "docker.image_pull", resource, "success",
		map[string]any{"reference": req.Body.Reference, "job": job.ID})
	return DockerPullImage202JSONResponse{JobQueuedJSONResponse{JobId: job.ID}}, nil
}

func (s *Server) DockerCheckImageUpdates(ctx context.Context, req DockerCheckImageUpdatesRequestObject) (DockerCheckImageUpdatesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerCheckImageUpdates404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DockerCheckImageUpdates403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerCheckImageUpdates502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	images, err := conn.Client.ImageList(ctx, imagetypes.ListOptions{})
	if err != nil {
		return DockerCheckImageUpdates502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}

	type resultRow = struct {
		Message         *string `json:"message,omitempty"`
		Reference       string  `json:"reference"`
		UpdateAvailable bool    `json:"updateAvailable"`
	}
	result := DockerCheckImageUpdates200JSONResponse{Results: []resultRow{}}
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if strings.HasPrefix(tag, "<none>") {
				continue
			}
			row := resultRow{Reference: tag}
			auth := s.dockerRegistryAuth(ctx, req.Id, tag)
			dist, derr := conn.Client.DistributionInspect(ctx, tag, auth)
			switch {
			case derr != nil:
				msg := dockerint.FriendlyError(derr)
				row.Message = &msg
			default:
				remote := dist.Descriptor.Digest.String()
				local := false
				for _, digest := range img.RepoDigests {
					if strings.HasSuffix(digest, remote) {
						local = true
					}
				}
				row.UpdateAvailable = !local && remote != ""
			}
			result.Results = append(result.Results, row)
		}
	}
	return result, nil
}

func (s *Server) DockerRemoveImage(ctx context.Context, req DockerRemoveImageRequestObject) (DockerRemoveImageResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerRemoveImage404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "docker.image_remove", resource)
		return DockerRemoveImage403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerRemoveImage502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	force := req.Params.Force != nil && *req.Params.Force
	if _, err := conn.Client.ImageRemove(ctx, req.ImageId, imagetypes.RemoveOptions{Force: force}); err != nil {
		s.audit.Log(ctx, actorFor(id), "docker.image_remove", resource, "failure", map[string]any{"error": err.Error()})
		return DockerRemoveImage502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.image_remove", resource, "success", map[string]any{"image": req.ImageId})
	return DockerRemoveImage204Response{}, nil
}

func (s *Server) DockerPruneImages(ctx context.Context, req DockerPruneImagesRequestObject) (DockerPruneImagesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerPruneImages404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "docker.image_prune", resource)
		return DockerPruneImages403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerPruneImages502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	report, err := conn.Client.ImagesPrune(ctx, filters.NewArgs())
	if err != nil {
		return DockerPruneImages502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.image_prune", resource, "success",
		map[string]any{"reclaimedBytes": report.SpaceReclaimed, "deleted": len(report.ImagesDeleted)})
	return DockerPruneImages200JSONResponse(DockerPruneResult{
		ReclaimedBytes: int64(report.SpaceReclaimed),
		Deleted:        len(report.ImagesDeleted),
	}), nil
}

// ---- volumes ---------------------------------------------------------------

func (s *Server) DockerListVolumes(ctx context.Context, req DockerListVolumesRequestObject) (DockerListVolumesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerListVolumes404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DockerListVolumes403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerListVolumes502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	volumes, err := conn.Client.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return DockerListVolumes502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	used := map[string]bool{}
	if containers, cerr := conn.Client.ContainerList(ctx, containerListAll()); cerr == nil {
		for _, c := range containers {
			for _, mount := range c.Mounts {
				if mount.Name != "" {
					used[mount.Name] = true
				}
			}
		}
	}
	out := []DockerVolume{}
	for _, vol := range volumes.Volumes {
		entry := DockerVolume{
			Name:       vol.Name,
			Driver:     vol.Driver,
			Mountpoint: vol.Mountpoint,
			InUse:      used[vol.Name],
		}
		if vol.CreatedAt != "" {
			created := vol.CreatedAt
			entry.Created = &created
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return DockerListVolumes200JSONResponse(out), nil
}

func (s *Server) DockerRemoveVolume(ctx context.Context, req DockerRemoveVolumeRequestObject) (DockerRemoveVolumeResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerRemoveVolume404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "docker.volume_remove", resource)
		return DockerRemoveVolume403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerRemoveVolume502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	force := req.Params.Force != nil && *req.Params.Force
	if err := conn.Client.VolumeRemove(ctx, req.Name, force); err != nil {
		s.audit.Log(ctx, actorFor(id), "docker.volume_remove", resource, "failure", map[string]any{"error": err.Error()})
		return DockerRemoveVolume502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.volume_remove", resource, "success", map[string]any{"volume": req.Name})
	return DockerRemoveVolume204Response{}, nil
}

func (s *Server) DockerPruneVolumes(ctx context.Context, req DockerPruneVolumesRequestObject) (DockerPruneVolumesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerPruneVolumes404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "docker.volume_prune", resource)
		return DockerPruneVolumes403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerPruneVolumes502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	report, err := conn.Client.VolumesPrune(ctx, filters.NewArgs())
	if err != nil {
		return DockerPruneVolumes502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.volume_prune", resource, "success",
		map[string]any{"reclaimedBytes": report.SpaceReclaimed, "deleted": len(report.VolumesDeleted)})
	return DockerPruneVolumes200JSONResponse(DockerPruneResult{
		ReclaimedBytes: int64(report.SpaceReclaimed),
		Deleted:        len(report.VolumesDeleted),
	}), nil
}

// ---- networks --------------------------------------------------------------

func (s *Server) DockerListNetworks(ctx context.Context, req DockerListNetworksRequestObject) (DockerListNetworksResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerListNetworks404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DockerListNetworks403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerListNetworks502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	networks, err := conn.Client.NetworkList(ctx, networkListOptions())
	if err != nil {
		return DockerListNetworks502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	out := []DockerNetwork{}
	for _, net := range networks {
		builtin := net.Name == "bridge" || net.Name == "host" || net.Name == "none"
		out = append(out, DockerNetwork{
			Id:         net.ID,
			Name:       net.Name,
			Driver:     net.Driver,
			Scope:      net.Scope,
			Containers: len(net.Containers),
			Builtin:    builtin,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return DockerListNetworks200JSONResponse(out), nil
}

func (s *Server) DockerRemoveNetwork(ctx context.Context, req DockerRemoveNetworkRequestObject) (DockerRemoveNetworkResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerRemoveNetwork404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "docker.network_remove", resource)
		return DockerRemoveNetwork403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerRemoveNetwork502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	if err := conn.Client.NetworkRemove(ctx, req.NetworkId); err != nil {
		s.audit.Log(ctx, actorFor(id), "docker.network_remove", resource, "failure", map[string]any{"error": err.Error()})
		return DockerRemoveNetwork502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.network_remove", resource, "success", map[string]any{"network": req.NetworkId})
	return DockerRemoveNetwork204Response{}, nil
}

// ---- stacks ----------------------------------------------------------------

func (s *Server) DockerListStacks(ctx context.Context, req DockerListStacksRequestObject) (DockerListStacksResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerListStacks404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelView) {
		return DockerListStacks403JSONResponse{forbidden()}, nil
	}
	if err != nil {
		return DockerListStacks502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	list, err := conn.Client.ContainerList(ctx, containerListAll())
	if err != nil {
		return DockerListStacks502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}

	type serviceRow = struct {
		Containers []DockerContainer `json:"containers"`
		Service    string            `json:"service"`
	}
	grouped := map[string]map[string][]DockerContainer{}
	for _, item := range list {
		project := item.Labels["com.docker.compose.project"]
		if project == "" {
			continue
		}
		service := item.Labels["com.docker.compose.service"]
		if service == "" {
			service = dockerint.ResourceName(item.Names, item.ID)
		}
		if grouped[project] == nil {
			grouped[project] = map[string][]DockerContainer{}
		}
		grouped[project][service] = append(grouped[project][service], toAPIDockerContainer(item))
	}

	out := []DockerStack{}
	for project, services := range grouped {
		stack := DockerStack{Project: project, Services: []serviceRow{}}
		names := make([]string, 0, len(services))
		for name := range services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			stack.Services = append(stack.Services, serviceRow{Service: name, Containers: services[name]})
		}
		out = append(out, stack)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return DockerListStacks200JSONResponse(out), nil
}

func (s *Server) DockerRedeployStack(ctx context.Context, req DockerRedeployStackRequestObject) (DockerRedeployStackResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerRedeployStack404JSONResponse{notFoundErr("integration")}, nil
	}
	if !s.can(ctx, id, resource, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "docker.stack_redeploy", resource)
		return DockerRedeployStack403JSONResponse{forbidden()}, nil
	}
	pull := req.Body.Pull == nil || *req.Body.Pull
	job, err := s.jobs.Enqueue(ctx, dockerint.JobRedeploy, dockerint.JobPayload{
		IntegrationID: req.Id,
		UserID:        id.User.ID,
		Project:       req.Project,
		Pull:          pull,
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "docker.stack_redeploy", resource, "success",
		map[string]any{"project": req.Project, "pull": pull, "job": job.ID})
	return DockerRedeployStack202JSONResponse{JobQueuedJSONResponse{JobId: job.ID}}, nil
}

// ---- registries ------------------------------------------------------------

const registrySecretPrefix = "registry:"

func (s *Server) DockerListRegistries(ctx context.Context, req DockerListRegistriesRequestObject) (DockerListRegistriesResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil || row.Type != dockerint.TypeID {
		return DockerListRegistries404JSONResponse{notFoundErr("integration")}, nil
	}
	resource := dockerint.TypeID + "/" + req.Id.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return DockerListRegistries403JSONResponse{forbidden()}, nil
	}
	registries := []DockerRegistryCred{}
	if found, gerr := s.getRegistryIndex(ctx, req.Id); gerr == nil {
		for host, username := range found {
			registries = append(registries, DockerRegistryCred{Registry: host, Username: username})
		}
	}
	sort.Slice(registries, func(i, j int) bool { return registries[i].Registry < registries[j].Registry })
	return DockerListRegistries200JSONResponse(registries), nil
}

func (s *Server) DockerPutRegistry(ctx context.Context, req DockerPutRegistryRequestObject) (DockerPutRegistryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil || row.Type != dockerint.TypeID {
		return DockerPutRegistry404JSONResponse{notFoundErr("integration")}, nil
	}
	resource := dockerint.TypeID + "/" + req.Id.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		s.deniedAudit(ctx, id, "docker.registry_credential", resource)
		return DockerPutRegistry403JSONResponse{forbidden()}, nil
	}
	host := strings.TrimSpace(req.Body.Registry)
	if host == "" || strings.ContainsAny(host, "/ ") {
		return DockerPutRegistry400JSONResponse{BadRequestJSONResponse(errBody("invalid_registry", "registry must be a bare host like ghcr.io"))}, nil
	}
	index, _ := s.getRegistryIndex(ctx, req.Id)
	if index == nil {
		index = map[string]string{}
	}
	index[host] = req.Body.Username
	scope := "integration:" + req.Id.String()
	if err := s.vault.Seal(ctx, scope, registrySecretPrefix+host, []byte(req.Body.Secret)); err != nil {
		return nil, err
	}
	if err := s.putRegistryIndex(ctx, req.Id, index); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "docker.registry_credential", resource, "success",
		map[string]any{"registry": host, "username": req.Body.Username})
	return DockerPutRegistry204Response{}, nil
}

func (s *Server) DockerDeleteRegistry(ctx context.Context, req DockerDeleteRegistryRequestObject) (DockerDeleteRegistryResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	row, err := s.store.GetIntegration(ctx, req.Id)
	if err != nil || row.Type != dockerint.TypeID {
		return DockerDeleteRegistry404JSONResponse{notFoundErr("integration")}, nil
	}
	resource := dockerint.TypeID + "/" + req.Id.String()
	if !s.can(ctx, id, resource, rbac.LevelConfigure) {
		return DockerDeleteRegistry403JSONResponse{forbidden()}, nil
	}
	index, _ := s.getRegistryIndex(ctx, req.Id)
	delete(index, req.Registry)
	if err := s.putRegistryIndex(ctx, req.Id, index); err != nil {
		return nil, err
	}
	if err := s.vault.Delete(ctx, "integration:"+req.Id.String(), registrySecretPrefix+req.Registry); err != nil {
		s.logger.Error("delete registry secret", "error", err)
	}
	s.audit.Log(ctx, actorFor(id), "docker.registry_credential_delete", resource, "success",
		map[string]any{"registry": req.Registry})
	return DockerDeleteRegistry204Response{}, nil
}

// Registry index (host → username) lives in settings; secrets in the vault.
func (s *Server) getRegistryIndex(ctx context.Context, instID uuid.UUID) (map[string]string, error) {
	index := map[string]string{}
	_, err := s.store.GetSettingInto(ctx, "docker_registries_"+instID.String(), &index)
	return index, err
}

func (s *Server) putRegistryIndex(ctx context.Context, instID uuid.UUID, index map[string]string) error {
	return s.store.SetSetting(ctx, "docker_registries_"+instID.String(), index)
}

// dockerRegistryAuth resolves the stored auth blob for an image reference.
func (s *Server) dockerRegistryAuth(ctx context.Context, instID uuid.UUID, reference string) string {
	host := dockerint.RegistryHost(reference)
	index, err := s.getRegistryIndex(ctx, instID)
	if err != nil {
		return ""
	}
	username, ok := index[host]
	if !ok {
		return ""
	}
	secret, err := s.vault.Open(ctx, "integration:"+instID.String(), registrySecretPrefix+host)
	if err != nil {
		return ""
	}
	return dockerint.EncodeRegistryAuth(username, string(secret), host)
}

func containerListAll() containertypes.ListOptions {
	return containertypes.ListOptions{All: true}
}

func networkListOptions() networktypes.NetworkListOptions {
	return networktypes.NetworkListOptions{}
}
