package pve

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// NodeStatus returns load, memory, and version details for one node.
func (c *Client) NodeStatus(ctx context.Context, node string) (NodeStatus, error) {
	var st NodeStatus
	err := c.get(ctx, joinPath("nodes", node, "status"), nil, &st)
	return st, err
}

// NodeStorage lists the storages visible on a node with usage figures.
func (c *Client) NodeStorage(ctx context.Context, node string) ([]Storage, error) {
	var storages []Storage
	err := c.get(ctx, joinPath("nodes", node, "storage"), nil, &storages)
	return storages, err
}

// NodeTasks lists recent tasks on a node, newest first. limit <= 0 uses the
// server default.
func (c *Client) NodeTasks(ctx context.Context, node string, limit int) ([]Task, error) {
	var q url.Values
	if limit > 0 {
		q = url.Values{"limit": {strconv.Itoa(limit)}}
	}
	var tasks []Task
	err := c.get(ctx, joinPath("nodes", node, "tasks"), q, &tasks)
	return tasks, err
}

// TaskStatus returns the current state of a task by UPID.
func (c *Client) TaskStatus(ctx context.Context, node, upid string) (TaskStatus, error) {
	var st TaskStatus
	err := c.get(ctx, joinPath("nodes", node, "tasks", upid, "status"), nil, &st)
	return st, err
}

// TaskLog returns task output lines starting at the given 0-based offset.
func (c *Client) TaskLog(ctx context.Context, node, upid string, start int) ([]TaskLogLine, error) {
	q := url.Values{"start": {strconv.Itoa(start)}}
	var lines []TaskLogLine
	err := c.get(ctx, joinPath("nodes", node, "tasks", upid, "log"), q, &lines)
	return lines, err
}

// WaitTask polls a task every 500ms until it stops or the timeout elapses
// (timeout <= 0 waits until ctx is done). A task that stopped with a failing
// ExitStatus returns a nil error — check TaskStatus.OK.
func (c *Client) WaitTask(ctx context.Context, node, upid string, timeout time.Duration) (TaskStatus, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		st, err := c.TaskStatus(ctx, node, upid)
		if err != nil {
			return TaskStatus{}, err
		}
		if st.Finished() {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return st, fmt.Errorf("pve: waiting for task %s: %w", upid, ctx.Err())
		case <-ticker.C:
		}
	}
}
