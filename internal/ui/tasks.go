package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// maxTaskErrors is the number of consecutive failed polls after which a task is given up.
const maxTaskErrors = 3

// refreshKind says which data to reload once a task has finished.
type refreshKind string

const (
	refreshGuests    refreshKind = "guests"
	refreshBackups   refreshKind = "backups"
	refreshSnapshots refreshKind = "snapshots"
)

// trackedTask is a running Proxmox task followed by the TUI.
type trackedTask struct {
	upid    string
	label   string
	refresh refreshKind
	vmid    int
	errors  int
}

// track returns the model with a new task to follow. The task list is copied,
// never modified in place, because Model is passed by value.
func (m Model) track(upid string, label string, refresh refreshKind, vmid int) Model {
	if m.taskIndex(upid) >= 0 {
		return m
	}
	tasks := make([]trackedTask, 0, len(m.tasks)+1)
	tasks = append(tasks, m.tasks...)
	m.tasks = append(tasks, trackedTask{upid: upid, label: label, refresh: refresh, vmid: vmid})
	return m
}

// untrack returns the model without the given task.
func (m Model) untrack(upid string) Model {
	tasks := make([]trackedTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task.upid != upid {
			tasks = append(tasks, task)
		}
	}
	m.tasks = tasks
	return m
}

// withTask returns the model with the task at index i replaced.
func (m Model) withTask(i int, task trackedTask) Model {
	tasks := make([]trackedTask, len(m.tasks))
	copy(tasks, m.tasks)
	tasks[i] = task
	m.tasks = tasks
	return m
}

func (m Model) taskIndex(upid string) int {
	for i, task := range m.tasks {
		if task.upid == upid {
			return i
		}
	}
	return -1
}

// refreshCmd reloads the data a finished task may have changed.
func (m Model) refreshCmd(kind refreshKind, vmid int) tea.Cmd {
	switch kind {
	case refreshBackups:
		return m.loadBackups
	case refreshSnapshots:
		if vmid > 0 {
			return m.loadSnapshots(vmid)
		}
	case refreshGuests:
		return m.load
	}
	return nil
}

// handleTaskResult applies the polled state of a tracked task.
func (m Model) handleTaskResult(msg taskResultMsg) (Model, tea.Cmd) {
	i := m.taskIndex(msg.upid)
	if i < 0 {
		return m, nil
	}
	task := m.tasks[i]

	if msg.err != nil {
		task.errors++
		if task.errors >= maxTaskErrors {
			m = m.untrack(msg.upid)
			source, _ := classify(msg.err)
			detail := append([]string{"task: " + task.upid}, errorDetails(msg.err)...)
			m = m.logProblem(source, "task not followed", "follow "+task.label, oneLine(msg.err.Error()), detail)
			m.message = "lost track of " + task.label + ": " + oneLine(msg.err.Error())
			// The task probably ended: reload, so the screen does not stay stale.
			return m, m.refreshCmd(task.refresh, task.vmid)
		}
		return m.withTask(i, task), taskTick(msg.upid)
	}

	if msg.task.Running() {
		task.errors = 0
		return m.withTask(i, task), taskTick(msg.upid)
	}

	m = m.untrack(msg.upid)
	// The cluster task list will show this task too: it is already handled here.
	m = m.markTaskSeen(msg.upid)
	if msg.task.Succeeded() {
		m.message = task.label + ": OK"
		m = m.logOK(clock(), sourceProxmox, "task ok", task.label, taskOutcome(msg.task), taskDetails(task.upid, msg.task, nil))
	} else {
		m = m.logProblem(sourceProxmox, "task failed", task.label, "FAILED: "+oneLine(msg.task.ExitStatus), taskDetails(task.upid, msg.task, msg.log))
		m.message = task.label + ": FAILED: " + oneLine(msg.task.ExitStatus)
	}
	return m, m.refreshCmd(task.refresh, task.vmid)
}

// destructiveRefresh tells what to reload when the task of an action ends.
func destructiveRefresh(msg destructiveActionMsg) refreshKind {
	switch {
	case strings.Contains(msg.action, "backup"):
		return refreshBackups
	case strings.Contains(msg.action, "guest"):
		return refreshGuests
	}
	return refreshSnapshots
}

// taskOutcome is what the log says of a task that succeeded: "OK", or the
// warnings.
func taskOutcome(task proxmox.Task) string {
	if task.ExitStatus == "" {
		return "OK"
	}
	return oneLine(task.ExitStatus)
}

// taskDetails describes a Proxmox task that ended, with its log if it failed.
func taskDetails(upid string, task proxmox.Task, log []string) []string {
	lines := []string{"task: " + upid}
	// One line for what identifies the task, so the log stays within reach
	// of the detail pane.
	var about []string
	if task.Type != "" {
		about = append(about, "type: "+task.Type)
	}
	if task.Node != "" {
		about = append(about, "node: "+task.Node)
	}
	if task.User != "" {
		about = append(about, "user: "+task.User)
	}
	if len(about) > 0 {
		lines = append(lines, strings.Join(about, ", "))
	}
	if task.ExitStatus != "" {
		lines = append(lines, "exit status: "+oneLine(task.ExitStatus))
	}
	if len(log) > 0 {
		lines = append(lines, "task log:")
		for _, line := range log {
			lines = append(lines, "  "+strings.TrimRight(line, "\r "))
		}
	}
	return lines
}
