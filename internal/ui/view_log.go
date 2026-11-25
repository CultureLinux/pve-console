package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const logTimeLayout = "2006-01-02 15:04:05"

// sourceStyle colors the source of an entry: amber for Proxmox, blue for pve-console.
func sourceStyle(source errSource) lipgloss.Style {
	if source == sourceProxmox {
		return styleWarn
	}
	return styleAccent
}

// logSourceLabel names the active source filter.
func (m Model) logSourceLabel() string {
	switch m.logSource {
	case sourceProxmox:
		return "proxmox"
	case sourceConsole:
		return "pve-console"
	}
	return "all"
}

// logSourceTabs renders "all | proxmox | pve-console" with the counts, the
// active filter highlighted.
func (m Model) logSourceTabs() string {
	proxmox, console := m.logCounts()
	items := []struct {
		label  string
		source errSource
		count  int
	}{
		{"all", 0, proxmox + console},
		{"proxmox", sourceProxmox, proxmox},
		{"pve-console", sourceConsole, console},
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		text := fmt.Sprintf("%s %d", item.label, item.count)
		if item.source == m.logSource {
			parts = append(parts, styleAccent.Bold(true).Render(text))
		} else {
			parts = append(parts, styleMuted.Render(text))
		}
	}
	tabs := strings.Join(parts, styleMuted.Render("  "+glyphSep+"  "))
	if m.logLevel != levelAll {
		tabs += styleMuted.Render("  " + glyphSep + "  only " + m.logLevel.label())
	}
	return tabs
}

func (m Model) logPanel(width int, height int) string {
	entries := m.filteredLog()
	inner := panelInner(width)

	timeW := len(logTimeLayout)
	timeLayout := logTimeLayout
	if inner < 62 {
		timeLayout, timeW = "15:04:05", 8
	}
	const sourceW = 11
	// glyphW is the room of the outcome mark (✓ or ✗) and its space.
	const glyphW = 2
	whatW := max(8, inner-1-timeW-1-sourceW-1-glyphW)

	head := col("TIME", timeW) + " " + col("SOURCE", sourceW) + " " + col("", glyphW) + col("WHAT", whatW)
	lines := tableHeader(head, inner)

	switch {
	case len(m.entries) == 0:
		lines = append(lines, styleMuted.Render("Nothing logged since pve-console started."))
	case len(entries) == 0:
		lines = append(lines, styleMuted.Render("No entry matches the filters or the search."), styleMuted.Render("Press tab to change the source, f the level, c to clear the search."))
	default:
		start, end := window(m.selectedLog, len(entries), panelRows(height)-len(lines))
		for i := start; i < end; i++ {
			entry := entries[i]
			what := entry.message
			if !strings.HasPrefix(entry.message, entry.context) {
				what = entry.context + ": " + entry.message
			}
			if entry.count > 1 {
				what += fmt.Sprintf(" ×%d", entry.count)
			}
			mark, markStyle := glyphFail, styleDanger
			if entry.ok {
				mark, markStyle = glyphOK, styleOK
			}
			lines = append(lines, row([]seg{
				{col(entry.time.Format(timeLayout), timeW) + " ", styleMuted},
				{col(entry.source.label(), sourceW) + " ", sourceStyle(entry.source)},
				{mark + " ", markStyle},
				{col(what, whatW), plain},
			}, inner, i == m.selectedLog))
		}
	}

	return panel("Log", m.logSourceTabs(), lines, width, height, true)
}

// selectedLogEntry returns the entry under the cursor.
func (m Model) selectedLogEntry() (logEntry, bool) {
	entries := m.filteredLog()
	if m.selectedLog < 0 || m.selectedLog >= len(entries) {
		return logEntry{}, false
	}
	return entries[m.selectedLog], true
}

// whoReported explains what the source of an entry means.
func whoReported(entry logEntry) string {
	switch {
	case entry.ok && entry.source == sourceProxmox:
		return "A Proxmox task ended well."
	case entry.ok:
		return "An action of yours ended well, run by pve-console."
	case entry.source == sourceProxmox && entry.kind == "task failed":
		return "A Proxmox task ended in error."
	case entry.source == sourceProxmox:
		return "Reported by Proxmox: a command run on the node failed."
	}
	return "Raised by pve-console itself, not by Proxmox."
}

func (m Model) logDetailPanel(width int, height int) string {
	inner := panelInner(width)
	entry, ok := m.selectedLogEntry()
	if !ok {
		return panel("Details", "", []string{styleMuted.Render("Select an entry to see what happened.")}, width, height, false)
	}

	style := sourceStyle(entry.source)
	mark := styleDanger.Render(glyphFail)
	if entry.ok {
		mark = styleOK.Render(glyphOK)
	}
	lines := []string{
		mark + " " + style.Render(entry.source.label()) + styleMuted.Render("  "+glyphSep+"  "+entry.kind),
		styleMuted.Render(whoReported(entry)),
		"",
	}
	when := entry.time.Format(logTimeLayout)
	if entry.count > 1 {
		when += fmt.Sprintf("  (%d times, last one)", entry.count)
	}
	lines = append(lines, kv("When", when))
	for i, part := range wrapText(entry.context, max(8, inner-9)) {
		label := "While"
		if i > 0 {
			label = ""
		}
		lines = append(lines, kv(label, part))
	}

	lines = append(lines, "", styleSection.Render("MESSAGE"))
	for _, part := range wrapText(entry.message, inner) {
		lines = append(lines, part)
	}
	if len(entry.detail) > 0 {
		lines = append(lines, "", styleSection.Render("DETAILS"))
		for _, detail := range entry.detail {
			if detail == "" {
				lines = append(lines, "")
				continue
			}
			for _, part := range wrapText(detail, inner) {
				lines = append(lines, styleMuted.Render(part))
			}
		}
	}

	if rows := panelRows(height); len(lines) > rows && rows > 1 {
		hidden := len(lines) - (rows - 1)
		lines = append(lines[:rows-1], styleMuted.Render(fmt.Sprintf("… %d more lines", hidden)))
	}
	return panel("Entry", styleMuted.Render(entry.time.Format(time.TimeOnly)), lines, width, height, false)
}

// logStacked shows the list above the detail, both at full width.
func (m Model) logStacked(width int, height int) string {
	listHeight := max(6, height*40/100)
	detailHeight := height - listHeight
	if detailHeight < 6 {
		return m.logPanel(width, height)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.logPanel(width, listHeight), m.logDetailPanel(width, detailHeight))
}
