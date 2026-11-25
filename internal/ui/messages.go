package ui

import (
	"time"

	"pvc/internal/proxmox"
)

type loadMsg snapshot

type tickMsg time.Time

type snapshotsMsg struct {
	vmid      int
	snapshots []proxmox.Snapshot
	err       error
}

type createSnapshotMsg struct {
	vmid int
	name string
	upid string
	err  error
}

type runBackupMsg struct {
	vmid int
	upid string
	err  error
}

type updateBackupScheduleMsg struct {
	id       string
	storage  string
	schedule string
	vmids    string
	err      error
}

type configMsg struct {
	vmid   int
	config proxmox.GuestConfig
	err    error
}

type userTFAMsg struct {
	userid  string
	entries []proxmox.TFAEntry
	err     error
}

type disableTFAMsg struct {
	userid string
	err    error
}

type usersMsg struct {
	users []proxmox.User
	err   error
}

type updatesMsg struct {
	updates []proxmox.PackageUpdate
	err     error
}

type backupsMsg struct {
	jobs    []proxmox.BackupJob
	volumes []proxmox.BackupVolume
	err     error
}

type destructiveActionMsg struct {
	action string
	vmid   int
	name   string
	upid   string
	err    error
}

// spinMsg redraws the screen so the spinners move.
type spinMsg struct{}

// taskPollMsg asks for a new status poll of a tracked task.
type taskPollMsg struct {
	upid string
}

// taskResultMsg carries the polled state of a tracked task.
type taskResultMsg struct {
	upid string
	task proxmox.Task
	err  error
	// log holds the last lines of the task log, read when the task failed.
	log []string
}

// clusterTasksMsg carries the finished tasks of the cluster that the log has
// not seen yet, with the UPIDs of every finished task.
type clusterTasksMsg struct {
	results []clusterResult
	// finished lists the UPIDs of every finished task of the cluster.
	finished []string
	err      error
}
