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

// agentStateMsg is the answer to the read of the guest agent option of a VM.
type agentStateMsg struct {
	vmid    int
	name    string
	enabled bool
	err     error
}

// agentSetMsg is the result of turning the guest agent option on or off.
type agentSetMsg struct {
	vmid   int
	enable bool
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
	// tokens are the API tokens of these users, shown in their detail.
	tokens tokensMsg
	err    error
}

// tokensMsg is the answer to the read of the API tokens and their permissions.
// A partial read comes with an error: what was read is kept.
type tokensMsg struct {
	tokens []proxmox.Token
	acls   []proxmox.ACLEntry
	// perms holds the privileges of each token, by full token id then path.
	perms map[string]map[string][]string
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
