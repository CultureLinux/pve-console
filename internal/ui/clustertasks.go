package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"pvc/internal/proxmox"
)

const (
	// firstPollWindow is how far back the first cluster task poll looks for
	// results: the cluster list goes back further than what is still useful.
	firstPollWindow = 24 * time.Hour
	// maxTaskLogReads bounds the task logs read by one poll (the newest failures;
	// the log of a task that succeeded is not read).
	maxTaskLogReads = 10
	// taskLogLines is the number of log lines kept for a failed task.
	taskLogLines = 30
	// maxSeenTasks bounds the set of finished tasks remembered.
	maxSeenTasks = 2000
)

// clusterTaskState remembers what the cluster task poll has already handled.
type clusterTaskState struct {
	// loading is true while a poll runs: polls never overlap.
	loading bool
	// primed is true once a poll succeeded (the 24 h window only applies before).
	primed bool
	// seen holds the UPIDs of the finished tasks already handled. It is
	// replaced, never changed in place, because Model is passed by value.
	seen map[string]struct{}
}

func (s clusterTaskState) has(upid string) bool {
	_, ok := s.seen[upid]
	return ok
}

// clusterResult is a finished task of the cluster; the log is read for failures only.
type clusterResult struct {
	task proxmox.Task
	log  []string
}

// markTaskSeen records a task as handled, so the cluster poll does not report
// it a second time (a task followed by the TUI is reported when it ends).
func (m Model) markTaskSeen(upid string) Model {
	seen := make(map[string]struct{}, len(m.clusterTasks.seen)+1)
	for known := range m.clusterTasks.seen {
		seen[known] = struct{}{}
	}
	seen[upid] = struct{}{}
	m.clusterTasks.seen = seen
	return m
}

// newClusterResults picks, among the tasks of the cluster, the finished ones
// the log does not know yet, and reads the log of the failures. Tasks followed
// by the TUI are left to handleTaskResult.
func (m Model) newClusterResults(ctx context.Context, tasks []proxmox.Task) clusterTasksMsg {
	var msg clusterTasksMsg
	var fresh []proxmox.Task
	cutoff := clock().Add(-firstPollWindow).Unix()

	for _, task := range tasks {
		if task.Running() || m.taskIndex(task.UPID) >= 0 {
			continue
		}
		msg.finished = append(msg.finished, task.UPID)
		if m.clusterTasks.has(task.UPID) {
			continue
		}
		// A task that finished without any exit status says nothing worth logging.
		if task.ExitStatus == "" {
			continue
		}
		if !m.clusterTasks.primed && task.EndTime < cutoff {
			continue
		}
		fresh = append(fresh, task)
	}
	sort.SliceStable(fresh, func(i, j int) bool { return fresh[i].EndTime < fresh[j].EndTime })

	failures := 0
	for _, task := range fresh {
		if task.Failed() {
			failures++
		}
	}
	for _, task := range fresh {
		result := clusterResult{task: task}
		if task.Failed() {
			failures--
			// The log only completes the entry: without it, the failure is still logged.
			if failures < maxTaskLogReads {
				if lines, err := m.client.TaskLog(ctx, task.UPID, taskLogLines); err == nil {
					result.log = lines
				}
			}
		}
		msg.results = append(msg.results, result)
	}
	return msg
}

// handleClusterTasks logs the new results of the cluster, quietly: they must
// not replace the result of what the user just did in the status row.
func (m Model) handleClusterTasks(msg clusterTasksMsg) Model {
	m.clusterTasks.loading = false
	if msg.err != nil {
		return m.logErrorQuiet("read cluster tasks", msg.err)
	}

	for _, result := range msg.results {
		task := result.task
		// The task may have been handled since the poll started.
		if m.clusterTasks.has(task.UPID) || m.taskIndex(task.UPID) >= 0 {
			continue
		}
		at := time.Unix(task.EndTime, 0)
		details := taskDetails(task.UPID, task, result.log)
		if task.Failed() {
			message := m.message
			m = m.logProblemAt(at, sourceProxmox, "task failed", clusterTaskContext(task), "FAILED: "+oneLine(task.ExitStatus), details)
			m.message = message
		} else {
			m = m.logOK(at, sourceProxmox, "task ok", clusterTaskContext(task), taskOutcome(task), details)
		}
	}

	seen := make(map[string]struct{}, len(m.clusterTasks.seen)+len(msg.finished))
	if len(m.clusterTasks.seen)+len(msg.finished) <= maxSeenTasks {
		for upid := range m.clusterTasks.seen {
			seen[upid] = struct{}{}
		}
	}
	for _, upid := range msg.finished {
		seen[upid] = struct{}{}
	}
	m.clusterTasks.seen = seen
	m.clusterTasks.primed = true
	return m
}

// clusterTaskContext names a task for the journal, e.g. "task vzdump 103".
func clusterTaskContext(task proxmox.Task) string {
	return strings.TrimSpace("task " + task.Type + " " + task.ID)
}
