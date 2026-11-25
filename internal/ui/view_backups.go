package ui

import (
	"fmt"
	"strings"
)

// backupModeTabs renders the "Volumes | Jobs" switch shown in the panel title.
func (m Model) backupModeTabs(count int) string {
	volumes := styleMuted.Render("Volumes")
	jobs := styleMuted.Render("Jobs")
	active := styleAccent.Bold(true)
	if m.backupMode == "jobs" {
		jobs = active.Render(fmt.Sprintf("Jobs %d", count))
	} else {
		volumes = active.Render(fmt.Sprintf("Volumes %d", count))
	}
	return volumes + styleMuted.Render("  "+glyphSep+"  ") + jobs
}

func (m Model) backupsPanel(width int, height int) string {
	count := m.backupItemCount()
	inner := panelInner(width)
	meta := m.backupModeTabs(count)

	var head string
	if m.backupMode == "jobs" {
		head = "  " + col("ID", 18) + " " + col("STORAGE", 14) + " " + col("SCHEDULE", 16) + " " + col("VMIDS", 12)
	} else {
		head = "  " + col("VMID", 5) + " " + col("NAME", m.backupNameWidth(inner)) + " " + col("DATE", 16) + " " + col("SIZE", 10)
	}
	lines := tableHeader(head, inner)

	switch {
	case m.data.backupErr != nil:
		lines = append(lines, styleDanger.Render(m.data.backupErr.Error()))
	case m.loading && count == 0:
		lines = append(lines, loading("indexing backups"))
	case count == 0:
		lines = append(lines, styleMuted.Render("No backup found."))
		if m.backupMode != "jobs" {
			lines = append(lines, styleMuted.Render("Press m to run one, tab to see the jobs."))
		}
	default:
		start, end := window(m.selectedBackup, count, panelRows(height)-len(lines))
		for i := start; i < end; i++ {
			lines = append(lines, row(m.backupSegments(i, inner), inner, i == m.selectedBackup))
		}
	}

	return panel("Backups", meta, lines, width, height, true)
}

// backupNameWidth is the flexible width of the guest name column of the volumes table.
func (m Model) backupNameWidth(inner int) int {
	return max(8, inner-1-2-5-1-1-16-1-10)
}

// backupSegments are the cells of the backup line at index.
func (m Model) backupSegments(index int, inner int) []seg {
	if m.backupMode == "jobs" {
		job := m.filteredBackupJobs()[index]
		dot, dotStyle := glyphDotOn, styleOK
		if !bool(job.Enabled) {
			dot, dotStyle = glyphDotOff, styleMuted
		}
		return []seg{
			{dot + " ", dotStyle},
			{col(job.ID, 18) + " ", plain},
			{col(job.Storage, 14) + " ", styleMuted},
			{col(job.Schedule, 16) + " ", plain},
			{col(job.VMID, 12), styleMuted},
		}
	}
	volume := m.filteredBackupVolumes()[index]
	return []seg{
		{"  ", plain},
		{col(fmt.Sprintf("%d", volume.VMID), 5) + " ", styleMuted},
		{col(m.guestName(volume.VMID), m.backupNameWidth(inner)) + " ", plain},
		{col(timeLabel(volume.CTime), 16) + " ", styleMuted},
		{col(humanBytes(volume.Size), 10), plain},
	}
}

func (m Model) backupsDetailPanel(width int, height int) string {
	inner := panelInner(width)
	var lines []string

	if m.backupMode == "jobs" {
		job, ok := m.selectedBackupJob()
		if !ok {
			lines = append(lines, styleMuted.Render("Select a job to see its details."))
			return panel("Job", "", lines, width, height, false)
		}
		state, stateStyle := "enabled", styleOK
		if !bool(job.Enabled) {
			state, stateStyle = "disabled", styleMuted
		}
		lines = append(lines,
			stateStyle.Render(state), "",
			kv("Schedule", job.Schedule),
			kv("Storage", job.Storage),
			kv("Guests", ellipsis(job.VMID, inner-9)),
		)
		if job.Mode != "" {
			lines = append(lines, kv("Mode", job.Mode))
		}
		if job.Compress != "" {
			lines = append(lines, kv("Compress", job.Compress))
		}
		return panel(job.ID, "", lines, width, height, false)
	}

	volume, ok := m.selectedBackupVolume()
	if !ok {
		lines = append(lines,
			kv("Volumes", fmt.Sprintf("%d", len(m.data.backupVolumes))),
			kv("Jobs", fmt.Sprintf("%d", len(m.data.backups))),
		)
		return panel("Backups", "", lines, width, height, false)
	}
	lines = append(lines,
		kv("Guest", strings.TrimSpace(fmt.Sprintf("%d %s", volume.VMID, m.knownGuestName(volume.VMID)))),
		kv("Date", timeLabel(volume.CTime)),
		kv("Size", humanBytes(volume.Size)),
	)
	if volume.Format != "" {
		lines = append(lines, kv("Format", volume.Format))
	}
	if volume.Notes != "" {
		lines = append(lines, kv("Notes", ellipsis(volume.Notes, inner-9)))
	}
	lines = append(lines, "", styleSection.Render("VOLUME ID"), ellipsis(volume.VolID, inner))
	return panel("Backup", styleMuted.Render(fmt.Sprintf("%d", volume.VMID)), lines, width, height, false)
}
