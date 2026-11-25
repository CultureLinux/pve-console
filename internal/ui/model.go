package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
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
	viewLog         string    = "log"
	viewUpdates     string    = "updates"
	viewBackups     string    = "backups"
)

// Model is the Bubble Tea model of the interactive terminal UI.
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
	configScroll     int
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
	upgradePhase     upgradePhase
	upgradeFeed      chan tea.Msg
	upgradeReboot    string
	upgradeNote      string
	tasks            []trackedTask
	clusterTasks     clusterTaskState
	entries          []logEntry
	unreadErrors     int
	logSource        errSource
	logLevel         logLevel
	selectedLog      int
	demo             bool
	message          string
	data             snapshot
}

// New returns a TUI model that shows the startup notice.
func New(client proxmox.Client) Model {
	return NewWithStartup(client, true)
}

// NewWithStartup returns a TUI model; showStartup controls the startup notice.
func NewWithStartup(client proxmox.Client, showStartup bool) Model {
	model := Model{client: client, loading: true, filter: "all", guestTypeFilter: "all", sortBy: "id", viewMode: viewGuests, snapshotVMState: true, backupMode: "volumes"}
	if showStartup {
		model.modalMode = modalStartup
	}
	return model
}

// Run starts the TUI and blocks until the user quits.
func Run(client proxmox.Client, showStartup bool) error {
	program := tea.NewProgram(NewWithStartup(client, showStartup), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

// RunDemo starts the TUI on a fake node (see DemoClient). Nothing real is read
// or changed, and the header says so.
func RunDemo() error {
	model := NewWithStartup(DemoClient(), false)
	model.demo = true
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

// Init loads the data and schedules the periodic refresh.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load, m.loadClusterTasks, tick(), spinTick(false))
}

// busy reports whether something is running, so spinners must animate.
func (m Model) busy() bool {
	return m.loading || len(m.tasks) > 0 || m.upgradeRunning
}
