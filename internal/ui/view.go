package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	// minWidth and minHeight are the smallest terminal the layout supports.
	minWidth  = 60
	minHeight = 14
	// detailMinWidth is the width from which the detail panel is shown next to the list.
	detailMinWidth = 100
	headerRows     = 2
	footerRows     = 2
)

// View renders the whole screen: header, panels, footer and the open modal.
func (m Model) View() string {
	width, height := m.size()
	if width < minWidth || height < minHeight {
		return tooSmall(width, height)
	}

	bodyHeight := height - headerRows - footerRows
	base := lipgloss.JoinVertical(lipgloss.Left, m.header(width), m.body(width, bodyHeight), m.footer(width))

	switch {
	case m.modalMode != modalNone:
		return overlay(base, m.modal(width), width, height)
	case m.showConfig:
		return overlay(base, m.configPanel(min(90, max(56, width-8)), max(12, height-6)), width, height)
	}
	return overlay(base, "", width, height)
}

// size returns the terminal size, with defaults before the first resize message.
func (m Model) size() (int, int) {
	width, height := m.width, m.height
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 34
	}
	return width, height
}

func tooSmall(width int, height int) string {
	text := fmt.Sprintf("Terminal too small: %dx%d, need at least %dx%d", width, height, minWidth, minHeight)
	return styleWarn.Render(ellipsis(text, max(1, width)))
}

// body lays out the list and, on wide terminals, the detail next to it.
func (m Model) body(width int, height int) string {
	if m.viewMode == viewLog {
		// The detail is what this view is for, and it holds long lines (commands,
		// task ids): list above, detail below, both at full width.
		return m.logStacked(width, height)
	}
	if width < detailMinWidth {
		return m.leftPanel(width, height)
	}
	leftWidth := width * 58 / 100
	rightWidth := width - leftWidth - 1
	return lipgloss.JoinHorizontal(lipgloss.Top, m.leftPanel(leftWidth, height), " ", m.detailPanel(rightWidth, height))
}

func (m Model) leftPanel(width int, height int) string {
	switch m.viewMode {
	case viewUsers:
		return m.userPanel(width, height)
	case viewUpdates:
		return m.updatesPanel(width, height)
	case viewBackups:
		return m.backupsPanel(width, height)
	}
	return m.guestPanel(width, height)
}

// --- header ---

// tab is one entry of the view switcher.
type tab struct {
	key   string
	label string
	view  string
	count string
}

func (m Model) tabs() []tab {
	count := func(n int) string {
		if n > 0 {
			return fmt.Sprintf("%d", n)
		}
		return ""
	}
	return []tab{
		{"g", "Guests", viewGuests, count(len(m.data.guests))},
		{"b", "Backups", viewBackups, count(len(m.data.backupVolumes))},
		{"u", "Users", viewUsers, count(len(m.data.users))},
		{"p", "Updates", viewUpdates, count(len(m.data.updates))},
		{"L", "Log", viewLog, count(len(m.entries))},
	}
}

// header renders the two header rows: identity and counters, then the view tabs
// and the active filters.
func (m Model) header(width int) string {
	return m.headerTop(width) + "\n" + m.headerTabs(width)
}

func (m Model) headerTop(width int) string {
	node := m.data.node.Name
	if node == "" {
		node = "connecting"
	}
	left := styleBrandBadge.Render("pve-console")
	if m.demo {
		left += " " + lipgloss.NewStyle().Bold(true).Foreground(onColor(colWarn)).Background(colWarn).Padding(0, 1).Render("DEMO")
	}
	left += " " + styleBold.Render(node)
	if version := proxmoxVersion(m.data.node.ProxmoxVersion); version != "" {
		left += styleMuted.Render("  " + glyphSep + "  Proxmox VE " + version)
	}

	running, stopped := 0, 0
	for _, guest := range m.data.guests {
		if guest.Status == "running" {
			running++
		} else {
			stopped++
		}
	}
	var activity string
	switch {
	case len(m.tasks) == 1:
		activity = spinner() + " " + styleMuted.Render("1 task")
	case len(m.tasks) > 1:
		activity = spinner() + " " + styleMuted.Render(fmt.Sprintf("%d tasks", len(m.tasks)))
	case m.loading:
		activity = spinner() + " " + styleMuted.Render("refreshing")
	}

	var tiers []string
	if len(m.data.guests) > 0 {
		full := styleOK.Render(glyphDotOn) + fmt.Sprintf(" %d running  ", running) + styleMuted.Render(fmt.Sprintf("%s %d stopped", glyphDotOff, stopped))
		short := styleOK.Render(glyphDotOn) + fmt.Sprintf(" %d  ", running) + styleMuted.Render(fmt.Sprintf("%s %d", glyphDotOff, stopped))
		tiers = []string{full, short}
	}
	for i, tier := range tiers {
		if activity != "" {
			tier += "   " + activity
		}
		tiers[i] = tier
	}
	if len(tiers) == 0 {
		tiers = []string{activity}
	}

	// The version and the update mark go in front of the counters. The narrower the
	// screen, the more is dropped, in this order: the version, then the full counters
	// (kept short), and last the mark of a new version, which matters more than them.
	tag := ""
	if m.versionText != "" {
		tag = styleMuted.Render(m.versionText)
	}
	indicator := m.updateIndicator()
	var candidates []string
	if indicator != "" {
		both := joinNonEmpty("  ", tag, indicator)
		for _, tier := range tiers {
			candidates = append(candidates, joinNonEmpty("   ", both, tier), joinNonEmpty("   ", indicator, tier))
		}
		candidates = append(candidates, tiers...)
	} else {
		for _, tier := range tiers {
			candidates = append(candidates, joinNonEmpty("   ", tag, tier), tier)
		}
	}
	for _, right := range candidates {
		if right != "" && textWidth(left)+2+textWidth(right) <= width {
			return rightAlign(left, right, width)
		}
	}
	return fit(left, width)
}

// joinNonEmpty joins the parts that are not empty.
func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}

func (m Model) headerTabs(width int) string {
	var parts []string
	for _, t := range m.tabs() {
		parts = append(parts, m.renderTab(t))
	}
	left := strings.Join(parts, "")
	if filters := m.activeFilters(); filters != "" && textWidth(left)+2+textWidth(filters) <= width {
		return rightAlign(left, filters, width)
	}
	return fit(left, width)
}

func (m Model) renderTab(t tab) string {
	if t.view == m.viewMode {
		text := t.key + " " + t.label
		if t.count != "" {
			text += " " + t.count
		}
		return styleTabActive.Render(text)
	}
	text := styleKey.Render(t.key) + " " + styleMuted.Render(t.label)
	if t.count != "" {
		counter := styleMuted
		if t.view == viewUpdates {
			counter = styleWarn
		}
		if t.view == viewLog && m.unreadErrors > 0 {
			counter = styleDanger.Bold(true)
			t.count = fmt.Sprintf("%d", m.unreadErrors)
		}
		text += " " + counter.Render(t.count)
	}
	return styleTabIdle.Render(text)
}

// activeFilters describes the filters that differ from the defaults.
func (m Model) activeFilters() string {
	var parts []string
	if m.viewMode == viewGuests {
		if m.filter != "all" {
			parts = append(parts, styleMuted.Render("status ")+m.filter)
		}
		if m.guestTypeFilter != "all" {
			parts = append(parts, styleMuted.Render("type ")+m.guestTypeFilter)
		}
		if m.sortBy != "id" {
			parts = append(parts, styleMuted.Render("sort ")+m.sortBy)
		}
	}
	if m.query != "" {
		parts = append(parts, styleMuted.Render("search ")+styleAccent.Render(m.query))
	}
	return strings.Join(parts, styleMuted.Render("  "+glyphSep+"  "))
}

// proxmoxVersion extracts "9.0.3" from "pve-manager/9.0.3/abcdef" or "9.0.3".
func proxmoxVersion(raw string) string {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "pve-manager/")
	version, _, _ := strings.Cut(raw, "/")
	return version
}

// --- footer ---

// hint is a key and what it does.
type hint struct {
	key   string
	label string
}

// footer renders the status row and the hint row.
func (m Model) footer(width int) string {
	return m.statusRow(width) + "\n" + m.hintRow(width)
}

// statusRow shows the last message with an icon and a color for its level. The
// row is always there, so the layout does not jump when a message appears.
func (m Model) statusRow(width int) string {
	if m.message == "" {
		return strings.Repeat(" ", width)
	}
	level := messageLevel(m.message)
	icon, style := messageStyle(level)
	right := ""
	if level == levelError && len(m.entries) > 0 && m.viewMode != viewLog {
		right = hintKey("L", "details") + " "
	}
	message := oneLine(m.message)
	room := max(1, width-4-textWidth(right))
	return rightAlign(" "+style.Render(icon)+" "+style.Render(ellipsis(message, room)), right, width)
}

// hintRow shows the search prompt, or the hints for what is on screen.
func (m Model) hintRow(width int) string {
	if m.inputMode == inputSearch {
		prompt := styleKey.Render("/") + " " + m.input + styleKey.Render(glyphCursor)
		right := hintKey("enter", "apply") + "  " + hintKey("esc", "cancel")
		return rightAlign(" "+prompt, right+" ", width)
	}
	hints, tail := m.hints()
	return fit(" "+fitHints(hints, tail, width-2), width)
}

// fitHints renders as many hints as fit in width columns. The tail (help, quit)
// is always kept; the hints that do not fit are dropped from the end.
func fitHints(hints []hint, tail []hint, width int) string {
	render := func(list []hint) []string {
		parts := make([]string, len(list))
		for i, h := range list {
			parts[i] = hintKey(h.key, h.label)
		}
		return parts
	}
	sep := "   "
	tailText := strings.Join(render(tail), sep)
	var used []string
	total := textWidth(tailText)
	for _, part := range render(hints) {
		next := total + textWidth(part) + len(sep)
		if next > width {
			break
		}
		used = append(used, part)
		total = next
	}
	if tailText != "" {
		used = append(used, tailText)
	}
	return strings.Join(used, sep)
}

var tailHints = []hint{{"?", "help"}, {"q", "quit"}}

// hints returns the hints of the current screen, most useful first, and the
// ones that always stay.
func (m Model) hints() ([]hint, []hint) {
	hints, tail := m.baseHints()
	// A new version is announced on every screen, next to help and quit.
	if m.modalMode == modalNone && (m.updateAvailable() || m.upd.phase == updateInstalled) {
		tail = append([]hint{{"U", "update"}}, tail...)
	}
	return hints, tail
}

func (m Model) baseHints() ([]hint, []hint) {
	switch m.modalMode {
	case modalStartup:
		return []hint{{"enter", "continue"}, {"esc", "close"}}, nil
	case modalHelp:
		return []hint{{"esc", "close"}}, nil
	case modalConfirm:
		return []hint{{"enter", "confirm"}, {"esc", "cancel"}}, nil
	case modalUpdate:
		return m.updateHints(), nil
	case modalEnroll:
		return m.enrollHints(), nil
	case modalPasswd:
		return m.passwdHints(), nil
	case modalUpgrade:
		if m.upgradeRunning {
			return []hint{{"…", "upgrade running"}}, nil
		}
		return []hint{{"enter", "close"}}, nil
	case modalSnapshot:
		return []hint{{"tab", "next"}, {"enter", "apply"}, {"esc", "cancel"}}, nil
	case modalBackup:
		return []hint{{"←→", "storage"}, {"tab", "next"}, {"enter", "apply"}, {"esc", "cancel"}}, nil
	case modalSchedule:
		return []hint{{"↑↓", "move"}, {"space", "toggle guest"}, {"tab", "next"}, {"enter", "apply"}, {"esc", "cancel"}}, nil
	}
	if m.showConfig {
		return []hint{{"↑↓", "scroll"}, {"esc", "close"}}, nil
	}

	switch m.viewMode {
	case viewUsers:
		return []hint{{"↑↓", "move"}, {"s", "show TFA"}, {"n", "enroll TOTP"}, {"P", "password"}, {"x", "disable TFA"}, {"/", "search"}, {"r", "refresh"}}, tailHints
	case viewUpdates:
		// a refreshes the index first, so it is useful even when the list is empty.
		return []hint{{"a", "upgrade"}, {"/", "search"}, {"r", "refresh"}}, tailHints
	case viewLog:
		return []hint{{"↑↓", "move"}, {"tab", "source: " + m.logSourceLabel()}, {"f", "level: " + m.logLevel.label()}, {"/", "search"}, {"x", "clear log"}}, tailHints
	case viewBackups:
		if m.backupMode == "jobs" {
			return []hint{{"↑↓", "move"}, {"e", "edit schedule"}, {"m", "run backup"}, {"tab", "volumes"}, {"/", "search"}}, tailHints
		}
		return []hint{{"↑↓", "move"}, {"m", "run backup"}, {"o", "restore"}, {"d", "delete"}, {"tab", "jobs"}, {"/", "search"}}, tailHints
	}

	if m.showSnaps {
		return []hint{{"↑↓", "select"}, {"n", "new"}, {"o", "roll back"}, {"d", "delete"}, {"esc", "close"}}, tailHints
	}
	hints := []hint{{"↑↓", "move"}, {"enter", "config"}}
	if guest, ok := m.selectedGuest(); ok && guest.Status == "running" {
		hints = append(hints, hint{"-", "stop"}, hint{"S", "shutdown"}, hint{"R", "reboot"})
	} else {
		hints = append(hints, hint{"+", "start"})
	}
	hints = append(hints, hint{"s", "snapshots"}, hint{"m", "backup"}, hint{"/", "search"}, hint{"f", "filter"}, hint{"tab", "type"})
	return hints, tailHints
}

// footerText is the plain text of the hints, for tests and for logs.
func (m Model) footerText() string {
	if m.inputMode == inputSearch {
		return "/ " + m.input + "  enter apply  esc cancel"
	}
	hints, tail := m.hints()
	var parts []string
	for _, h := range append(hints, tail...) {
		parts = append(parts, h.key+" "+h.label)
	}
	return strings.Join(parts, "  ")
}

// --- messages ---

type level int

const (
	levelInfo level = iota
	levelOK
	levelWarn
	levelError
)

// messageLevel guesses the severity of a status message from its text. The
// messages are produced all over the model, so the rule lives in one place.
func messageLevel(message string) level {
	text := strings.ToLower(message)
	contains := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(text, word) {
				return true
			}
		}
		return false
	}
	switch {
	case contains("failed", "error", "unable", "invalid", "lost track", "mismatch", "refus", "denied", "not found", "cannot", "is required", "are required"):
		return levelError
	case contains("not running", "still running", "reboot required", "update available"):
		return levelWarn
	case contains("completed", ": ok", "created", "started", "loaded", "disabled", "scheduled", "requested", "updated to"):
		return levelOK
	}
	return levelInfo
}

func messageStyle(l level) (string, lipgloss.Style) {
	switch l {
	case levelOK:
		return glyphOK, styleOK
	case levelWarn:
		return glyphWarn, styleWarn
	case levelError:
		return glyphFail, styleDanger
	}
	return glyphInfo, styleMuted
}

// kv renders a label and its value on one line of a detail panel.
func kv(key string, value string) string {
	return styleMuted.Render(fmt.Sprintf("%-9s", key)) + value
}
