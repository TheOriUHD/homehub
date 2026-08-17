package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/google/uuid"
)

// Job kinds handled by this integration.
const (
	JobPull     = "docker.pull"
	JobRecreate = "docker.recreate"
	JobRedeploy = "docker.redeploy"
)

// JobPayload parameterizes every docker job kind.
type JobPayload struct {
	IntegrationID uuid.UUID `json:"integrationId"`
	UserID        uuid.UUID `json:"userId"`
	Reference     string    `json:"reference,omitempty"`
	Container     string    `json:"container,omitempty"`
	Project       string    `json:"project,omitempty"`
	Pull          bool      `json:"pull"`
}

// JobDeps wires docker jobs into the platform without import cycles.
type JobDeps struct {
	// Conn resolves a live docker connection for an instance.
	Conn func(id uuid.UUID) (*Conn, error)
	// RegistryAuth returns the base64 auth blob for a reference, or "".
	RegistryAuth func(ctx context.Context, instID uuid.UUID, reference string) string
	// Notify delivers the job outcome to the requesting user.
	Notify func(ctx context.Context, userID uuid.UUID, severity, title, body, resource string)
}

// RegisterJobs binds the docker job kinds to a job engine.
func RegisterJobs(register func(kind string, fn func(ctx context.Context, payload json.RawMessage) error), deps JobDeps) {
	run := func(fn func(ctx context.Context, p JobPayload, conn *Conn) (string, error)) func(ctx context.Context, payload json.RawMessage) error {
		return func(ctx context.Context, payload json.RawMessage) error {
			var p JobPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return err
			}
			conn, err := deps.Conn(p.IntegrationID)
			if err != nil {
				deps.Notify(ctx, p.UserID, "error", "Docker job failed", err.Error(), "")
				return err
			}
			title, err := fn(ctx, p, conn)
			resource := TypeID + "/" + p.IntegrationID.String()
			if err != nil {
				deps.Notify(ctx, p.UserID, "error", title+" failed", FriendlyError(err), resource)
				return err
			}
			deps.Notify(ctx, p.UserID, "success", title, "", resource)
			return nil
		}
	}

	register(JobPull, run(func(ctx context.Context, p JobPayload, conn *Conn) (string, error) {
		title := "Pulled " + p.Reference
		return title, pullImage(ctx, conn, p.Reference, deps.RegistryAuth(ctx, p.IntegrationID, p.Reference))
	}))

	register(JobRecreate, run(func(ctx context.Context, p JobPayload, conn *Conn) (string, error) {
		title := "Recreated " + p.Container
		item, err := conn.Locate(ctx, p.Container)
		if err != nil {
			return title, err
		}
		auth := deps.RegistryAuth(ctx, p.IntegrationID, item.Image)
		return title, recreateContainer(ctx, conn, item.ID, p.Pull, auth)
	}))

	register(JobRedeploy, run(func(ctx context.Context, p JobPayload, conn *Conn) (string, error) {
		title := "Redeployed stack " + p.Project
		list, err := conn.Client.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			return title, err
		}
		var failures []string
		count := 0
		for _, item := range list {
			if item.Labels["com.docker.compose.project"] != p.Project {
				continue
			}
			count++
			auth := deps.RegistryAuth(ctx, p.IntegrationID, item.Image)
			if err := recreateContainer(ctx, conn, item.ID, p.Pull, auth); err != nil {
				failures = append(failures, ResourceName(item.Names, item.ID)+": "+err.Error())
			}
		}
		if count == 0 {
			return title, fmt.Errorf("no containers found for project %q", p.Project)
		}
		if len(failures) > 0 {
			return title, fmt.Errorf("%d of %d containers failed: %s", len(failures), count, strings.Join(failures, "; "))
		}
		return title, nil
	}))
}

func pullImage(ctx context.Context, conn *Conn, reference, auth string) error {
	reader, err := conn.Client.ImagePull(ctx, reference, image.PullOptions{RegistryAuth: auth})
	if err != nil {
		return err
	}
	defer reader.Close()
	// Drain the progress stream; the pull completes when it ends.
	_, err = io.Copy(io.Discard, reader)
	return err
}

// recreateContainer is the API-only redeploy: optionally pull, then swap the
// container for a fresh one with identical configuration. The old container
// is kept under a temporary name until the replacement starts, so a failure
// rolls back instead of leaving nothing running.
func recreateContainer(ctx context.Context, conn *Conn, containerID string, pull bool, auth string) error {
	cli := conn.Client
	inspect, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(inspect.Name, "/")

	if pull && inspect.Config != nil && inspect.Config.Image != "" {
		if err := pullImage(ctx, conn, inspect.Config.Image, auth); err != nil {
			return fmt.Errorf("pull %s: %w", inspect.Config.Image, err)
		}
	}

	stopTimeout := 15
	wasRunning := inspect.State != nil && inspect.State.Running
	if wasRunning {
		if err := cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
	}

	tempName := fmt.Sprintf("%s-old-%d", name, time.Now().Unix())
	if err := cli.ContainerRename(ctx, containerID, tempName); err != nil {
		return fmt.Errorf("rename old container: %w", err)
	}

	rollback := func() {
		_ = cli.ContainerRename(ctx, containerID, name)
		if wasRunning {
			_ = cli.ContainerStart(ctx, containerID, container.StartOptions{})
		}
	}

	networking := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{}}
	if inspect.NetworkSettings != nil {
		for netName, endpoint := range inspect.NetworkSettings.Networks {
			networking.EndpointsConfig[netName] = endpoint
		}
	}

	created, err := cli.ContainerCreate(ctx, inspect.Config, inspect.HostConfig, networking, nil, name)
	if err != nil {
		rollback()
		return fmt.Errorf("create replacement: %w", err)
	}
	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, created.ID, container.RemoveOptions{Force: true})
		rollback()
		return fmt.Errorf("start replacement: %w", err)
	}
	if err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("replacement running, but removing the old container failed: %w", err)
	}
	return nil
}

// EncodeRegistryAuth builds the X-Registry-Auth blob from stored credentials.
func EncodeRegistryAuth(username, secret, serverAddress string) string {
	raw, err := json.Marshal(registry.AuthConfig{
		Username:      username,
		Password:      secret,
		ServerAddress: serverAddress,
	})
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(raw)
}

// RegistryHost extracts the registry host from an image reference
// ("ghcr.io/owner/app:tag" → "ghcr.io"; bare references → Docker Hub).
func RegistryHost(reference string) string {
	first, _, found := strings.Cut(reference, "/")
	if found && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost") {
		return first
	}
	return "docker.io"
}
