package proxmox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pvc/internal/validate"
)

// Task is a Proxmox task, identified by its UPID.
type Task struct {
	UPID       string `json:"upid"`
	Node       string `json:"node,omitempty"`
	Type       string `json:"type,omitempty"`
	ID         string `json:"id,omitempty"`
	User       string `json:"user,omitempty"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus,omitempty"`
	StartTime  int64  `json:"starttime,omitempty"`
	EndTime    int64  `json:"endtime,omitempty"`
}

// Running reports whether the task has not finished yet.
func (t Task) Running() bool {
	return t.Status == "running"
}

// Succeeded reports whether the task finished with "OK" or with warnings.
func (t Task) Succeeded() bool {
	return t.ExitStatus == "OK" || strings.HasPrefix(t.ExitStatus, "WARNINGS")
}

// Failed reports whether the task finished with an error. A finished task
// without any exit status is not counted: nothing says it went wrong.
func (t Task) Failed() bool {
	return !t.Running() && t.ExitStatus != "" && !t.Succeeded()
}

// clusterTask is one row of /cluster/tasks, the list of the web interface's
// task panel. A running task has no endtime; for a finished one, status is the
// exit status ("OK", "WARNINGS: 1" or the error).
type clusterTask struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	User      string `json:"user"`
	Status    string `json:"status"`
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime"`
}

// ClusterTasks returns the recent tasks of every node, the ones the web
// interface shows in its task panel: those started by the web interface, by
// schedules (backup jobs, package index updates) and by this program.
func (c Client) ClusterTasks(ctx context.Context) ([]Task, error) {
	var rows []clusterTask
	if err := c.pveshJSON(ctx, &rows, "get", "/cluster/tasks", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to list cluster tasks: %w", err)
	}

	tasks := make([]Task, 0, len(rows))
	for _, row := range rows {
		// A row without a valid UPID cannot be followed or read: skip it.
		if validate.UPID(row.UPID) != nil {
			continue
		}
		task := Task{
			UPID: row.UPID, Node: row.Node, Type: row.Type, ID: row.ID, User: row.User,
			StartTime: row.StartTime, EndTime: row.EndTime, Status: "running",
		}
		if task.Node == "" {
			task.Node = upidNode(row.UPID)
		}
		if row.EndTime > 0 {
			task.Status = "stopped"
			task.ExitStatus = row.Status
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// parseUPID extracts the task id returned by an asynchronous pvesh call. An
// empty result means the call did not return a task (it was synchronous).
func parseUPID(stdout []byte) string {
	upid := strings.Trim(strings.TrimSpace(string(stdout)), `"`)
	if validate.UPID(upid) != nil {
		return ""
	}
	return upid
}

// upidNode returns the node that runs the task; it is the second field of the UPID.
func upidNode(upid string) string {
	parts := strings.SplitN(upid, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// TaskStatus returns the current state of a task.
func (c Client) TaskStatus(ctx context.Context, upid string) (Task, error) {
	if err := validate.UPID(upid); err != nil {
		return Task{}, err
	}

	var task Task
	path := fmt.Sprintf("/nodes/%s/tasks/%s/status", upidNode(upid), upid)
	if err := c.pveshJSON(ctx, &task, "get", path, "--output-format", "json"); err != nil {
		return Task{}, fmt.Errorf("unable to read task %s: %w", upid, err)
	}
	task.UPID = upid
	if task.Node == "" {
		task.Node = upidNode(upid)
	}
	return task, nil
}

// TaskLog returns the last tail lines of the log of a task.
func (c Client) TaskLog(ctx context.Context, upid string, tail int) ([]string, error) {
	if err := validate.UPID(upid); err != nil {
		return nil, err
	}

	var entries []struct {
		Text string `json:"t"`
	}
	path := fmt.Sprintf("/nodes/%s/tasks/%s/log", upidNode(upid), upid)
	if err := c.pveshJSON(ctx, &entries, "get", path, "-limit", "500", "--output-format", "json"); err != nil {
		return nil, fmt.Errorf("unable to read log of task %s: %w", upid, err)
	}

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.Text)
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return lines, nil
}

// WaitTask polls a task every interval until it stops or ctx ends. onUpdate, if
// set, is called after every poll. When ctx ends first, the last known state is
// returned together with an error.
func (c Client) WaitTask(ctx context.Context, upid string, interval time.Duration, onUpdate func(Task)) (Task, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		task, err := c.TaskStatus(ctx, upid)
		if err != nil {
			return task, err
		}
		if onUpdate != nil {
			onUpdate(task)
		}
		if !task.Running() {
			return task, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return task, fmt.Errorf("task %s is still running: %w", upid, ctx.Err())
		case <-timer.C:
		}
	}
}
