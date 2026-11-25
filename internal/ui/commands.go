package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

// Timeouts of the commands run by the TUI.
const (
	// readTimeout bounds read-only pvesh/apt queries.
	readTimeout = 12 * time.Second
	// shortActionTimeout bounds quick actions (backup start, job update, TFA).
	shortActionTimeout = 30 * time.Second
	// snapshotTimeout bounds snapshot creation, which can take minutes with RAM state.
	snapshotTimeout = 5 * time.Minute
	// guestActionTimeout bounds guest start/stop and deletions.
	guestActionTimeout = 2 * time.Minute
	// longActionTimeout bounds dist-upgrade, restore and rollback.
	longActionTimeout = 30 * time.Minute
	// taskPollInterval is the delay between two status polls of a running task.
	taskPollInterval = 3 * time.Second
	// refreshInterval is the delay between automatic guest list refreshes.
	refreshInterval = 15 * time.Second
)

func (m Model) load() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	var s snapshot
	node, err := m.client.NodeInfo(ctx)
	if err != nil {
		s.err = err
		return loadMsg(s)
	}
	s.node = node

	guests, err := m.client.Guests(ctx)
	if err != nil {
		s.err = err
		return loadMsg(s)
	}
	s.guests = guests

	storages, err := m.client.Storages(ctx)
	if err != nil {
		s.err = err
		return loadMsg(s)
	}
	s.storages = storages

	return loadMsg(s)
}

// loadClusterTasks lists the tasks of the cluster, as the web interface does,
// and reads the log of the failures that are new to the log.
func (m Model) loadClusterTasks() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	tasks, err := m.client.ClusterTasks(ctx)
	if err != nil {
		return clusterTasksMsg{err: err}
	}
	return m.newClusterResults(ctx, tasks)
}

func (m Model) loadUsers() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	users, err := m.client.Users(ctx)
	if err != nil {
		return usersMsg{err: err}
	}
	for i := range users {
		entries, err := m.client.UserTFA(ctx, users[i].UserID)
		if err == nil && len(entries) > 0 {
			users[i].TFAEnabled = true
		}
	}
	return usersMsg{users: users}
}

func (m Model) loadUpdates() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	updates, err := m.client.PackageUpdates(ctx)
	return updatesMsg{updates: updates, err: err}
}

func (m Model) loadBackups() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	jobs, jobsErr := m.client.BackupJobs(ctx)
	volumes, volumesErr := m.client.BackupVolumes(ctx, m.data.storages)
	if volumesErr != nil {
		return backupsMsg{jobs: jobs, err: volumesErr}
	}
	return backupsMsg{jobs: jobs, volumes: volumes, err: jobsErr}
}

func (m Model) loadSnapshots(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		snapshots, err := m.client.Snapshots(ctx, vmid)
		return snapshotsMsg{vmid: vmid, snapshots: snapshots, err: err}
	}
}

func (m Model) createSnapshot(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), snapshotTimeout)
		defer cancel()
		upid, err := m.client.CreateSnapshot(ctx, vmid, name, "Created from pve-console TUI", m.snapshotVMState)
		return createSnapshotMsg{vmid: vmid, name: name, upid: upid, err: err}
	}
}

func (m Model) runBackup(vmid int, storage string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		upid, err := m.client.RunBackup(ctx, vmid, storage)
		return runBackupMsg{vmid: vmid, upid: upid, err: err}
	}
}

func (m Model) updateBackupJob(id string, storage string, schedule string, vmids string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		err := m.client.UpdateBackupJob(ctx, id, storage, schedule, vmids)
		return updateBackupScheduleMsg{id: id, storage: storage, schedule: schedule, vmids: vmids, err: err}
	}
}

func (m Model) startGuest(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), guestActionTimeout)
		defer cancel()
		upid, err := m.client.StartGuest(ctx, vmid)
		return destructiveActionMsg{action: "start-guest", vmid: vmid, name: fmt.Sprintf("%d", vmid), upid: upid, err: err}
	}
}

func (m Model) stopGuest(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), guestActionTimeout)
		defer cancel()
		upid, err := m.client.StopGuest(ctx, vmid)
		return destructiveActionMsg{action: "stop-guest", vmid: vmid, name: fmt.Sprintf("%d", vmid), upid: upid, err: err}
	}
}

func (m Model) rebootGuest(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), guestActionTimeout)
		defer cancel()
		upid, err := m.client.RebootGuest(ctx, vmid)
		return destructiveActionMsg{action: "reboot-guest", vmid: vmid, name: fmt.Sprintf("%d", vmid), upid: upid, err: err}
	}
}

func (m Model) shutdownGuest(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), guestActionTimeout)
		defer cancel()
		upid, err := m.client.ShutdownGuest(ctx, vmid)
		return destructiveActionMsg{action: "shutdown-guest", vmid: vmid, name: fmt.Sprintf("%d", vmid), upid: upid, err: err}
	}
}

func (m Model) deleteBackupVolume(volid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), guestActionTimeout)
		defer cancel()
		upid, err := m.client.DeleteBackupVolume(ctx, volid)
		return destructiveActionMsg{action: "delete-backup", name: volid, upid: upid, err: err}
	}
}

func (m Model) restoreBackupVolume(volume proxmox.BackupVolume) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), longActionTimeout)
		defer cancel()
		err := m.client.RestoreBackupVolume(ctx, volume)
		return destructiveActionMsg{action: "restore-backup", vmid: volume.VMID, name: volume.VolID, err: err}
	}
}

func (m Model) deleteSnapshot(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), guestActionTimeout)
		defer cancel()
		upid, err := m.client.DeleteSnapshot(ctx, vmid, name)
		return destructiveActionMsg{action: "delete-snapshot", vmid: vmid, name: name, upid: upid, err: err}
	}
}

func (m Model) rollbackSnapshot(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), longActionTimeout)
		defer cancel()
		upid, err := m.client.RollbackSnapshot(ctx, vmid, name)
		return destructiveActionMsg{action: "restore-snapshot", vmid: vmid, name: name, upid: upid, err: err}
	}
}

func (m Model) loadUserTFA(userid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		entries, err := m.client.UserTFA(ctx, userid)
		return userTFAMsg{userid: userid, entries: entries, err: err}
	}
}

func (m Model) disableUserTFA(userid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		err := m.client.DisableUserTFA(ctx, userid)
		return disableTFAMsg{userid: userid, err: err}
	}
}

// loadAgent reads whether the guest agent is enabled, to ask before toggling it.
func (m Model) loadAgent(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		enabled, err := m.client.GuestAgent(ctx, vmid)
		return agentStateMsg{vmid: vmid, name: name, enabled: enabled, err: err}
	}
}

func (m Model) setAgent(vmid int, enable bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
		defer cancel()
		err := m.client.SetGuestAgent(ctx, vmid, enable)
		return agentSetMsg{vmid: vmid, enable: enable, err: err}
	}
}

func (m Model) loadConfig(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		config, err := m.client.GuestConfig(ctx, vmid)
		return configMsg{vmid: vmid, config: config, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// spinTick schedules the next redraw of the spinners: fast while something runs,
// slow otherwise (an idle screen does not change, so nothing is written).
func spinTick(busy bool) tea.Cmd {
	interval := time.Second
	if busy {
		interval = 100 * time.Millisecond
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return spinMsg{} })
}

// taskTick schedules the next status poll of a task.
func taskTick(upid string) tea.Cmd {
	return tea.Tick(taskPollInterval, func(time.Time) tea.Msg {
		return taskPollMsg{upid: upid}
	})
}

// pollTask reads the current state of a task.
func (m Model) pollTask(upid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		task, err := m.client.TaskStatus(ctx, upid)
		msg := taskResultMsg{upid: upid, task: task, err: err}
		if err == nil && !task.Running() && !task.Succeeded() {
			// Why it failed is in the task log: read its end, best effort.
			if lines, logErr := m.client.TaskLog(ctx, upid, 10); logErr == nil {
				msg.log = lines
			}
		}
		return msg
	}
}
