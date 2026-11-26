package ui

import tea "github.com/charmbracelet/bubbletea"

// hasScrollableDetail reports whether the detail panel of the current view can
// take the focus, so that its content can be scrolled. The detail of a view
// whose content is always short does not need it.
func (m Model) hasScrollableDetail() bool {
	_, _, split := splitWidths(m.widthOrDefault())
	return split && m.viewMode == viewTokens
}

func (m Model) widthOrDefault() int {
	width, _ := m.size()
	return width
}

// detailLineCount is the number of lines of the detail of the current view, and
// the number of lines that are visible at once.
func (m Model) detailLineCount() (total int, visible int) {
	width, height := m.size()
	_, right, _ := splitWidths(width)
	visible = panelRows(height - headerRows - footerRows)
	if m.viewMode == viewTokens {
		_, lines := m.tokenDetailContent(panelInner(right))
		total = len(lines)
	}

	return total, visible
}

// scrollDetail moves the first visible line of the detail by delta lines.
func (m *Model) scrollDetail(delta int) {
	total, visible := m.detailLineCount()
	m.detailScroll = max(0, min(m.detailScroll+delta, total-visible))
}

// updateDetailKey handles a key press while the detail panel has the focus. It
// reports whether it used the key: the others (view tabs, quit, help) keep their
// usual meaning.
func (m *Model) updateDetailKey(msg tea.KeyMsg) bool {
	_, visible := m.detailLineCount()
	switch msg.String() {
	case "up", "k":
		m.scrollDetail(-1)
	case "down", "j":
		m.scrollDetail(1)
	case "pgup", "ctrl+u":
		m.scrollDetail(-max(1, visible-1))
	case "pgdown", "ctrl+d", " ":
		m.scrollDetail(max(1, visible-1))
	case "home":
		m.detailScroll = 0
	case "end":
		m.scrollDetail(1 << 20)
	case "esc", "left", "enter":
		m.detailFocus = false
	default:
		return false
	}

	return true
}

// keepFocusConsistent drops the focus of the detail panel when it no longer
// applies (another view, a narrow screen), and scrolls back to the top when the
// selected item changed.
func (m Model) keepFocusConsistent(before Model) Model {
	if !m.hasScrollableDetail() || m.viewMode != before.viewMode {
		m.detailFocus = false
	}
	if m.viewMode != before.viewMode || m.selectedToken != before.selectedToken {
		m.detailScroll = 0
	}

	return m
}
