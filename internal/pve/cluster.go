package pve

import (
	"context"
	"net/http"
	"net/url"
)

// Version returns the API server's version information.
func (c *Client) Version(ctx context.Context) (VersionInfo, error) {
	var v VersionInfo
	err := c.get(ctx, "/version", nil, &v)
	return v, err
}

// ClusterStatus returns the cluster quorum row plus one row per node.
func (c *Client) ClusterStatus(ctx context.Context) ([]ClusterStatusEntry, error) {
	var entries []ClusterStatusEntry
	err := c.get(ctx, "/cluster/status", nil, &entries)
	return entries, err
}

// ClusterResources returns all nodes, guests, and storages in the cluster.
func (c *Client) ClusterResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource
	err := c.get(ctx, "/cluster/resources", nil, &resources)
	return resources, err
}

// BackupJobs lists the cluster's scheduled vzdump backup jobs.
func (c *Client) BackupJobs(ctx context.Context) ([]BackupJob, error) {
	var jobs []BackupJob
	err := c.get(ctx, "/cluster/backup", nil, &jobs)
	return jobs, err
}

// CreateBackupJob creates a scheduled vzdump backup job.
func (c *Client) CreateBackupJob(ctx context.Context, job BackupJobSpec) error {
	return c.do(ctx, http.MethodPost, "/cluster/backup", nil, job.form(), nil)
}

// UpdateBackupJob updates the scheduled vzdump backup job with the given id.
func (c *Client) UpdateBackupJob(ctx context.Context, id string, job BackupJobSpec) error {
	return c.do(ctx, http.MethodPut, joinPath("cluster", "backup", id), nil, job.form(), nil)
}

func (s BackupJobSpec) form() url.Values {
	v := url.Values{}
	set := func(key, val string) {
		if val != "" {
			v.Set(key, val)
		}
	}
	set("schedule", s.Schedule)
	set("storage", s.Storage)
	set("node", s.Node)
	set("mode", s.Mode)
	set("compress", s.Compress)
	set("comment", s.Comment)
	set("mailto", s.MailTo)
	set("prune-backups", s.Prune)
	if s.All {
		v.Set("all", "1")
	} else {
		set("vmid", s.VMID)
	}
	v.Set("enabled", formBool(s.Enabled))
	return v
}
