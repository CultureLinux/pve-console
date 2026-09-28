package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pvc/internal/proxmox"
	"pvc/internal/validate"
	"pvc/internal/version"
)

type snapshot struct {
	node          proxmox.NodeInfo
	guests        []proxmox.Guest
	storages      []proxmox.Storage
	users         []proxmox.User
	updates       []proxmox.PackageUpdate
	backups       []proxmox.BackupJob
	backupVolumes []proxmox.BackupVolume
	updateErr     error
	backupErr     error
	err           error
}

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
	err  error
}
type runBackupMsg struct {
	vmid int
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
type distUpgradeMsg struct {
	output string
	err    error
}
type destructiveActionMsg struct {
	action string
	vmid   int
	name   string
	err    error
}

type inputMode string
type modalMode string

const (
	inputNone       inputMode = ""
	inputSearch     inputMode = "search"
	inputDisableTFA inputMode = "disable-tfa"
	modalNone       modalMode = ""
	modalSnapshot   modalMode = "snapshot"
	modalBackup     modalMode = "backup"
	modalSchedule   modalMode = "schedule"
	modalHelp       modalMode = "help"
	modalStartup    modalMode = "startup"
	modalUpgrade    modalMode = "upgrade"
	modalConfirm    modalMode = "confirm"
	viewGuests      string    = "guests"
	viewUsers       string    = "users"
	viewUpdates     string    = "updates"
	viewBackups     string    = "backups"
)

type Model struct {
	client           proxmox.Client
	width            int
	height           int
	selected         int
	selectedUser     int
	selectedBackup   int
	selectedSnapshot int
	loading          bool
	inputMode        inputMode
	input            string
	modalMode        modalMode
	modalField       int
	modalInput       string
	modalVMIDs       string
	modalStorage     int
	modalGuest       int
	modalTargetVMID  int
	modalJobID       string
	modalAction      string
	modalTitle       string
	modalBody        string
	viewMode         string
	query            string
	filter           string
	guestTypeFilter  string
	sortBy           string
	showSnaps        bool
	showConfig       bool
	backupMode       string
	snapshotVMState  bool
	snapshotVMID     int
	configVMID       int
	snapshots        []proxmox.Snapshot
	snapErr          error
	config           proxmox.GuestConfig
	configErr        error
	tfaUserID        string
	tfaEntries       []proxmox.TFAEntry
	tfaErr           error
	upgradeRunning   bool
	upgradeOutput    string
	upgradeErr       error
	message          string
	data             snapshot
}

func New(client proxmox.Client) Model {
	return NewWithStartup(client, true)
}

func NewWithStartup(client proxmox.Client, showStartup bool) Model {
	model := Model{client: client, loading: true, filter: "all", guestTypeFilter: "all", sortBy: "id", viewMode: viewGuests, snapshotVMState: true, backupMode: "volumes"}
	if showStartup {
		model.modalMode = modalStartup
	}
	return model
}

func Run(client proxmox.Client, showStartup bool) error {
	program := tea.NewProgram(NewWithStartup(client, showStartup), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load, tick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.modalMode != modalNone {
			if m.modalMode == modalStartup {
				switch msg.String() {
				case "esc", "enter":
					m.closeModal()
				}
				return m, nil
			}
			if m.modalMode == modalHelp {
				switch msg.String() {
				case "esc", "enter", "?", "h":
					m.closeModal()
				}
				return m, nil
			}
			if m.modalMode == modalUpgrade {
				switch msg.String() {
				case "esc", "enter":
					if !m.upgradeRunning {
						m.closeModal()
					}
				}
				return m, nil
			}
			if m.modalMode == modalConfirm {
				switch msg.String() {
				case "esc":
					m.closeModal()
				case "enter":
					return m.handleConfirmSubmit()
				}
				return m, nil
			}
			switch msg.String() {
			case "esc":
				m.closeModal()
			case "enter":
				return m.handleModalSubmit()
			case "tab", "right":
				if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 && msg.String() == "right" {
					m.nextModalStorage(1)
				} else {
					m.modalField = (m.modalField + 1) % m.modalFieldCount()
				}
			case "shift+tab":
				m.modalField--
				if m.modalField < 0 {
					m.modalField = m.modalFieldCount() - 1
				}
			case "left":
				if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 {
					m.nextModalStorage(-1)
				} else {
					m.modalField--
					if m.modalField < 0 {
						m.modalField = m.modalFieldCount() - 1
					}
				}
			case "up":
				if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 {
					m.nextModalStorage(-1)
				} else if m.modalMode == modalSchedule && m.modalField == 2 {
					m.nextModalGuest(-1)
				}
			case "down":
				if (m.modalMode == modalBackup || m.modalMode == modalSchedule) && m.modalField == 0 {
					m.nextModalStorage(1)
				} else if m.modalMode == modalSchedule && m.modalField == 2 {
					m.nextModalGuest(1)
				}
			case " ":
				if m.modalMode == modalSchedule && m.modalField == 2 {
					m.toggleModalGuest()
				} else if m.modalMode == modalSchedule && m.modalField == 1 {
					m.modalInput += msg.String()
				} else if m.modalMode != modalBackup && m.modalField == 0 {
					m.modalInput += msg.String()
				}
			case "backspace":
				if ((m.modalMode == modalSchedule && m.modalField == 1) || (m.modalMode != modalBackup && m.modalMode != modalSchedule && m.modalField == 0)) && len(m.modalInput) > 0 {
					m.modalInput = m.modalInput[:len(m.modalInput)-1]
				}
			default:
				if m.modalMode == modalSchedule && m.modalField == 1 && len(msg.String()) == 1 {
					m.modalInput += msg.String()
				} else if m.modalMode != modalBackup && m.modalMode != modalSchedule && m.modalField == 0 && len(msg.String()) == 1 {
					m.modalInput += msg.String()
				}
			}
			return m, nil
		}

		if m.inputMode != inputNone {
			switch msg.String() {
			case "esc":
				m.inputMode = inputNone
				m.input = ""
			case "enter":
				return m.handleInputSubmit()
			case "tab", " ":
				if len(msg.String()) == 1 {
					m.input += msg.String()
				}
			case "backspace":
				if len(m.input) > 0 {
					m.input = m.input[:len(m.input)-1]
				}
			default:
				if len(msg.String()) == 1 {
					m.input += msg.String()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "?", "h":
			m.openModal(modalHelp, 0, "", "")
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.showConfig {
				m.showConfig = false
				m.message = ""
				return m, nil
			}
			if m.showSnaps {
				m.showSnaps = false
				m.message = ""
				return m, nil
			}
			if m.viewMode != viewGuests {
				m.viewMode = viewGuests
				m.message = ""
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			if m.viewMode != viewGuests {
				return m, nil
			}
			guest, ok := m.selectedGuest()
			if ok {
				m.showConfig = true
				m.configVMID = guest.VMID
				m.config = nil
				m.configErr = nil
				m.message = fmt.Sprintf("loading config for %d", guest.VMID)
				return m, m.loadConfig(guest.VMID)
			}
		case "/":
			m.inputMode = inputSearch
			m.input = m.query
			m.message = ""
		case "u":
			m.toggleView()
			if m.viewMode == viewUsers && len(m.data.users) == 0 {
				m.loading = true
				return m, m.loadUsers
			}
		case "g":
			m.viewMode = viewGuests
			m.showConfig = false
			m.message = ""
		case "p":
			m.viewMode = viewUpdates
			m.message = "package updates"
			if len(m.data.updates) == 0 {
				m.loading = true
				return m, m.loadUpdates
			}
		case "b":
			m.viewMode = viewBackups
			m.message = "backup jobs"
			if m.backupItemCount() == 0 {
				m.loading = true
				return m, m.loadBackups
			}
		case "tab":
			if m.viewMode == viewBackups {
				if m.backupMode == "jobs" {
					m.backupMode = "volumes"
				} else {
					m.backupMode = "jobs"
				}
				m.selectedBackup = 0
			} else if m.viewMode == viewGuests {
				m.nextGuestTypeFilter()
				m.selected = 0
			}
		case "f":
			m.nextFilter()
			m.selected = 0
		case "t":
			m.nextSort()
			m.selected = 0
		case "c":
			m.query = ""
			m.resetActiveSelection()
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "a":
			if m.viewMode == viewUpdates && len(m.data.updates) > 0 && !m.upgradeRunning {
				m.modalMode = modalUpgrade
				m.upgradeRunning = true
				m.upgradeOutput = ""
				m.upgradeErr = nil
				m.message = "running apt dist-upgrade"
				return m, m.runDistUpgrade
			}
		case "r":
			m.loading = true
			if m.viewMode == viewUsers {
				return m, m.loadUsers
			}
			if m.viewMode == viewUpdates {
				return m, m.loadUpdates
			}
			if m.viewMode == viewBackups {
				return m, m.loadBackups
			}
			return m, m.load
		case "s":
			if m.viewMode == viewUsers {
				user, ok := m.selectedUserModel()
				if ok {
					m.tfaUserID = user.UserID
					m.tfaEntries = nil
					m.tfaErr = nil
					m.message = fmt.Sprintf("loading TFA for %s", user.UserID)
					return m, m.loadUserTFA(user.UserID)
				}
				return m, nil
			}
			guest, ok := m.selectedGuest()
			if ok {
				m.showSnaps = true
				m.snapshotVMID = guest.VMID
				m.snapshots = nil
				m.selectedSnapshot = 0
				m.snapErr = nil
				m.message = fmt.Sprintf("loading snapshots for %d", guest.VMID)
				return m, m.loadSnapshots(guest.VMID)
			}
		case "n":
			if m.viewMode == viewUsers {
				return m, nil
			}
			guest, ok := m.selectedGuest()
			if ok {
				m.openModal(modalSnapshot, guest.VMID, "", "")
			}
		case "m":
			if m.viewMode == viewUsers || m.viewMode == viewUpdates {
				return m, nil
			}
			vmid := 0
			if m.viewMode == viewBackups {
				vmid = m.selectedBackupVMID()
			} else if guest, ok := m.selectedGuest(); ok {
				vmid = guest.VMID
			}
			if vmid > 0 {
				m.openModal(modalBackup, vmid, "", m.defaultBackupStorage())
			}
		case "e":
			if m.viewMode == viewBackups && m.backupMode == "jobs" {
				job, ok := m.selectedBackupJob()
				if !ok {
					return m, nil
				}
				m.openModal(modalSchedule, 0, job.ID, job.Storage)
				m.modalInput = job.Schedule
				m.modalVMIDs = job.VMID
			}
		case "d":
			if m.viewMode == viewBackups && m.backupMode != "jobs" {
				if volume, ok := m.selectedBackupVolume(); ok {
					m.openConfirm("delete-backup", "DELETE BACKUP", volume.VolID, 0, "Delete backup "+volume.VolID+" ?")
				}
			} else if m.viewMode == viewGuests && m.showSnaps {
				if snap, ok := m.selectedSnapshotModel(); ok && snap.Name != "current" {
					m.openConfirm("delete-snapshot", "DELETE SNAPSHOT", snap.Name, m.snapshotVMID, fmt.Sprintf("Delete snapshot %s for %d ?", snap.Name, m.snapshotVMID))
				}
			}
		case "o":
			if m.viewMode == viewBackups && m.backupMode != "jobs" {
				if volume, ok := m.selectedBackupVolume(); ok {
					m.openConfirm("restore-backup", "RESTORE BACKUP", volume.VolID, volume.VMID, fmt.Sprintf("Restore %s to VMID %d ?", volume.VolID, volume.VMID))
				}
			} else if m.viewMode == viewGuests && m.showSnaps {
				if snap, ok := m.selectedSnapshotModel(); ok && snap.Name != "current" {
					m.openConfirm("restore-snapshot", "RESTORE SNAPSHOT", snap.Name, m.snapshotVMID, fmt.Sprintf("Rollback guest %d to snapshot %s ?", m.snapshotVMID, snap.Name))
				}
			}
		case "x":
			if m.viewMode == viewUsers {
				user, ok := m.selectedUserModel()
				if ok {
					m.inputMode = inputDisableTFA
					m.input = ""
					m.message = fmt.Sprintf("type %s to disable TFA", user.UserID)
				}
			}
		}
	case loadMsg:
		m.loading = false
		next := snapshot(msg)
		next.users = m.data.users
		next.updates = m.data.updates
		next.backups = m.data.backups
		next.backupVolumes = m.data.backupVolumes
		next.updateErr = m.data.updateErr
		next.backupErr = m.data.backupErr
		m.data = next
		m.clampSelections()
	case tickMsg:
		if !m.loading {
			m.loading = true
			return m, tea.Batch(m.load, tick())
		}
		return m, tick()
	case snapshotsMsg:
		if msg.vmid == m.snapshotVMID {
			m.snapshots = msg.snapshots
			m.selectedSnapshot = clampIndex(m.selectedSnapshot, len(msg.snapshots))
			m.snapErr = msg.err
			if msg.err != nil {
				m.message = msg.err.Error()
			} else {
				m.message = fmt.Sprintf("%d snapshots loaded for %d", len(msg.snapshots), msg.vmid)
			}
		}
	case createSnapshotMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = fmt.Sprintf("snapshot %s created for %d", msg.name, msg.vmid)
			m.showSnaps = true
			m.snapshotVMID = msg.vmid
			return m, m.loadSnapshots(msg.vmid)
		}
	case runBackupMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = fmt.Sprintf("backup started for %d", msg.vmid)
			m.viewMode = viewBackups
			return m, m.loadBackups
		}
	case updateBackupScheduleMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = fmt.Sprintf("backup job %s scheduled %s", msg.id, msg.schedule)
			m.viewMode = viewBackups
			m.backupMode = "jobs"
			return m, m.loadBackups
		}
	case configMsg:
		if msg.vmid == m.configVMID {
			m.config = msg.config
			m.configErr = msg.err
			if msg.err != nil {
				m.message = msg.err.Error()
			} else {
				m.message = fmt.Sprintf("config loaded for %d", msg.vmid)
			}
		}
	case userTFAMsg:
		if msg.userid == m.tfaUserID {
			m.tfaEntries = msg.entries
			m.tfaErr = msg.err
			if msg.err != nil {
				m.message = msg.err.Error()
			} else {
				m.message = fmt.Sprintf("%d TFA entries for %s", len(msg.entries), msg.userid)
			}
		}
	case disableTFAMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = fmt.Sprintf("TFA disabled for %s", msg.userid)
			m.tfaUserID = msg.userid
			return m, m.loadUserTFA(msg.userid)
		}
	case usersMsg:
		m.loading = false
		m.data.users = msg.users
		m.clampSelections()
		if msg.err != nil {
			m.message = msg.err.Error()
		}
	case updatesMsg:
		m.loading = false
		m.data.updates = msg.updates
		m.data.updateErr = msg.err
	case backupsMsg:
		m.loading = false
		m.data.backups = msg.jobs
		m.data.backupVolumes = msg.volumes
		m.data.backupErr = msg.err
		m.clampSelections()
	case destructiveActionMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = fmt.Sprintf("%s completed", msg.action)
			if strings.Contains(msg.action, "backup") {
				return m, m.loadBackups
			}
			if msg.vmid > 0 {
				return m, m.loadSnapshots(msg.vmid)
			}
		}
	case distUpgradeMsg:
		m.upgradeRunning = false
		m.upgradeOutput = msg.output
		m.upgradeErr = msg.err
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = "apt dist-upgrade completed"
			return m, m.loadUpdates
		}
	}

	return m, nil
}

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}

	contentWidth := max(60, width-4)
	leftWidth := max(38, contentWidth*62/100)
	rightWidth := max(26, contentWidth-leftWidth-2)

	header := m.header(contentWidth)
	bodyHeight := max(10, height-6)
	left := m.leftPanel(leftWidth, bodyHeight)
	right := m.detailPanel(rightWidth, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	footer := footerStyle.Width(contentWidth).Render(m.footerText())
	base := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.modalMode != modalNone {
		return overlayView(base, m.modal(contentWidth), contentWidth, height)
	}
	if input := m.inputModal(contentWidth); input != "" {
		return overlayView(base, input, contentWidth, height)
	}
	if m.showConfig {
		modalWidth := min(86, max(56, contentWidth-8))
		return overlayView(base, m.configPanel(modalWidth, max(12, height-8)), contentWidth, height)
	}

	return base
}

func (m Model) leftPanel(width int, height int) string {
	if m.viewMode == viewUsers {
		return m.userPanel(width, height)
	}
	if m.viewMode == viewUpdates {
		return m.updatesPanel(width, height)
	}
	if m.viewMode == viewBackups {
		return m.backupsPanel(width, height)
	}
	return m.guestPanel(width, height)
}

func (m Model) load() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
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

func (m Model) loadUsers() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
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
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	updates, err := m.client.PackageUpdates(ctx)
	return updatesMsg{updates: updates, err: err}
}

func (m Model) runDistUpgrade() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	output, err := m.client.DistUpgrade(ctx)
	return distUpgradeMsg{output: output, err: err}
}
func (m Model) loadBackups() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
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
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		snapshots, err := m.client.Snapshots(ctx, vmid)
		return snapshotsMsg{vmid: vmid, snapshots: snapshots, err: err}
	}
}

func (m Model) createSnapshot(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.client.CreateSnapshot(ctx, vmid, name, "Created from pve-console TUI", m.snapshotVMState)
		return createSnapshotMsg{vmid: vmid, name: name, err: err}
	}
}

func (m Model) runBackup(vmid int, storage string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.client.RunBackup(ctx, vmid, storage)
		return runBackupMsg{vmid: vmid, err: err}
	}
}

func (m Model) updateBackupJob(id string, storage string, schedule string, vmids string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.client.UpdateBackupJob(ctx, id, storage, schedule, vmids)
		return updateBackupScheduleMsg{id: id, storage: storage, schedule: schedule, vmids: vmids, err: err}
	}
}

func (m Model) deleteBackupVolume(volid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := m.client.DeleteBackupVolume(ctx, volid)
		return destructiveActionMsg{action: "delete-backup", name: volid, err: err}
	}
}

func (m Model) restoreBackupVolume(volume proxmox.BackupVolume) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := m.client.RestoreBackupVolume(ctx, volume)
		return destructiveActionMsg{action: "restore-backup", vmid: volume.VMID, name: volume.VolID, err: err}
	}
}

func (m Model) deleteSnapshot(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := m.client.DeleteSnapshot(ctx, vmid, name)
		return destructiveActionMsg{action: "delete-snapshot", vmid: vmid, name: name, err: err}
	}
}

func (m Model) rollbackSnapshot(vmid int, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := m.client.RollbackSnapshot(ctx, vmid, name)
		return destructiveActionMsg{action: "restore-snapshot", vmid: vmid, name: name, err: err}
	}
}
func (m Model) loadUserTFA(userid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		entries, err := m.client.UserTFA(ctx, userid)
		return userTFAMsg{userid: userid, entries: entries, err: err}
	}
}

func (m Model) disableUserTFA(userid string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.client.DisableUserTFA(ctx, userid)
		return disableTFAMsg{userid: userid, err: err}
	}
}

func (m *Model) openConfirm(action string, title string, target string, vmid int, body string) {
	m.modalMode = modalConfirm
	m.modalAction = action
	m.modalTitle = title
	m.modalBody = body
	m.modalInput = target
	m.modalTargetVMID = vmid
	m.inputMode = inputNone
	m.input = ""
}
func (m *Model) openModal(mode modalMode, vmid int, jobID string, value string) {
	m.modalMode = mode
	m.modalField = 0
	m.modalInput = value
	m.modalVMIDs = ""
	m.modalStorage = m.storageIndex(value)
	m.modalGuest = 0
	m.modalTargetVMID = vmid
	m.modalJobID = jobID
	m.inputMode = inputNone
	m.input = ""
}

func (m *Model) closeModal() {
	m.modalMode = modalNone
	m.modalField = 0
	m.modalInput = ""
	m.modalVMIDs = ""
	m.modalStorage = 0
	m.modalGuest = 0
	m.modalTargetVMID = 0
	m.modalJobID = ""
	m.modalAction = ""
	m.modalTitle = ""
	m.modalBody = ""
}

func (m Model) handleConfirmSubmit() (tea.Model, tea.Cmd) {
	action := m.modalAction
	vmid := m.modalTargetVMID
	name := m.modalInput
	m.closeModal()
	m.message = "running " + action
	switch action {
	case "delete-backup":
		return m, m.deleteBackupVolume(name)
	case "restore-backup":
		volume, ok := m.selectedBackupVolumeByVolID(name)
		if !ok {
			m.message = "backup selection not found"
			return m, nil
		}
		return m, m.restoreBackupVolume(volume)
	case "delete-snapshot":
		return m, m.deleteSnapshot(vmid, name)
	case "restore-snapshot":
		return m, m.rollbackSnapshot(vmid, name)
	}
	return m, nil
}
func (m Model) handleModalSubmit() (tea.Model, tea.Cmd) {
	lastField := m.modalFieldCount() - 1
	confirmField := lastField - 1
	if m.modalField == lastField {
		m.closeModal()
		return m, nil
	}
	if m.modalField != confirmField {
		m.modalField = confirmField
		return m, nil
	}

	value := strings.TrimSpace(m.modalInput)
	vmids := strings.TrimSpace(m.modalVMIDs)
	mode := m.modalMode
	vmid := m.modalTargetVMID
	jobID := m.modalJobID
	storage := m.selectedModalStorage()
	m.closeModal()

	switch mode {
	case modalSnapshot:
		if value == "" {
			value = "manual-" + time.Now().Format("20060102-150405")
		}
		if err := validate.SnapshotName(value); err != nil {
			m.message = err.Error()
			return m, nil
		}
		m.message = fmt.Sprintf("creating snapshot %s for %d", value, vmid)
		return m, m.createSnapshot(vmid, value)
	case modalBackup:
		m.message = fmt.Sprintf("starting backup for %d", vmid)
		return m, m.runBackup(vmid, storage)
	case modalSchedule:
		if value == "" {
			m.message = "backup schedule is required"
			return m, nil
		}
		if vmids == "" {
			m.message = "backup VMIDs are required"
			return m, nil
		}
		m.message = fmt.Sprintf("updating backup job %s", jobID)
		return m, m.updateBackupJob(jobID, storage, value, vmids)
	}

	return m, nil
}

func (m Model) modalFieldCount() int {
	if m.modalMode == modalSchedule {
		return 5
	}
	return 3
}

func (m Model) selectedModalStorage() string {
	storages := m.backupStorages()
	if len(storages) == 0 {
		return strings.TrimSpace(m.modalInput)
	}
	if m.modalStorage < 0 || m.modalStorage >= len(storages) {
		return storages[0]
	}
	return storages[m.modalStorage]
}

func (m *Model) nextModalStorage(delta int) {
	storages := m.backupStorages()
	if len(storages) == 0 {
		return
	}
	m.modalStorage = (m.modalStorage + delta + len(storages)) % len(storages)
	m.modalInput = storages[m.modalStorage]
}

func (m *Model) nextModalGuest(delta int) {
	if len(m.data.guests) == 0 {
		return
	}
	m.modalGuest = (m.modalGuest + delta + len(m.data.guests)) % len(m.data.guests)
}

func (m *Model) toggleModalGuest() {
	if len(m.data.guests) == 0 || m.modalGuest >= len(m.data.guests) {
		return
	}
	vmid := fmt.Sprintf("%d", m.data.guests[m.modalGuest].VMID)
	parts := vmidParts(m.modalVMIDs)
	for i, part := range parts {
		if part == vmid {
			parts = append(parts[:i], parts[i+1:]...)
			m.modalVMIDs = strings.Join(parts, ",")
			return
		}
	}
	parts = append(parts, vmid)
	m.modalVMIDs = strings.Join(parts, ",")
}

func (m Model) modalGuestLabel() string {
	if len(m.data.guests) == 0 || m.modalGuest >= len(m.data.guests) {
		return "-"
	}
	guest := m.data.guests[m.modalGuest]
	mark := " "
	for _, part := range vmidParts(m.modalVMIDs) {
		if part == fmt.Sprintf("%d", guest.VMID) {
			mark = "x"
			break
		}
	}
	return fmt.Sprintf("[%s] %d %s", mark, guest.VMID, guest.Name)
}

func (m Model) handleInputSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	mode := m.inputMode
	m.inputMode = inputNone
	m.input = ""

	switch mode {
	case inputSearch:
		m.query = input
		m.resetActiveSelection()
	case inputDisableTFA:
		user, ok := m.selectedUserModel()
		if !ok {
			return m, nil
		}
		if input != user.UserID {
			m.message = "confirmation mismatch"
			return m, nil
		}
		m.message = fmt.Sprintf("disabling TFA for %s", user.UserID)
		return m, m.disableUserTFA(user.UserID)
	}

	return m, nil
}

func (m Model) loadConfig(vmid int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		config, err := m.client.GuestConfig(ctx, vmid)
		return configMsg{vmid: vmid, config: config, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(15*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) header(width int) string {
	title := "pve-console"
	node := m.data.node.Name
	if node == "" {
		node = "loading"
	}
	meta := strings.TrimSpace(m.data.node.ProxmoxVersion)
	if meta == "" {
		meta = "Proxmox"
	}
	meta += fmt.Sprintf(" | view:%s type:%s filter:%s sort:%s updates:%d", m.viewMode, m.guestTypeFilter, m.filter, m.sortBy, len(m.data.updates))
	if m.query != "" {
		meta += " | search:" + m.query
	}
	if m.inputMode != inputNone {
		meta += fmt.Sprintf(" | %s:%s", m.inputMode, m.input)
	}
	if m.loading {
		meta += " | refreshing"
	}
	return headerStyle.Width(width).Render(fmt.Sprintf("%s  %s  %s", title, node, meta))
}

func (m Model) guestPanel(width int, height int) string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("GUESTS"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-5s %-24s %-6s %-10s", "ID", "NAME", "TYPE", "STATUS")))
	b.WriteString("\n")

	if m.data.err != nil {
		b.WriteString(errorStyle.Render(m.data.err.Error()))
	} else if m.loading && len(m.data.guests) == 0 {
		b.WriteString(loadingBlock("scanning guests"))
	} else if len(m.filteredGuests()) == 0 {
		b.WriteString(mutedStyle.Render("no guests found"))
	} else {
		guests := m.filteredGuests()
		rows := max(1, height-5)
		start := 0
		if m.selected >= rows {
			start = m.selected - rows + 1
		}
		end := min(len(guests), start+rows)
		for i := start; i < end; i++ {
			guest := guests[i]
			status := statusLabel(guest.Status)
			line := fmt.Sprintf("%-5d %-24s %-6s %-10s", guest.VMID, truncate(guest.Name, 24), strings.ToUpper(string(guest.Type)), status)
			if i == m.selected {
				b.WriteString(selectedRowStyle.Width(width - 4).Render(line))
			} else {
				b.WriteString(rowStyle.Width(width - 4).Render(line))
			}
			b.WriteString("\n")
		}
	}

	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) userPanel(width int, height int) string {
	users := m.filteredUsers()
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(fmt.Sprintf("USERS (%d)", len(users))))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-28s %-7s %-5s %-20s", "USERID", "ENABLED", "TFA", "EMAIL")))
	b.WriteString("\n")

	if m.loading && len(m.data.users) == 0 {
		b.WriteString(loadingBlock("reading users"))
	} else if len(m.data.users) == 0 {
		b.WriteString(mutedStyle.Render("no users loaded"))
	} else if len(users) == 0 {
		b.WriteString(mutedStyle.Render("no users found"))
	} else {
		rows := max(1, height-5)
		start := 0
		if m.selectedUser >= rows {
			start = m.selectedUser - rows + 1
		}
		end := min(len(users), start+rows)
		for i := start; i < end; i++ {
			user := users[i]
			line := fmt.Sprintf("%-28s %-7t %-5t %-20s", truncate(user.UserID, 28), bool(user.Enable), user.TFAEnabled, truncate(user.Email, 20))
			if i == m.selectedUser {
				b.WriteString(selectedRowStyle.Width(width - 4).Render(line))
			} else {
				b.WriteString(rowStyle.Width(width - 4).Render(line))
			}
			b.WriteString("\n")
		}
	}

	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) updatesPanel(width int, height int) string {
	updates := m.filteredUpdates()
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(fmt.Sprintf("APT UPDATES (%d)", len(updates))))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-28s %-18s %-18s", "PACKAGE", "CURRENT", "CANDIDATE")))
	b.WriteString("\n")
	if m.data.updateErr != nil {
		b.WriteString(errorStyle.Render(m.data.updateErr.Error()))
	} else if m.loading && len(m.data.updates) == 0 {
		b.WriteString(loadingBlock("checking apt"))
	} else if len(m.data.updates) == 0 {
		b.WriteString(okStyle.Render("system up to date"))
	} else if len(updates) == 0 {
		b.WriteString(mutedStyle.Render("no updates found"))
	} else {
		rows := max(1, height-5)
		for i := 0; i < min(len(updates), rows); i++ {
			update := updates[i]
			line := fmt.Sprintf("%-28s %-18s %-18s", truncate(update.Name, 28), truncate(update.Current, 18), truncate(update.Candidate, 18))
			b.WriteString(rowStyle.Width(width - 4).Render(line))
			b.WriteString("\n")
		}
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) backupsPanel(width int, height int) string {
	count := m.backupItemCount()
	var b strings.Builder
	if m.backupMode == "jobs" {
		b.WriteString(panelTitleStyle.Render(fmt.Sprintf("BACKUP JOBS (%d)", count)))
	} else {
		b.WriteString(panelTitleStyle.Render(fmt.Sprintf("BACKUP VOLUMES (%d)", count)))
	}
	b.WriteString("\n")
	if m.backupMode == "jobs" {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%-18s %-14s %-18s %-8s %-18s", "ID", "STORAGE", "SCHEDULE", "ENABLED", "VMIDS")))
	} else {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("%-6s %-22s %-17s %-10s", "VMID", "NAME", "DATE", "SIZE")))
	}
	b.WriteString("\n")
	if m.data.backupErr != nil {
		b.WriteString(errorStyle.Render(m.data.backupErr.Error()))
	} else if m.loading && count == 0 {
		b.WriteString(loadingBlock("indexing backups"))
	} else if count == 0 {
		b.WriteString(mutedStyle.Render("no backups found"))
	} else {
		rows := max(1, height-5)
		start := 0
		if m.selectedBackup >= rows {
			start = m.selectedBackup - rows + 1
		}
		end := min(count, start+rows)
		for i := start; i < end; i++ {
			line := m.backupLine(i)
			if i == m.selectedBackup {
				b.WriteString(selectedRowStyle.Width(width - 4).Render(line))
			} else {
				b.WriteString(rowStyle.Width(width - 4).Render(line))
			}
			b.WriteString("\n")
		}
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
}

func loadingBlock(label string) string {
	frame := int(time.Now().UnixMilli()/350) % len(loaderPetFrames)
	return mutedStyle.Render(loadingPet(label, frame))
}

var loaderPetFrames = []string{
	"(o_o)", "(O_o)", "(o_O)", "(^_^)",
}

func loadingPet(label string, frame int) string {
	if frame < 0 {
		frame = 0
	}
	face := loaderPetFrames[frame%len(loaderPetFrames)]
	steps := []string{".", "..", "...", " .."}
	step := steps[frame%len(steps)]
	lines := []string{
		" +---+",
		fmt.Sprintf("| %s |  %s", face, label),
		" +---+   tamapve" + step,
	}
	return strings.Join(lines, "\n")
}
func (m Model) detailPanel(width int, height int) string {
	if m.viewMode == viewUsers {
		return m.userDetailPanel(width, height)
	}
	if m.viewMode == viewUpdates {
		return m.updatesDetailPanel(width, height)
	}
	if m.viewMode == viewBackups {
		return m.backupsDetailPanel(width, height)
	}
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("DETAIL"))
	b.WriteString("\n")

	if guest, ok := m.selectedGuest(); ok {
		b.WriteString(kv("ID", fmt.Sprintf("%d", guest.VMID)))
		b.WriteString(kv("Name", guest.Name))
		b.WriteString(kv("Type", strings.ToUpper(string(guest.Type))))
		b.WriteString(kv("Status", statusLabel(guest.Status)))
		b.WriteString(kv("CPU", fmt.Sprintf("%.2f", guest.CPU)))
		b.WriteString(kv("Memory", bytesPair(guest.Memory, guest.MaxMem)))
		b.WriteString(kv("Uptime", durationLabel(guest.Uptime)))
	} else {
		b.WriteString(mutedStyle.Render("Select a guest"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(panelTitleStyle.Render("STORAGE"))
	b.WriteString("\n")
	for _, storage := range m.data.storages {
		line := fmt.Sprintf("%-14s %3.0f%% %s", truncate(storage.Name, 14), storage.Usage*100, activeLabel(storage.Active))
		b.WriteString(line)
		b.WriteString("\n")
	}

	if m.showSnaps {
		b.WriteString("\n")
		b.WriteString(panelTitleStyle.Render(fmt.Sprintf("SNAPSHOTS %d", m.snapshotVMID)))
		b.WriteString("\n")
		if m.snapErr != nil {
			b.WriteString(errorStyle.Render(m.snapErr.Error()))
			b.WriteString("\n")
		} else if len(m.snapshots) == 0 {
			b.WriteString(mutedStyle.Render("none loaded"))
			b.WriteString("\n")
		} else {
			for i, snapshot := range m.snapshots {
				line := truncate(snapshot.Name, max(8, width-8))
				if i == m.selectedSnapshot {
					b.WriteString(selectedRowStyle.Width(width - 4).Render(line))
				} else {
					b.WriteString(line)
				}
				b.WriteString("\n")
			}
			b.WriteString(mutedStyle.Render("d delete  o restore"))
			b.WriteString("\n")
		}
	}

	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.message))
		b.WriteString("\n")
	}

	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) userDetailPanel(width int, height int) string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("USER"))
	b.WriteString("\n")

	if user, ok := m.selectedUserModel(); ok {
		b.WriteString(kv("User", user.UserID))
		b.WriteString(kv("Enabled", fmt.Sprintf("%t", bool(user.Enable))))
		b.WriteString(kv("TFA", fmt.Sprintf("%t", user.TFAEnabled)))
		b.WriteString(kv("Email", user.Email))
		b.WriteString(kv("Name", strings.TrimSpace(user.FirstName+" "+user.LastName)))
		b.WriteString(kv("Comment", user.Comment))
	} else {
		b.WriteString(mutedStyle.Render("Select a user"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(panelTitleStyle.Render("TFA"))
	b.WriteString("\n")
	if m.tfaErr != nil {
		b.WriteString(errorStyle.Render(m.tfaErr.Error()))
		b.WriteString("\n")
	} else if m.tfaUserID == "" {
		b.WriteString(mutedStyle.Render("press s to load TFA entries"))
		b.WriteString("\n")
	} else if len(m.tfaEntries) == 0 {
		b.WriteString(mutedStyle.Render("no TFA entries"))
		b.WriteString("\n")
	} else {
		for _, entry := range m.tfaEntries {
			line := fmt.Sprintf("%s %s %s", entry.ID, entry.Type, entry.Description)
			b.WriteString(truncate(line, max(8, width-8)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(warnStyle.Render("x disable TFA, confirm with userid"))
	b.WriteString("\n")
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.message))
		b.WriteString("\n")
	}

	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) updatesDetailPanel(width int, height int) string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("APT"))
	b.WriteString("\n")
	b.WriteString(kv("Updates", fmt.Sprintf("%d", len(m.data.updates))))
	if len(m.data.updates) > 0 {
		b.WriteString(kv("Action", "a dist-upgrade"))
	}
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.message))
		b.WriteString("\n")
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) backupsDetailPanel(width int, height int) string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("BACKUPS"))
	b.WriteString("\n")
	b.WriteString(kv("Jobs", fmt.Sprintf("%d", len(m.data.backups))))
	b.WriteString(kv("Volumes", fmt.Sprintf("%d", len(m.data.backupVolumes))))
	b.WriteString(kv("Mode", m.backupMode))
	b.WriteString(kv("Toggle", "tab"))
	b.WriteString(kv("Manual", "m"))
	b.WriteString(kv("Schedule", "e"))
	if m.backupMode != "jobs" {
		b.WriteString(kv("Delete", "d"))
		b.WriteString(kv("Restore", "o"))
	}
	b.WriteString("\n")
	if m.backupMode == "jobs" {
		job, ok := m.selectedBackupJob()
		if !ok {
			return panelStyle.Width(width).Height(height).Render(b.String())
		}
		b.WriteString(kv("ID", job.ID))
		b.WriteString(kv("Storage", job.Storage))
		b.WriteString(kv("Schedule", job.Schedule))
		b.WriteString(kv("Enabled", fmt.Sprintf("%t", bool(job.Enabled))))
		b.WriteString(kv("VMIDs", job.VMID))
	}
	if m.backupMode != "jobs" {
		volume, ok := m.selectedBackupVolume()
		if !ok {
			return panelStyle.Width(width).Height(height).Render(b.String())
		}
		b.WriteString(kv("VMID", fmt.Sprintf("%d", volume.VMID)))
		b.WriteString(kv("Name", m.guestName(volume.VMID)))
		b.WriteString(kv("Date", timeLabel(volume.CTime)))
		b.WriteString(kv("Format", volume.Format))
		b.WriteString(kv("Size", humanBytes(volume.Size)))
		b.WriteString(kv("VolID", volume.VolID))
	}
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.message))
		b.WriteString("\n")
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) inputModal(width int) string {
	if m.inputMode == inputNone {
		return ""
	}

	title := "SEARCH"
	body := []string{kv("Query", selectedRowStyle.Width(32).Render(blankInput(m.input))), "enter apply  esc cancel"}
	if m.inputMode == inputDisableTFA {
		user := ""
		if selected, ok := m.selectedUserModel(); ok {
			user = selected.UserID
		}
		title = "DISABLE TFA"
		body = []string{
			kv("User", user),
			kv("Confirm", selectedRowStyle.Width(32).Render(blankInput(m.input))),
			warnStyle.Render("Type the user id to confirm."),
			"enter disable TFA  esc cancel",
		}
	}

	lines := []string{panelTitleStyle.Render(title), ""}
	lines = append(lines, body...)
	return lipgloss.NewStyle().
		Width(min(52, max(36, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("81")).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func blankInput(value string) string {
	if value == "" {
		return " "
	}
	return value
}

func overlayView(base string, modal string, width int, height int) string {
	if modal == "" {
		return base
	}
	lines := strings.Split(base, "\n")
	modalLines := strings.Split(modal, "\n")
	if len(lines) < height {
		for len(lines) < height {
			lines = append(lines, "")
		}
	}

	modalHeight := len(modalLines)
	modalWidth := 0
	for _, line := range modalLines {
		modalWidth = max(modalWidth, lipgloss.Width(line))
	}
	startRow := max(1, (height-modalHeight)/2)
	startCol := max(0, (width-modalWidth)/2)
	for i, line := range modalLines {
		row := startRow + i
		if row >= len(lines) {
			break
		}
		lines[row] = strings.Repeat(" ", startCol) + line
	}
	return strings.Join(lines, "\n")
}
func (m Model) modal(width int) string {
	if m.modalMode == modalStartup {
		return m.startupModal(width)
	}
	if m.modalMode == modalHelp {
		return m.helpModal(width)
	}
	if m.modalMode == modalSchedule {
		return m.scheduleModal(width)
	}
	if m.modalMode == modalUpgrade {
		return m.upgradeModal(width)
	}
	if m.modalMode == modalConfirm {
		return m.confirmModal(width)
	}

	title := "SNAPSHOT"
	label := "Name"
	confirm := "Snap"
	context := fmt.Sprintf("VMID %d", m.modalTargetVMID)
	if m.modalMode == modalBackup {
		title = "BACKUP"
		label = "Storage"
		confirm = "Backup"
		context = fmt.Sprintf("VMID %d", m.modalTargetVMID)
	}
	if m.modalMode == modalSchedule {
		title = "BACKUP SCHEDULE"
		label = "Schedule"
		confirm = "Save"
		context = "Job " + m.modalJobID
	}

	input := m.modalInput
	if m.modalMode == modalBackup {
		input = "< " + m.selectedModalStorage() + " >"
	}
	if input == "" {
		input = " "
	}
	inputStyle := rowStyle
	if m.modalField == 0 {
		inputStyle = selectedRowStyle
	}
	lastField := m.modalFieldCount() - 1
	confirmField := lastField - 1
	confirmStyle := rowStyle
	if m.modalField == confirmField {
		confirmStyle = selectedRowStyle
	}
	cancelStyle := rowStyle
	if m.modalField == lastField {
		cancelStyle = selectedRowStyle
	}

	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(context))
	b.WriteString("\n\n")
	b.WriteString(kv(label, inputStyle.Width(28).Render(input)))
	if m.modalMode == modalSchedule {
		vmids := m.modalVMIDs
		if vmids == "" {
			vmids = " "
		}
		vmidStyle := rowStyle
		if m.modalField == 1 {
			vmidStyle = selectedRowStyle
		}
		b.WriteString(kv("VMIDs", vmidStyle.Width(28).Render(vmids)))
		b.WriteString(kv("Guest", m.modalGuestLabel()))
	}
	b.WriteString("\n")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, confirmStyle.Padding(0, 2).Render(confirm), "  ", cancelStyle.Padding(0, 2).Render("Annuler")))
	b.WriteString("\n")
	help := "tab select  enter apply  esc cancel"
	if m.modalMode == modalBackup {
		help = "up/down storage  tab select  enter apply  esc cancel"
	} else if m.modalMode == modalSchedule {
		help = "vmids: up/down guest  space toggle  tab select  enter apply  esc cancel"
	}
	b.WriteString(mutedStyle.Render(help))

	return lipgloss.NewStyle().
		Width(min(48, max(32, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("81")).
		Padding(1, 2).
		Render(b.String())
}

func (m Model) scheduleModal(width int) string {
	lastField := m.modalFieldCount() - 1
	confirmField := lastField - 1
	storageStyle := rowStyle
	if m.modalField == 0 {
		storageStyle = selectedRowStyle
	}
	scheduleStyle := rowStyle
	if m.modalField == 1 {
		scheduleStyle = selectedRowStyle
	}
	confirmStyle := rowStyle
	if m.modalField == confirmField {
		confirmStyle = selectedRowStyle
	}
	cancelStyle := rowStyle
	if m.modalField == lastField {
		cancelStyle = selectedRowStyle
	}

	input := blankInput(m.modalInput)
	storage := "< " + m.selectedModalStorage() + " >"
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("BACKUP SCHEDULE"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Job " + m.modalJobID))
	b.WriteString("\n\n")
	b.WriteString(kv("Storage", storageStyle.Width(30).Render(storage)))
	b.WriteString(kv("Schedule", scheduleStyle.Width(30).Render(input)))
	b.WriteString("\n")
	b.WriteString(panelTitleStyle.Render("VMIDS"))
	b.WriteString("\n")
	b.WriteString(m.modalGuestChecklist(max(28, min(44, width-12)), 8))
	b.WriteString("\n")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, confirmStyle.Padding(0, 2).Render("Save"), "  ", cancelStyle.Padding(0, 2).Render("Annuler")))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("storage: arrows  schedule: type/backspace  vmids: up/down space  tab select  enter apply  esc cancel"))

	return lipgloss.NewStyle().
		Width(min(62, max(44, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("81")).
		Padding(1, 2).
		Render(b.String())
}

func (m Model) modalGuestChecklist(width int, rows int) string {
	if len(m.data.guests) == 0 {
		return mutedStyle.Render("no guests loaded")
	}
	start := 0
	if m.modalGuest >= rows {
		start = m.modalGuest - rows + 1
	}
	end := min(len(m.data.guests), start+rows)
	selected := map[string]bool{}
	for _, part := range vmidParts(m.modalVMIDs) {
		selected[part] = true
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		guest := m.data.guests[i]
		mark := " "
		if selected[fmt.Sprintf("%d", guest.VMID)] {
			mark = "x"
		}
		line := fmt.Sprintf("[%s] %-5d %s", mark, guest.VMID, truncate(guest.Name, max(8, width-12)))
		style := rowStyle
		if m.modalField == 2 && i == m.modalGuest {
			style = selectedRowStyle
		}
		b.WriteString(style.Width(width).Render(line))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
func (m Model) confirmModal(width int) string {
	lines := []string{
		warnStyle.Render(m.modalTitle),
		"",
		m.modalBody,
		"",
		warnStyle.Render("enter confirm  esc cancel"),
	}
	return lipgloss.NewStyle().
		Width(min(72, max(44, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}
func (m Model) upgradeModal(width int) string {
	lines := []string{panelTitleStyle.Render("APT DIST-UPGRADE"), ""}
	if m.upgradeRunning {
		lines = append(lines, loadingBlock("apt-get -y dist-upgrade"))
	} else {
		if m.upgradeErr != nil {
			lines = append(lines, errorStyle.Render(m.upgradeErr.Error()), "")
		}
		output := strings.TrimSpace(m.upgradeOutput)
		if output == "" {
			output = "no output"
		}
		for _, line := range strings.Split(output, "\n") {
			lines = append(lines, truncate(line, max(20, min(86, width-12))))
			if len(lines) >= 20 {
				lines = append(lines, mutedStyle.Render("... output truncated"))
				break
			}
		}
		lines = append(lines, "", mutedStyle.Render("enter/esc close"))
	}

	return lipgloss.NewStyle().
		Width(min(92, max(54, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}
func (m Model) startupModal(width int) string {
	lines := []string{
		warnStyle.Render("EXPERIMENTAL"),
		version.String(),
		"",
		"pve-console is still experimental.",
		"Use it carefully on production hosts.",
		"",
		"All Proxmox actions are executed as root.",
		"Make sure you understand the selected guest, storage, and job before confirming an action.",
		"",
		mutedStyle.Render("enter continue  esc close warning"),
	}

	return lipgloss.NewStyle().
		Width(min(62, max(42, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m Model) helpModal(width int) string {
	lines := []string{
		panelTitleStyle.Render("HELP"),
		"",
		"Navigation",
		"  up/down, k/j   move selection",
		"  enter          open guest config",
		"  esc            back / close",
		"  r              refresh",
		"  q              quit",
		"",
		"Views",
		"  g              guests",
		"  u              users",
		"  p              updates",
		"  b              backups",
		"  tab            guest type or backup mode",
		"",
		"Guests",
		"  /              search",
		"  c              clear search",
		"  f              status filter",
		"  t              sort",
		"  s              load snapshots",
		"  n              new snapshot",
		"  m              manual backup",
		"",
		"Backups",
		"  m              manual backup",
		"  e              edit schedule and VMIDs",
		"  storage modal  up/down select storage",
		"  VMIDs modal    up/down guest, space toggle",
		"",
		"Users",
		"  s              load TFA",
		"  x              disable TFA",
		"",
		"Help",
		"  ?, h           open / close help",
		"  enter, esc     close help",
		"",
		mutedStyle.Render("dev by @clinux"),
	}

	return lipgloss.NewStyle().
		Width(min(56, max(38, width-8))).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("81")).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

func (m Model) configPanel(width int, height int) string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(fmt.Sprintf("CONFIG %d", m.configVMID)))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("esc back"))
	b.WriteString("\n\n")

	if m.configErr != nil {
		b.WriteString(errorStyle.Render(m.configErr.Error()))
		b.WriteString("\n")
		return panelStyle.Width(width).Height(height).Render(b.String())
	}
	if m.config == nil {
		b.WriteString(mutedStyle.Render("loading..."))
		b.WriteString("\n")
		return panelStyle.Width(width).Height(height).Render(b.String())
	}

	writeConfigSection(&b, "HARDWARE", m.config, []string{
		"cores", "sockets", "cpu", "memory", "balloon",
		"scsihw", "bios", "machine", "ostype", "agent",
	})
	writeConfigPrefixes(&b, "DISKS", m.config, []string{
		"scsi", "virtio", "sata", "ide", "efidisk", "tpmstate", "rootfs", "mp",
	})
	writeConfigPrefixes(&b, "NETWORK", m.config, []string{"net"})
	writeConfigSection(&b, "OPTIONS", m.config, remainingConfigKeys(m.config))

	return panelStyle.Width(width).Height(height).Render(b.String())
}

func writeConfigSection(b *strings.Builder, title string, config proxmox.GuestConfig, keys []string) {
	wroteTitle := false
	for _, key := range keys {
		value, ok := config[key]
		if !ok {
			continue
		}
		if !wroteTitle {
			b.WriteString(panelTitleStyle.Render(title))
			b.WriteString("\n")
			wroteTitle = true
		}
		b.WriteString(kv(truncate(key, 10), truncate(fmt.Sprint(value), 44)))
	}
	if wroteTitle {
		b.WriteString("\n")
	}
}

func writeConfigPrefixes(b *strings.Builder, title string, config proxmox.GuestConfig, prefixes []string) {
	keys := make([]string, 0)
	for key := range config {
		for _, prefix := range prefixes {
			if strings.HasPrefix(key, prefix) {
				keys = append(keys, key)
				break
			}
		}
	}
	sort.Strings(keys)
	writeConfigSection(b, title, config, keys)
}

func remainingConfigKeys(config proxmox.GuestConfig) []string {
	known := map[string]bool{
		"cores": true, "sockets": true, "cpu": true, "memory": true, "balloon": true,
		"scsihw": true, "bios": true, "machine": true, "ostype": true, "agent": true,
	}
	keys := make([]string, 0)
	for key := range config {
		if known[key] || hasAnyPrefix(key, []string{"scsi", "virtio", "sata", "ide", "efidisk", "tpmstate", "rootfs", "mp", "net"}) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func (m Model) selectedGuest() (proxmox.Guest, bool) {
	guests := m.filteredGuests()
	if len(guests) == 0 || m.selected >= len(guests) {
		return proxmox.Guest{}, false
	}
	return guests[m.selected], true
}

func (m Model) filteredGuests() []proxmox.Guest {
	guests := make([]proxmox.Guest, 0, len(m.data.guests))
	query := strings.ToLower(strings.TrimSpace(m.query))
	for _, guest := range m.data.guests {
		if m.guestTypeFilter != "all" && string(guest.Type) != m.guestTypeFilter {
			continue
		}
		if m.filter != "all" && guest.Status != m.filter {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(guest.Name), query) && !strings.Contains(fmt.Sprintf("%d", guest.VMID), query) {
			continue
		}
		guests = append(guests, guest)
	}

	sort.SliceStable(guests, func(i int, j int) bool {
		switch m.sortBy {
		case "name":
			return strings.ToLower(guests[i].Name) < strings.ToLower(guests[j].Name)
		case "status":
			if guests[i].Status == guests[j].Status {
				return guests[i].VMID < guests[j].VMID
			}
			return guests[i].Status < guests[j].Status
		default:
			return guests[i].VMID < guests[j].VMID
		}
	})

	return guests
}

func (m Model) filteredUsers() []proxmox.User {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.users
	}
	users := make([]proxmox.User, 0, len(m.data.users))
	for _, user := range m.data.users {
		values := []string{user.UserID, user.Email, user.FirstName, user.LastName, user.Comment}
		if containsAnyFold(values, query) {
			users = append(users, user)
		}
	}
	return users
}

func (m Model) filteredUpdates() []proxmox.PackageUpdate {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.updates
	}
	updates := make([]proxmox.PackageUpdate, 0, len(m.data.updates))
	for _, update := range m.data.updates {
		values := []string{update.Name, update.Current, update.Candidate, update.Suite}
		if containsAnyFold(values, query) {
			updates = append(updates, update)
		}
	}
	return updates
}

func (m Model) filteredBackupJobs() []proxmox.BackupJob {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.backups
	}
	jobs := make([]proxmox.BackupJob, 0, len(m.data.backups))
	for _, job := range m.data.backups {
		values := []string{job.ID, job.Storage, job.Schedule, job.VMID, job.Mode, job.Compress, fmt.Sprintf("%t", bool(job.Enabled))}
		if containsAnyFold(values, query) {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func (m Model) filteredBackupVolumes() []proxmox.BackupVolume {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.backupVolumes
	}
	volumes := make([]proxmox.BackupVolume, 0, len(m.data.backupVolumes))
	for _, volume := range m.data.backupVolumes {
		values := []string{fmt.Sprintf("%d", volume.VMID), m.guestName(volume.VMID), volume.VolID, volume.Format, volume.Notes, timeLabel(volume.CTime), humanBytes(volume.Size)}
		if containsAnyFold(values, query) {
			volumes = append(volumes, volume)
		}
	}
	return volumes
}

func (m *Model) nextFilter() {
	switch m.filter {
	case "all":
		m.filter = "running"
	case "running":
		m.filter = "stopped"
	default:
		m.filter = "all"
	}
}

func (m *Model) nextGuestTypeFilter() {
	switch m.guestTypeFilter {
	case "all":
		m.guestTypeFilter = string(proxmox.GuestTypeQEMU)
	case string(proxmox.GuestTypeQEMU):
		m.guestTypeFilter = string(proxmox.GuestTypeLXC)
	default:
		m.guestTypeFilter = "all"
	}
}

func (m *Model) nextSort() {
	switch m.sortBy {
	case "id":
		m.sortBy = "name"
	case "name":
		m.sortBy = "status"
	default:
		m.sortBy = "id"
	}
}

func (m *Model) toggleView() {
	if m.viewMode == viewUsers {
		m.viewMode = viewGuests
		m.message = ""
		return
	}
	m.viewMode = viewUsers
	m.message = "users view: s load TFA, x disable TFA"
}

func (m *Model) moveSelection(delta int) {
	if m.viewMode == viewGuests && m.showSnaps {
		next := m.selectedSnapshot + delta
		if next >= 0 && next < len(m.snapshots) {
			m.selectedSnapshot = next
		}
		return
	}
	if m.viewMode == viewUsers {
		next := m.selectedUser + delta
		if next >= 0 && next < len(m.filteredUsers()) {
			m.selectedUser = next
		}
		return
	}
	if m.viewMode == viewBackups {
		next := m.selectedBackup + delta
		if next >= 0 && next < m.backupItemCount() {
			m.selectedBackup = next
		}
		return
	}

	next := m.selected + delta
	if next >= 0 && next < len(m.filteredGuests()) {
		m.selected = next
	}
}

func (m *Model) resetActiveSelection() {
	switch m.viewMode {
	case viewUsers:
		m.selectedUser = 0
	case viewBackups:
		m.selectedBackup = 0
	default:
		m.selected = 0
	}
	m.clampSelections()
}

func (m *Model) clampSelections() {
	m.selected = clampIndex(m.selected, len(m.filteredGuests()))
	m.selectedUser = clampIndex(m.selectedUser, len(m.filteredUsers()))
	m.selectedBackup = clampIndex(m.selectedBackup, m.backupItemCount())
}

func clampIndex(index int, count int) int {
	if count <= 0 || index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}

func (m Model) selectedUserModel() (proxmox.User, bool) {
	users := m.filteredUsers()
	if len(users) == 0 || m.selectedUser >= len(users) {
		return proxmox.User{}, false
	}
	return users[m.selectedUser], true
}

func (m Model) backupItemCount() int {
	if m.backupMode == "jobs" {
		return len(m.filteredBackupJobs())
	}
	return len(m.filteredBackupVolumes())
}

func (m Model) backupLine(index int) string {
	if m.backupMode == "jobs" {
		job := m.filteredBackupJobs()[index]
		return fmt.Sprintf("%-18s %-14s %-18s %-8t %-18s", truncate(job.ID, 18), truncate(job.Storage, 14), truncate(job.Schedule, 18), bool(job.Enabled), truncate(job.VMID, 18))
	}
	volume := m.filteredBackupVolumes()[index]
	return fmt.Sprintf("%-6d %-22s %-17s %-10s", volume.VMID, truncate(m.guestName(volume.VMID), 22), timeLabel(volume.CTime), humanBytes(volume.Size))
}

func (m Model) selectedBackupJob() (proxmox.BackupJob, bool) {
	jobs := m.filteredBackupJobs()
	if len(jobs) == 0 || m.selectedBackup >= len(jobs) {
		return proxmox.BackupJob{}, false
	}
	return jobs[m.selectedBackup], true
}

func (m Model) selectedSnapshotModel() (proxmox.Snapshot, bool) {
	if len(m.snapshots) == 0 || m.selectedSnapshot >= len(m.snapshots) {
		return proxmox.Snapshot{}, false
	}
	return m.snapshots[m.selectedSnapshot], true
}

func (m Model) selectedBackupVolumeByVolID(volid string) (proxmox.BackupVolume, bool) {
	for _, volume := range m.filteredBackupVolumes() {
		if volume.VolID == volid {
			return volume, true
		}
	}
	return proxmox.BackupVolume{}, false
}
func (m Model) selectedBackupVolume() (proxmox.BackupVolume, bool) {
	volumes := m.filteredBackupVolumes()
	if len(volumes) == 0 || m.selectedBackup >= len(volumes) {
		return proxmox.BackupVolume{}, false
	}
	return volumes[m.selectedBackup], true
}

func (m Model) selectedBackupVMID() int {
	if m.backupMode != "jobs" {
		volume, ok := m.selectedBackupVolume()
		if ok {
			return volume.VMID
		}
		return 0
	}
	job, ok := m.selectedBackupJob()
	if !ok {
		return 0
	}
	for _, part := range vmidParts(job.VMID) {
		var vmid int
		if _, err := fmt.Sscanf(part, "%d", &vmid); err == nil && vmid > 0 {
			return vmid
		}
	}
	return 0
}

func vmidParts(value string) []string {
	raw := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	})
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func normalizedQuery(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsAnyFold(values []string, query string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (m Model) guestName(vmid int) string {
	for _, guest := range m.data.guests {
		if guest.VMID == vmid {
			return guest.Name
		}
	}
	if vmid == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", vmid)
}

func (m Model) defaultBackupStorage() string {
	storages := m.backupStorages()
	if len(storages) > 0 {
		return storages[0]
	}
	return ""
}

func (m Model) backupStorages() []string {
	names := make([]string, 0)
	for _, job := range m.data.backups {
		if job.Storage != "" {
			names = appendUnique(names, job.Storage)
		}
	}
	for _, storage := range m.data.storages {
		if bool(storage.Active) && strings.Contains(strings.ToLower(storage.Name), "backup") {
			names = appendUnique(names, storage.Name)
		}
	}
	for _, storage := range m.data.storages {
		if bool(storage.Active) {
			names = appendUnique(names, storage.Name)
		}
	}
	return names
}

func (m Model) storageIndex(name string) int {
	for i, storage := range m.backupStorages() {
		if storage == name {
			return i
		}
	}
	return 0
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (m Model) footerText() string {
	if m.modalMode == modalStartup {
		return "experimental warning  enter continue  esc close warning"
	}
	if m.modalMode == modalHelp {
		return "help  enter/esc close  dev by @clinux"
	}
	if m.modalMode == modalSnapshot {
		ram := "on"
		if !m.snapshotVMState {
			ram = "off"
		}
		return "snapshot modal  ram:" + ram + "  enter snap  esc cancel"
	}
	if m.modalMode == modalBackup {
		return "backup modal  enter backup  esc cancel"
	}
	if m.modalMode == modalSchedule {
		return "schedule modal  enter save  esc cancel"
	}
	if m.modalMode == modalConfirm {
		return "confirmation  enter confirm  esc cancel"
	}
	if m.modalMode == modalUpgrade {
		if m.upgradeRunning {
			return "apt dist-upgrade running"
		}
		return "apt dist-upgrade output  enter/esc close"
	}
	if m.inputMode == inputDisableTFA {
		return "confirm userid: " + m.input + "  enter disable TFA  esc cancel"
	}
	if m.inputMode == inputSearch {
		return "search: " + m.input + "  enter apply  esc cancel"
	}
	if m.viewMode == viewUsers {
		return "g guests  p updates  b backups  s load-tfa  x disable-tfa  r refresh  ? help  q quit"
	}
	if m.viewMode == viewUpdates {
		return "g guests  u users  b backups  a dist-upgrade  r refresh  ? help  q quit"
	}
	if m.viewMode == viewBackups {
		if m.viewMode == viewBackups && m.backupMode != "jobs" {
			return "g guests  tab jobs/volumes  d delete  o restore  m manual-backup  u users  p updates  r refresh  ? help  q quit"
		}
		return "g guests  tab jobs/volumes  m manual-backup  e edit-schedule  u users  p updates  r refresh  ? help  q quit"
	}
	if m.showSnaps {
		return "snapshots  up/down select  d delete  o restore  esc close  n snapshot  r refresh  ? help  q quit"
	}
	return "tab vm/ct/all  enter config  u users  p updates  b backups  m backup  r refresh  / search  f status  t sort  s snapshots  n snapshot  ? help  q quit"
}

func kv(key string, value string) string {
	return fmt.Sprintf("%s %s\n", mutedStyle.Width(8).Render(key), value)
}

func statusLabel(status string) string {
	switch status {
	case "running":
		return okStyle.Render("running")
	case "stopped":
		return mutedStyle.Render("stopped")
	default:
		return warnStyle.Render(status)
	}
}

func activeLabel(active proxmox.Bool) string {
	if bool(active) {
		return okStyle.Render("active")
	}
	return errorStyle.Render("inactive")
}

func durationLabel(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %02dh", days, hours)
	}
	return fmt.Sprintf("%02dh%02dm", hours, int(d.Minutes())%60)
}

func timeLabel(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}
	return time.Unix(timestamp, 0).Format("2006-01-02 15:04")
}

func bytesPair(used uint64, total uint64) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s", humanBytes(used), humanBytes(total))
}

func humanBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div := uint64(unit)
	exp := 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return value[:limit]
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)
	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("81"))
	rowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
	selectedRowStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("62")).
				Bold(true)
	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)
	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true)
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(0, 1)
)
