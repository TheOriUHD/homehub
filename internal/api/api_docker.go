package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/google/uuid"

	"homehub/internal/httpctx"
	dockerint "homehub/internal/integrations/docker"
	"homehub/internal/rbac"
	"homehub/internal/store"
	"homehub/internal/term"
)

// dockerConn resolves an integration id to a live Docker connection.
func (s *Server) dockerConn(ctx context.Context, id *httpctx.Identity, instID uuid.UUID) (*dockerint.Conn, string, error) {
	row, err := s.store.GetIntegration(ctx, instID)
	if err != nil || row.Type != dockerint.TypeID {
		return nil, "", store.ErrNotFound
	}
	resource := dockerint.TypeID + "/" + instID.String()
	if !s.visibleUnder(ctx, id, resource) {
		return nil, "", store.ErrNotFound
	}
	conn, err := s.integrations.Connection(instID)
	if err != nil {
		return nil, resource, err
	}
	dc, ok := conn.(*dockerint.Conn)
	if !ok {
		return nil, resource, errors.New("integration connection has unexpected type")
	}
	return dc, resource, nil
}

func dockerUpstreamErr(err error) Error {
	return errBody("upstream", dockerint.FriendlyError(err))
}

func containerResource(resource, name string) string {
	return resource + "/container/" + name
}

func toAPIDockerContainer(item types.Container) DockerContainer {
	name := dockerint.ResourceName(item.Names, item.ID)
	out := DockerContainer{
		Id:      item.ID,
		Name:    name,
		Image:   item.Image,
		State:   item.State,
		Status:  item.Status,
		Created: time.Unix(item.Created, 0),
	}
	if item.ImageID != "" {
		imageID := item.ImageID
		out.ImageId = &imageID
	}
	if project := item.Labels["com.docker.compose.project"]; project != "" {
		out.ComposeProject = &project
	}
	if service := item.Labels["com.docker.compose.service"]; service != "" {
		out.ComposeService = &service
	}
	if len(item.Ports) > 0 {
		ports := make([]struct {
			Private int    `json:"private"`
			Public  *int   `json:"public,omitempty"`
			Type    string `json:"type"`
		}, 0, len(item.Ports))
		seen := map[string]bool{}
		for _, p := range item.Ports {
			key := fmt.Sprintf("%d/%s/%d", p.PrivatePort, p.Type, p.PublicPort)
			if seen[key] {
				continue
			}
			seen[key] = true
			entry := struct {
				Private int    `json:"private"`
				Public  *int   `json:"public,omitempty"`
				Type    string `json:"type"`
			}{Private: int(p.PrivatePort), Type: p.Type}
			if p.PublicPort > 0 {
				public := int(p.PublicPort)
				entry.Public = &public
			}
			ports = append(ports, entry)
		}
		out.Ports = &ports
	}
	return out
}

func (s *Server) DockerListContainers(ctx context.Context, req DockerListContainersRequestObject) (DockerListContainersResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerListContainers404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return DockerListContainers502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	list, err := conn.Client.ContainerList(ctx, containertypes.ListOptions{All: true})
	if err != nil {
		return DockerListContainers502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	out := []DockerContainer{}
	for _, item := range list {
		name := dockerint.ResourceName(item.Names, item.ID)
		if !s.can(ctx, id, containerResource(resource, name), rbac.LevelView) {
			continue
		}
		out = append(out, toAPIDockerContainer(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].State == "running") != (out[j].State == "running") {
			return out[i].State == "running"
		}
		return out[i].Name < out[j].Name
	})
	return DockerListContainers200JSONResponse(out), nil
}

func (s *Server) DockerGetContainer(ctx context.Context, req DockerGetContainerRequestObject) (DockerGetContainerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerGetContainer404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return DockerGetContainer502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	if !s.can(ctx, id, containerResource(resource, req.Name), rbac.LevelView) {
		return DockerGetContainer403JSONResponse{forbidden()}, nil
	}
	item, err := conn.Locate(ctx, req.Name)
	if err != nil {
		return DockerGetContainer404JSONResponse{notFoundErr("container")}, nil
	}
	inspect, err := conn.Client.ContainerInspect(ctx, item.ID)
	if err != nil {
		return DockerGetContainer502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}

	detail := DockerContainerDetail{
		Container: toAPIDockerContainer(item),
		Env:       []string{},
		Labels:    map[string]string{},
		Mounts: []struct {
			Destination string  `json:"destination"`
			Rw          bool    `json:"rw"`
			Source      *string `json:"source,omitempty"`
			Type        string  `json:"type"`
		}{},
		Networks: []struct {
			IpAddress *string `json:"ipAddress,omitempty"`
			Name      string  `json:"name"`
		}{},
	}
	if inspect.Config != nil {
		detail.Env = inspect.Config.Env
		detail.Labels = inspect.Config.Labels
		detail.Cmd = strings.Join(inspect.Config.Cmd, " ")
		if len(inspect.Config.Entrypoint) > 0 {
			entry := strings.Join(inspect.Config.Entrypoint, " ")
			detail.Entrypoint = &entry
		}
	}
	for _, mount := range inspect.Mounts {
		source := mount.Source
		entry := struct {
			Destination string  `json:"destination"`
			Rw          bool    `json:"rw"`
			Source      *string `json:"source,omitempty"`
			Type        string  `json:"type"`
		}{Destination: mount.Destination, Rw: mount.RW, Type: string(mount.Type)}
		if mount.Type == "volume" {
			source = mount.Name
		}
		if source != "" {
			entry.Source = &source
		}
		detail.Mounts = append(detail.Mounts, entry)
	}
	if inspect.NetworkSettings != nil {
		for netName, endpoint := range inspect.NetworkSettings.Networks {
			entry := struct {
				IpAddress *string `json:"ipAddress,omitempty"`
				Name      string  `json:"name"`
			}{Name: netName}
			if endpoint != nil && endpoint.IPAddress != "" {
				ip := endpoint.IPAddress
				entry.IpAddress = &ip
			}
			detail.Networks = append(detail.Networks, entry)
		}
	}
	if inspect.HostConfig != nil {
		policy := string(inspect.HostConfig.RestartPolicy.Name)
		if policy != "" {
			detail.RestartPolicy = &policy
		}
	}
	if inspect.State != nil {
		if started, perr := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); perr == nil && !started.IsZero() && started.Year() > 1 {
			detail.StartedAt = &started
		}
		if !inspect.State.Running {
			exit := inspect.State.ExitCode
			detail.ExitCode = &exit
		}
	}
	return DockerGetContainer200JSONResponse(detail), nil
}

func (s *Server) DockerContainerAction(ctx context.Context, req DockerContainerActionRequestObject) (DockerContainerActionResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerContainerAction404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return DockerContainerAction502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	containerRes := containerResource(resource, req.Name)
	action := string(req.Body.Action)
	if !s.can(ctx, id, containerRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "docker.container_"+action, containerRes)
		return DockerContainerAction403JSONResponse{forbidden()}, nil
	}
	item, err := conn.Locate(ctx, req.Name)
	if err != nil {
		return DockerContainerAction404JSONResponse{notFoundErr("container")}, nil
	}

	switch action {
	case "start":
		err = conn.Client.ContainerStart(ctx, item.ID, containertypes.StartOptions{})
	case "stop":
		err = conn.Client.ContainerStop(ctx, item.ID, containertypes.StopOptions{})
	case "restart":
		err = conn.Client.ContainerRestart(ctx, item.ID, containertypes.StopOptions{})
	case "pause":
		err = conn.Client.ContainerPause(ctx, item.ID)
	case "unpause":
		err = conn.Client.ContainerUnpause(ctx, item.ID)
	case "kill":
		err = conn.Client.ContainerKill(ctx, item.ID, "KILL")
	}
	if err != nil {
		s.audit.Log(ctx, actorFor(id), "docker.container_"+action, containerRes, "failure", map[string]any{"error": err.Error()})
		return DockerContainerAction502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.container_"+action, containerRes, "success", nil)
	return DockerContainerAction204Response{}, nil
}

func (s *Server) DockerRemoveContainer(ctx context.Context, req DockerRemoveContainerRequestObject) (DockerRemoveContainerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerRemoveContainer404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return DockerRemoveContainer502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	containerRes := containerResource(resource, req.Name)
	if !s.can(ctx, id, containerRes, rbac.LevelDelete) {
		s.deniedAudit(ctx, id, "docker.container_remove", containerRes)
		return DockerRemoveContainer403JSONResponse{forbidden()}, nil
	}
	item, err := conn.Locate(ctx, req.Name)
	if err != nil {
		return DockerRemoveContainer404JSONResponse{notFoundErr("container")}, nil
	}
	force := req.Params.Force != nil && *req.Params.Force
	volumes := req.Params.Volumes != nil && *req.Params.Volumes
	if err := conn.Client.ContainerRemove(ctx, item.ID, containertypes.RemoveOptions{Force: force, RemoveVolumes: volumes}); err != nil {
		s.audit.Log(ctx, actorFor(id), "docker.container_remove", containerRes, "failure", map[string]any{"error": err.Error()})
		return DockerRemoveContainer502JSONResponse{UpstreamErrorJSONResponse(dockerUpstreamErr(err))}, nil
	}
	s.audit.Log(ctx, actorFor(id), "docker.container_remove", containerRes, "success", map[string]any{"force": force, "volumes": volumes})
	return DockerRemoveContainer204Response{}, nil
}

func (s *Server) DockerRecreateContainer(ctx context.Context, req DockerRecreateContainerRequestObject) (DockerRecreateContainerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	_, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerRecreateContainer404JSONResponse{notFoundErr("integration")}, nil
	}
	containerRes := containerResource(resource, req.Name)
	if !s.can(ctx, id, containerRes, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "docker.container_recreate", containerRes)
		return DockerRecreateContainer403JSONResponse{forbidden()}, nil
	}
	pull := req.Body.Pull == nil || *req.Body.Pull
	job, err := s.jobs.Enqueue(ctx, dockerint.JobRecreate, dockerint.JobPayload{
		IntegrationID: req.Id,
		UserID:        id.User.ID,
		Container:     req.Name,
		Pull:          pull,
	})
	if err != nil {
		return nil, err
	}
	s.audit.Log(ctx, actorFor(id), "docker.container_recreate", containerRes, "success",
		map[string]any{"pull": pull, "job": job.ID})
	return DockerRecreateContainer202JSONResponse{JobQueuedJSONResponse{JobId: job.ID}}, nil
}

func (s *Server) DockerExecContainer(ctx context.Context, req DockerExecContainerRequestObject) (DockerExecContainerResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerExecContainer404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return DockerExecContainer502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	containerRes := containerResource(resource, req.Name)
	// An exec is a shell — and on a host with HomeHub's own container, a shell
	// next to master.key — so it needs the shell level, not control.
	if !s.can(ctx, id, containerRes, rbac.LevelShell) {
		s.deniedAudit(ctx, id, "docker.exec", containerRes)
		return DockerExecContainer403JSONResponse{forbidden()}, nil
	}
	item, err := conn.Locate(ctx, req.Name)
	if err != nil {
		return DockerExecContainer404JSONResponse{notFoundErr("container")}, nil
	}
	command := ""
	if req.Body.Command != nil {
		command = *req.Body.Command
	}
	sessionID := s.term.Create(id.User.ID, term.OpenDockerExec(conn.Client, item.ID, command))
	s.audit.Log(ctx, actorFor(id), "docker.exec", containerRes, "success", map[string]any{"command": command})
	return DockerExecContainer200JSONResponse(TermSession{SessionId: sessionID, WsPath: "/api/term/" + sessionID}), nil
}

func (s *Server) DockerContainerLogs(ctx context.Context, req DockerContainerLogsRequestObject) (DockerContainerLogsResponseObject, error) {
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		return nil, ErrUnauthenticated
	}
	conn, resource, err := s.dockerConn(ctx, id, req.Id)
	if errors.Is(err, store.ErrNotFound) {
		return DockerContainerLogs404JSONResponse{notFoundErr("integration")}, nil
	}
	if err != nil {
		return DockerContainerLogs502JSONResponse{UpstreamErrorJSONResponse(errBody("unreachable", err.Error()))}, nil
	}
	containerRes := containerResource(resource, req.Name)
	if !s.can(ctx, id, containerRes, rbac.LevelView) {
		return DockerContainerLogs403JSONResponse{forbidden()}, nil
	}
	item, err := conn.Locate(ctx, req.Name)
	if err != nil {
		return DockerContainerLogs404JSONResponse{notFoundErr("container")}, nil
	}
	inspect, err := conn.Client.ContainerInspect(ctx, item.ID)
	if err != nil {
		return DockerContainerLogs404JSONResponse{notFoundErr("container")}, nil
	}
	tail := "500"
	if req.Body.Tail != nil {
		tail = fmt.Sprint(*req.Body.Tail)
	}
	tty := inspect.Config != nil && inspect.Config.Tty
	sessionID := s.term.Create(id.User.ID, term.OpenDockerLogs(conn.Client, item.ID, tty, tail))
	return DockerContainerLogs200JSONResponse(TermSession{SessionId: sessionID, WsPath: "/api/term/" + sessionID}), nil
}
