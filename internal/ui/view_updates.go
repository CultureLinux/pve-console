package ui

import (
	"fmt"
)

func (m Model) updatesPanel(width int, height int) string {
	updates := m.filteredUpdates()
	inner := panelInner(width)

	meta := fmt.Sprintf("%d", len(updates))
	if len(updates) != len(m.data.updates) {
		meta = fmt.Sprintf("%d of %d", len(updates), len(m.data.updates))
	}

	const versionW = 16
	showVersions := inner >= 60
	nameW := max(8, inner-1-2)
	head := "  " + col("PACKAGE", nameW)
	if showVersions {
		nameW = inner - 1 - 2 - 2*(versionW+1)
		head = "  " + col("PACKAGE", nameW) + " " + col("CURRENT", versionW) + " " + col("CANDIDATE", versionW)
	}
	lines := tableHeader(head, inner)

	switch {
	case m.data.updateErr != nil:
		lines = append(lines, styleDanger.Render(m.data.updateErr.Error()))
	case m.loading && len(m.data.updates) == 0:
		lines = append(lines, loading("checking apt"))
	case len(m.data.updates) == 0:
		lines = append(lines, styleOK.Render(glyphOK+" System is up to date."))
	case len(updates) == 0:
		lines = append(lines, styleMuted.Render("No package matches the search."))
	default:
		start, end := window(0, len(updates), panelRows(height)-len(lines))
		for i := start; i < end; i++ {
			update := updates[i]
			segs := []seg{{"  ", plain}, {col(update.Name, nameW), plain}}
			if showVersions {
				segs = []seg{
					{"  ", plain},
					{col(update.Name, nameW) + " ", plain},
					{col(update.Current, versionW) + " ", styleMuted},
					{col(update.Candidate, versionW), styleOK},
				}
			}
			lines = append(lines, row(segs, inner, false))
		}
		if hidden := len(updates) - (end - start); hidden > 0 {
			lines = append(lines, styleMuted.Render(fmt.Sprintf("  … %d more", hidden)))
		}
	}

	return panel("APT updates", styleMuted.Render(meta), lines, width, height, true)
}

func (m Model) updatesDetailPanel(width int, height int) string {
	var lines []string
	switch {
	case m.data.updateErr != nil:
		lines = append(lines, styleDanger.Render("Unable to read the package list."))
	case len(m.data.updates) == 0:
		lines = append(lines,
			styleOK.Render(glyphOK+" Everything is up to date."),
			"",
			styleMuted.Render("Press a to refresh the package index"),
			styleMuted.Render("and check again."),
		)
	default:
		lines = append(lines,
			styleWarn.Render(fmt.Sprintf("%d package updates available", len(m.data.updates))),
			"",
			styleMuted.Render("Press a: the package index is refreshed,"),
			styleMuted.Render("then you confirm the apt dist-upgrade."),
			styleMuted.Render("Existing configuration files are kept."),
		)
	}
	return panel("Summary", "", lines, width, height, false)
}
