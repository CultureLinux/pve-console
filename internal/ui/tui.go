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
)

type snapshot struct {
	node      proxmox.NodeInfo
	guests    []proxmox.Guest
	storages  []proxmox.Storage
	users     []proxmox.User
	updates   []proxmox.PackageUpdate
	backups   []proxmox.BackupJob
	updateErr error
	backupErr error
	err       error
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

type inputMode string

const (
	inputNone       inputMode = ""
	inputSearch     inputMode = "search"
	inputSnapshot   inputMode = "snapshot"
	inputDisableTFA inputMode = "disable-tfa"
	viewGuests      string    = "guests"
	viewUsers       string    = "users"
	viewUpdates     string    = "updates"
	viewBackups     string    = "backups"
)

type Model struct {
	client          proxmox.Client
	width           int
	height          int
	selected        int
	selectedUser    int
	loading         bool
	inputMode       inputMode
	input           string
	viewMode        string
	query           string
	filter          string
	sortBy          string
	showSnaps       bool
	showConfig      bool
	snapshotVMState bool
	snapshotVMID    int
	configVMID      int
	snapshots       []proxmox.Snapshot
	snapErr         error
	config          proxmox.GuestConfig
	configErr       error
	tfaUserID       string
	tfaEntries      []proxmox.TFAEntry
	tfaErr          error
	message         string
	data            snapshot
}

func New(client proxmox.Client) Model {
	return Model{client: client, loading: true, filter: "all", sortBy: "id", viewMode: viewGuests, snapshotVMState: true}
}

func Run(client proxmox.Client) error {
	program := tea.NewProgram(New(client), tea.WithAltScreen())
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
		if m.inputMode != inputNone {
			switch msg.String() {
			case "esc":
				m.inputMode = inputNone
				m.input = ""
			case "enter":
				return m.handleInputSubmit()
			case "tab", " ":
				if m.inputMode == inputSnapshot {
					m.snapshotVMState = !m.snapshotVMState
					return m, nil
				}
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
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.showConfig {
				m.showConfig = false
				m.message = ""
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			if m.viewMode == viewUsers {
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
		case "p":
			m.viewMode = viewUpdates
			m.message = "package updates"
		case "b":
			m.viewMode = viewBackups
			m.message = "backup jobs"
		case "f":
			m.nextFilter()
			m.selected = 0
		case "t":
			m.nextSort()
			m.selected = 0
		case "c":
			m.query = ""
			m.selected = 0
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "r":
			m.loading = true
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
				m.inputMode = inputSnapshot
				m.input = ""
				m.message = fmt.Sprintf("snapshot base name for %d, enter for manual", guest.VMID)
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
		m.data = snapshot(msg)
		if m.selected >= len(m.filteredGuests()) {
			m.selected = len(m.filteredGuests()) - 1
		}
		if m.selected < 0 {
			m.selected = 0
		}
		if m.selectedUser >= len(m.data.users) {
			m.selectedUser = len(m.data.users) - 1
		}
		if m.selectedUser < 0 {
			m.selectedUser = 0
		}
	case tickMsg:
		if !m.loading {
			m.loading = true
			return m, tea.Batch(m.load, tick())
		}
		return m, tick()
	case snapshotsMsg:
		if msg.vmid == m.snapshotVMID {
			m.snapshots = msg.snapshots
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

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
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

	users, err := m.client.Users(ctx)
	if err == nil {
		s.users = users
	}
	updates, err := m.client.PackageUpdates(ctx)
	if err == nil {
		s.updates = updates
	} else {
		s.updateErr = err
	}
	backups, err := m.client.BackupJobs(ctx)
	if err == nil {
		s.backups = backups
	} else {
		s.backupErr = err
	}

	return loadMsg(s)
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
		err := m.client.CreateSnapshot(ctx, vmid, name, "Created from pvm TUI", m.snapshotVMState)
		return createSnapshotMsg{vmid: vmid, name: name, err: err}
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

func (m Model) handleInputSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	mode := m.inputMode
	m.inputMode = inputNone
	m.input = ""

	switch mode {
	case inputSearch:
		m.query = input
		m.selected = 0
	case inputSnapshot:
		guest, ok := m.selectedGuest()
		if !ok {
			return m, nil
		}
		if input == "" {
			input = "manual-" + time.Now().Format("20060102-150405")
		}
		name := input
		if err := validate.SnapshotName(name); err != nil {
			m.message = err.Error()
			return m, nil
		}
		m.message = fmt.Sprintf("creating snapshot %s for %d", name, guest.VMID)
		return m, m.createSnapshot(guest.VMID, name)
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
	title := "pvm"
	node := m.data.node.Name
	if node == "" {
		node = "loading"
	}
	meta := strings.TrimSpace(m.data.node.ProxmoxVersion)
	if meta == "" {
		meta = "Proxmox"
	}
	meta += fmt.Sprintf(" | view:%s filter:%s sort:%s updates:%d", m.viewMode, m.filter, m.sortBy, len(m.data.updates))
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
		b.WriteString(mutedStyle.Render("loading..."))
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
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render("USERS"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-28s %-7s %-20s", "USERID", "ENABLED", "EMAIL")))
	b.WriteString("\n")

	if len(m.data.users) == 0 {
		b.WriteString(mutedStyle.Render("no users loaded"))
	} else {
		rows := max(1, height-5)
		start := 0
		if m.selectedUser >= rows {
			start = m.selectedUser - rows + 1
		}
		end := min(len(m.data.users), start+rows)
		for i := start; i < end; i++ {
			user := m.data.users[i]
			line := fmt.Sprintf("%-28s %-7t %-20s", truncate(user.UserID, 28), bool(user.Enable), truncate(user.Email, 20))
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
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(fmt.Sprintf("APT UPDATES (%d)", len(m.data.updates))))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-28s %-18s %-18s", "PACKAGE", "CURRENT", "CANDIDATE")))
	b.WriteString("\n")
	if m.data.updateErr != nil {
		b.WriteString(errorStyle.Render(m.data.updateErr.Error()))
	} else if len(m.data.updates) == 0 {
		b.WriteString(okStyle.Render("system up to date"))
	} else {
		rows := max(1, height-5)
		for i := 0; i < min(len(m.data.updates), rows); i++ {
			update := m.data.updates[i]
			line := fmt.Sprintf("%-28s %-18s %-18s", truncate(update.Name, 28), truncate(update.Current, 18), truncate(update.Candidate, 18))
			b.WriteString(rowStyle.Width(width - 4).Render(line))
			b.WriteString("\n")
		}
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
}

func (m Model) backupsPanel(width int, height int) string {
	var b strings.Builder
	b.WriteString(panelTitleStyle.Render(fmt.Sprintf("BACKUP JOBS (%d)", len(m.data.backups))))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-18s %-14s %-18s %-8s %-18s", "ID", "STORAGE", "SCHEDULE", "ENABLED", "VMIDS")))
	b.WriteString("\n")
	if m.data.backupErr != nil {
		b.WriteString(errorStyle.Render(m.data.backupErr.Error()))
	} else if len(m.data.backups) == 0 {
		b.WriteString(mutedStyle.Render("no backup jobs"))
	} else {
		rows := max(1, height-5)
		for i := 0; i < min(len(m.data.backups), rows); i++ {
			job := m.data.backups[i]
			line := fmt.Sprintf("%-18s %-14s %-18s %-8t %-18s", truncate(job.ID, 18), truncate(job.Storage, 14), truncate(job.Schedule, 18), bool(job.Enabled), truncate(job.VMID, 18))
			b.WriteString(rowStyle.Width(width - 4).Render(line))
			b.WriteString("\n")
		}
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
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
	if m.showConfig {
		return m.configPanel(width, height)
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
			for _, snapshot := range m.snapshots {
				b.WriteString(truncate(snapshot.Name, max(8, width-8)))
				b.WriteString("\n")
			}
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
		b.WriteString(kv("Action", "apt update && apt upgrade"))
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
	b.WriteString(kv("Source", "/cluster/backup"))
	if m.message != "" {
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(m.message))
		b.WriteString("\n")
	}
	return panelStyle.Width(width).Height(height).Render(b.String())
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
	if m.viewMode == viewUsers {
		next := m.selectedUser + delta
		if next >= 0 && next < len(m.data.users) {
			m.selectedUser = next
		}
		return
	}

	next := m.selected + delta
	if next >= 0 && next < len(m.filteredGuests()) {
		m.selected = next
	}
}

func (m Model) selectedUserModel() (proxmox.User, bool) {
	if len(m.data.users) == 0 || m.selectedUser >= len(m.data.users) {
		return proxmox.User{}, false
	}
	return m.data.users[m.selectedUser], true
}

func (m Model) footerText() string {
	if m.inputMode == inputSnapshot {
		ram := "on"
		if !m.snapshotVMState {
			ram = "off"
		}
		return "snapshot name: " + m.input + "  ram:" + ram + "  tab toggle-ram  enter create  esc cancel"
	}
	if m.inputMode == inputDisableTFA {
		return "confirm userid: " + m.input + "  enter disable TFA  esc cancel"
	}
	if m.inputMode == inputSearch {
		return "search: " + m.input + "  enter apply  esc cancel"
	}
	if m.viewMode == viewUsers {
		return "u guests  p updates  b backups  s load-tfa  x disable-tfa  r refresh  q quit"
	}
	if m.viewMode == viewUpdates {
		return "u users  b backups  r refresh  q quit"
	}
	if m.viewMode == viewBackups {
		return "u users  p updates  r refresh  q quit"
	}
	return "enter config  u users  p updates  b backups  r refresh  / search  f filter  t sort  s snapshots  n new-snapshot  q quit"
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
