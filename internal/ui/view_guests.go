package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pvc/internal/proxmox"
)

var plain = lipgloss.NewStyle()

// guestTypeLabel is the short type shown in the list.
func guestTypeLabel(typ proxmox.GuestType) string {
	if typ == proxmox.GuestTypeLXC {
		return "CT"
	}
	return "VM"
}

func (m Model) guestPanel(width int, height int) string {
	guests := m.filteredGuests()
	inner := panelInner(width)

	meta := fmt.Sprintf("%d", len(guests))
	if len(guests) != len(m.data.guests) {
		meta = fmt.Sprintf("%d of %d", len(guests), len(m.data.guests))
	}

	// Columns: dot, id, name (flexible), type, status, and cpu/mem when there is room.
	const idW, typeW, statusW, usageW = 5, 4, 8, 4
	showUsage := inner >= 62
	fixed := 1 + 2 + idW + 1 + 1 + typeW + 1 + statusW
	if showUsage {
		fixed += 2 * (1 + usageW)
	}
	nameW := max(6, inner-fixed)

	head := "  " + col("ID", idW) + " " + col("NAME", nameW) + " " + col("TYPE", typeW) + " " + col("STATUS", statusW)
	if showUsage {
		head += " " + right("CPU", usageW) + " " + right("MEM", usageW)
	}
	lines := tableHeader(head, inner)

	switch {
	case m.data.err != nil:
		lines = append(lines, styleDanger.Render(m.data.err.Error()))
	case m.loading && len(m.data.guests) == 0:
		lines = append(lines, loading("scanning guests"))
	case len(guests) == 0 && len(m.data.guests) == 0:
		lines = append(lines, styleMuted.Render("No guests found on this node."))
	case len(guests) == 0:
		lines = append(lines, styleMuted.Render("No guest matches the current filters."), styleMuted.Render("Press c to clear the search, f to change the status filter."))
	default:
		start, end := window(m.selected, len(guests), panelRows(height)-len(lines))
		for i := start; i < end; i++ {
			guest := guests[i]
			dot, dotStyle := statusDot(guest.Status)
			segs := []seg{
				{dot + " ", dotStyle},
				{col(fmt.Sprintf("%d", guest.VMID), idW) + " ", styleMuted},
				{col(guest.Name, nameW) + " ", plain},
				{col(guestTypeLabel(guest.Type), typeW) + " ", styleMuted},
				{col(guest.Status, statusW), dotStyle},
			}
			if showUsage {
				cpu, mem := "-", "-"
				if guest.Status == "running" {
					cpu = percent(guest.CPU)
					mem = percent(ratio(float64(guest.Memory), float64(guest.MaxMem)))
				}
				segs = append(segs, seg{" " + right(cpu, usageW) + " " + right(mem, usageW), styleMuted})
			}
			lines = append(lines, row(segs, inner, i == m.selected))
		}
	}

	return panel("Guests", styleMuted.Render(meta), lines, width, height, true)
}

func (m Model) detailPanel(width int, height int) string {
	switch m.viewMode {
	case viewUsers:
		return m.userDetailPanel(width, height)
	case viewTokens:
		return m.tokenDetailPanel(width, height)
	case viewUpdates:
		return m.updatesDetailPanel(width, height)
	case viewBackups:
		return m.backupsDetailPanel(width, height)
	}

	inner := panelInner(width)
	title, meta := "Details", ""
	var lines []string

	if guest, ok := m.selectedGuest(); ok {
		title, meta = guest.Name, styleMuted.Render(fmt.Sprintf("%d", guest.VMID))
		dot, dotStyle := statusDot(guest.Status)
		kind := "QEMU VM"
		if guest.Type == proxmox.GuestTypeLXC {
			kind = "LXC container"
		}
		lines = append(lines, dotStyle.Render(dot)+" "+dotStyle.Render(guest.Status)+styleMuted.Render("  "+glyphSep+"  "+kind))
		if line := m.guestNetLine(guest); line != "" {
			lines = append(lines, line)
		}
		lines = append(lines, "")

		barW := max(8, inner-9-5)
		cpu, mem := 0.0, 0.0
		if guest.Status == "running" {
			cpu = guest.CPU
			mem = ratio(float64(guest.Memory), float64(guest.MaxMem))
		}
		lines = append(lines, kv("CPU", bar(cpu, barW)+" "+percent(cpu)))
		lines = append(lines, kv("Memory", bar(mem, barW)+" "+percent(mem)))
		lines = append(lines, kv("", styleMuted.Render(bytesPair(guest.Memory, guest.MaxMem))))
		lines = append(lines, kv("Uptime", durationLabel(guest.Uptime)))
	} else {
		lines = append(lines, styleMuted.Render("Select a guest to see its details."))
	}

	if len(m.data.storages) > 0 {
		lines = append(lines, "", styleSection.Render("STORAGE"))
		nameW := min(14, max(8, inner/3))
		barW := max(6, inner-nameW-1-5-1)
		for _, storage := range m.data.storages {
			if !bool(storage.Active) {
				lines = append(lines, col(storage.Name, nameW)+" "+styleDanger.Render("inactive"))
				continue
			}
			lines = append(lines, col(storage.Name, nameW)+" "+bar(storage.Usage, barW)+" "+percent(storage.Usage))
		}
	}

	if m.showSnaps {
		lines = append(lines, "", styleSection.Render(fmt.Sprintf("SNAPSHOTS %s %d", glyphSep, m.snapshotVMID)))
		switch {
		case m.snapErr != nil:
			lines = append(lines, styleDanger.Render(m.snapErr.Error()))
		case len(m.snapshots) == 0:
			lines = append(lines, styleMuted.Render("No snapshot loaded."))
		default:
			for i, snapshot := range m.snapshots {
				date := ""
				if snapshot.SnapTime > 0 {
					date = timeLabel(snapshot.SnapTime)
				}
				nameW := max(8, inner-1-1-16)
				lines = append(lines, row([]seg{{col(snapshot.Name, nameW), plain}, {" " + date, styleMuted}}, inner, i == m.selectedSnapshot))
			}
		}
	}

	return panel(title, meta, lines, width, height, false)
}

// configPanel shows the configuration of a guest. It scrolls with the arrow keys.
func (m Model) configPanel(width int, height int) string {
	title := fmt.Sprintf("Configuration %s %d", glyphSep, m.configVMID)
	inner := panelInner(width)

	if m.configErr != nil {
		return panel(title, "", []string{styleDanger.Render(m.configErr.Error())}, width, height, true)
	}
	if m.config == nil {
		return panel(title, "", []string{loading("reading configuration")}, width, height, true)
	}

	var lines []string
	lines = appendConfigSection(lines, "HARDWARE", m.config, []string{
		"cores", "sockets", "cpu", "memory", "balloon",
		"scsihw", "bios", "machine", "ostype", "agent",
	}, inner)
	lines = appendConfigPrefixes(lines, "DISKS", m.config, []string{
		"scsi", "virtio", "sata", "ide", "efidisk", "tpmstate", "rootfs", "mp",
	}, inner)
	lines = appendConfigPrefixes(lines, "NETWORK", m.config, []string{"net"}, inner)
	lines = appendConfigSection(lines, "OPTIONS", m.config, remainingConfigKeys(m.config), inner)
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	lines, _, meta := scrollWindow(lines, panelRows(height), m.configScroll)
	return panel(title, meta, lines, width, height, true)
}

func appendConfigSection(lines []string, title string, config proxmox.GuestConfig, keys []string, inner int) []string {
	wroteTitle := false
	for _, key := range keys {
		value, ok := config[key]
		if !ok {
			continue
		}
		if !wroteTitle {
			lines = append(lines, styleSection.Render(title))
			wroteTitle = true
		}
		lines = append(lines, styleMuted.Render(col(truncate(key, 10), 11))+ellipsis(fmt.Sprint(value), max(8, inner-11)))
	}
	if wroteTitle {
		lines = append(lines, "")
	}
	return lines
}

func appendConfigPrefixes(lines []string, title string, config proxmox.GuestConfig, prefixes []string, inner int) []string {
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
	return appendConfigSection(lines, title, config, keys, inner)
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
