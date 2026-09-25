package ui

import (
	"context"
	"fmt"
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

type Model struct {
	client   proxmox.Client
	width    int
	height   int
	selected int
	loading  bool
	data     snapshot
}

func New(client proxmox.Client) Model {
	return Model{client: client, loading: true}
}

func Run(client proxmox.Client) error {
	program := tea.NewProgram(New(client), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return m.load
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.data.guests)-1 {
				m.selected++
			}
		case "r":
			m.loading = true
			return m, m.load
		}
	case loadMsg:
		m.loading = false
		m.data = snapshot(msg)
		if m.selected >= len(m.data.guests) {
			m.selected = len(m.data.guests) - 1
		}
		if m.selected < 0 {
			m.selected = 0
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
	footer := footerStyle.Width(contentWidth).Render("q quit  r refresh  ↑/↓ or j/k select")

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
	if m.loading {
		meta += " · refreshing"
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
	} else if len(m.data.guests) == 0 {
		b.WriteString(mutedStyle.Render("no guests found"))
	} else {
		rows := max(1, height-5)
		start := 0
		if m.selected >= rows {
			start = m.selected - rows + 1
		}
		end := min(len(m.data.guests), start+rows)
		for i := start; i < end; i++ {
			guest := m.data.guests[i]
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

	if len(m.data.guests) > 0 && m.selected < len(m.data.guests) {
		guest := m.data.guests[m.selected]
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

	return panelStyle.Width(width).Height(height).Render(b.String())
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
