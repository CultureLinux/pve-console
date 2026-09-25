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
)

type snapshot struct {
	node     proxmox.NodeInfo
	guests   []proxmox.Guest
	storages []proxmox.Storage
	err      error
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

type Model struct {
	client       proxmox.Client
	width        int
	height       int
	selected     int
	loading      bool
	searching    bool
	query        string
	filter       string
	sortBy       string
	showSnaps    bool
	snapshotVMID int
	snapshots    []proxmox.Snapshot
	snapErr      error
	message      string
	data         snapshot
}

func New(client proxmox.Client) Model {
	return Model{client: client, loading: true, filter: "all", sortBy: "id"}
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
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
			case "enter":
				m.searching = false
				m.selected = 0
			case "backspace":
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
					m.selected = 0
				}
			default:
				if len(msg.String()) == 1 {
					m.query += msg.String()
					m.selected = 0
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "/":
			m.searching = true
			m.message = ""
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
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.filteredGuests())-1 {
				m.selected++
			}
		case "r":
			m.loading = true
			return m, m.load
		case "s":
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
			guest, ok := m.selectedGuest()
			if ok {
				name := "manual-" + time.Now().Format("20060102-150405")
				m.message = fmt.Sprintf("creating snapshot %s for %d", name, guest.VMID)
				return m, m.createSnapshot(guest.VMID, name)
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
	left := m.guestPanel(leftWidth, bodyHeight)
	right := m.detailPanel(rightWidth, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	footer := footerStyle.Width(contentWidth).Render("q quit  r refresh  / search  f filter  t sort  s snapshots  n new-snapshot")

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
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
		err := m.client.CreateSnapshot(ctx, vmid, name, "Created from pvm TUI")
		return createSnapshotMsg{vmid: vmid, name: name, err: err}
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
	meta += fmt.Sprintf(" | filter:%s sort:%s", m.filter, m.sortBy)
	if m.query != "" {
		meta += " | search:" + m.query
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

func (m Model) detailPanel(width int, height int) string {
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
