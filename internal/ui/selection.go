package ui

import (
	"fmt"
	"sort"
	"strings"

	"pvc/internal/proxmox"
)

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
		if m.guestTypeFilter != "all" && string(guest.Type) != m.guestTypeFilter {
			continue
		}
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

func (m Model) filteredUsers() []proxmox.User {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.users
	}
	users := make([]proxmox.User, 0, len(m.data.users))
	for _, user := range m.data.users {
		values := []string{user.UserID, user.Email, user.FirstName, user.LastName, user.Comment}
		if containsAnyFold(values, query) {
			users = append(users, user)
		}
	}
	return users
}

func (m Model) filteredUpdates() []proxmox.PackageUpdate {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.updates
	}
	updates := make([]proxmox.PackageUpdate, 0, len(m.data.updates))
	for _, update := range m.data.updates {
		values := []string{update.Name, update.Current, update.Candidate, update.Suite}
		if containsAnyFold(values, query) {
			updates = append(updates, update)
		}
	}
	return updates
}

func (m Model) filteredBackupJobs() []proxmox.BackupJob {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.backups
	}
	jobs := make([]proxmox.BackupJob, 0, len(m.data.backups))
	for _, job := range m.data.backups {
		values := []string{job.ID, job.Storage, job.Schedule, job.VMID, job.Mode, job.Compress, fmt.Sprintf("%t", bool(job.Enabled))}
		if containsAnyFold(values, query) {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func (m Model) filteredBackupVolumes() []proxmox.BackupVolume {
	query := normalizedQuery(m.query)
	if query == "" {
		return m.data.backupVolumes
	}
	volumes := make([]proxmox.BackupVolume, 0, len(m.data.backupVolumes))
	for _, volume := range m.data.backupVolumes {
		values := []string{fmt.Sprintf("%d", volume.VMID), m.guestName(volume.VMID), volume.VolID, volume.Format, volume.Notes, timeLabel(volume.CTime), humanBytes(volume.Size)}
		if containsAnyFold(values, query) {
			volumes = append(volumes, volume)
		}
	}
	return volumes
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

func (m *Model) nextGuestTypeFilter() {
	switch m.guestTypeFilter {
	case "all":
		m.guestTypeFilter = string(proxmox.GuestTypeQEMU)
	case string(proxmox.GuestTypeQEMU):
		m.guestTypeFilter = string(proxmox.GuestTypeLXC)
	default:
		m.guestTypeFilter = "all"
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
	m.message = "users view: s load TFA, n enroll TOTP, P password, x disable TFA"
}

func (m *Model) moveSelection(delta int) {
	if m.viewMode == viewGuests && m.showSnaps {
		next := m.selectedSnapshot + delta
		if next >= 0 && next < len(m.snapshots) {
			m.selectedSnapshot = next
		}
		return
	}
	if m.viewMode == viewUsers {
		next := m.selectedUser + delta
		if next >= 0 && next < len(m.filteredUsers()) {
			m.selectedUser = next
		}
		return
	}
	if m.viewMode == viewLog {
		next := m.selectedLog + delta
		if next >= 0 && next < len(m.filteredLog()) {
			m.selectedLog = next
		}
		return
	}
	if m.viewMode == viewBackups {
		next := m.selectedBackup + delta
		if next >= 0 && next < m.backupItemCount() {
			m.selectedBackup = next
		}
		return
	}

	next := m.selected + delta
	if next >= 0 && next < len(m.filteredGuests()) {
		m.selected = next
	}
}

func (m *Model) resetActiveSelection() {
	switch m.viewMode {
	case viewUsers:
		m.selectedUser = 0
	case viewBackups:
		m.selectedBackup = 0
	case viewLog:
		m.selectedLog = 0
	default:
		m.selected = 0
	}
	m.clampSelections()
}

func (m *Model) clampSelections() {
	m.selected = clampIndex(m.selected, len(m.filteredGuests()))
	m.selectedUser = clampIndex(m.selectedUser, len(m.filteredUsers()))
	m.selectedBackup = clampIndex(m.selectedBackup, m.backupItemCount())
	m.selectedLog = clampIndex(m.selectedLog, len(m.filteredLog()))
}

func clampIndex(index int, count int) int {
	if count <= 0 || index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}

func (m Model) selectedUserModel() (proxmox.User, bool) {
	users := m.filteredUsers()
	if len(users) == 0 || m.selectedUser >= len(users) {
		return proxmox.User{}, false
	}
	return users[m.selectedUser], true
}

func (m Model) backupItemCount() int {
	if m.backupMode == "jobs" {
		return len(m.filteredBackupJobs())
	}
	return len(m.filteredBackupVolumes())
}

func (m Model) selectedBackupJob() (proxmox.BackupJob, bool) {
	jobs := m.filteredBackupJobs()
	if len(jobs) == 0 || m.selectedBackup >= len(jobs) {
		return proxmox.BackupJob{}, false
	}
	return jobs[m.selectedBackup], true
}

func (m Model) selectedSnapshotModel() (proxmox.Snapshot, bool) {
	if len(m.snapshots) == 0 || m.selectedSnapshot >= len(m.snapshots) {
		return proxmox.Snapshot{}, false
	}
	return m.snapshots[m.selectedSnapshot], true
}

func (m Model) selectedBackupVolumeByVolID(volid string) (proxmox.BackupVolume, bool) {
	for _, volume := range m.filteredBackupVolumes() {
		if volume.VolID == volid {
			return volume, true
		}
	}
	return proxmox.BackupVolume{}, false
}

func (m Model) selectedBackupVolume() (proxmox.BackupVolume, bool) {
	volumes := m.filteredBackupVolumes()
	if len(volumes) == 0 || m.selectedBackup >= len(volumes) {
		return proxmox.BackupVolume{}, false
	}
	return volumes[m.selectedBackup], true
}

func (m Model) selectedBackupVMID() int {
	if m.backupMode != "jobs" {
		volume, ok := m.selectedBackupVolume()
		if ok {
			return volume.VMID
		}
		return 0
	}
	job, ok := m.selectedBackupJob()
	if !ok {
		return 0
	}
	for _, part := range vmidParts(job.VMID) {
		var vmid int
		if _, err := fmt.Sscanf(part, "%d", &vmid); err == nil && vmid > 0 {
			return vmid
		}
	}
	return 0
}

func vmidParts(value string) []string {
	raw := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	})
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func normalizedQuery(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsAnyFold(values []string, query string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

// knownGuestName is the name of a guest, or "" when it is not in the list.
func (m Model) knownGuestName(vmid int) string {
	for _, guest := range m.data.guests {
		if guest.VMID == vmid {
			return guest.Name
		}
	}
	return ""
}

func (m Model) guestName(vmid int) string {
	for _, guest := range m.data.guests {
		if guest.VMID == vmid {
			return guest.Name
		}
	}
	if vmid == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", vmid)
}

func (m Model) defaultBackupStorage() string {
	storages := m.backupStorages()
	if len(storages) > 0 {
		return storages[0]
	}
	return ""
}

func (m Model) backupStorages() []string {
	names := make([]string, 0)
	for _, job := range m.data.backups {
		if job.Storage != "" {
			names = appendUnique(names, job.Storage)
		}
	}
	for _, storage := range m.data.storages {
		if bool(storage.Active) && strings.Contains(strings.ToLower(storage.Name), "backup") {
			names = appendUnique(names, storage.Name)
		}
	}
	for _, storage := range m.data.storages {
		if bool(storage.Active) {
			names = appendUnique(names, storage.Name)
		}
	}
	return names
}

func (m Model) storageIndex(name string) int {
	for i, storage := range m.backupStorages() {
		if storage == name {
			return i
		}
	}
	return 0
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
