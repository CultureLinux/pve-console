package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/proxmox"
)

func TestFilteredGuests(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.guests = []proxmox.Guest{
		{VMID: 302, Name: "minio", Status: "running"},
		{VMID: 300, Name: "wordpress", Status: "stopped"},
		{VMID: 301, Name: "worker", Status: "running"},
	}
	model.filter = "running"
	model.query = "wo"
	model.sortBy = "name"

	guests := model.filteredGuests()
	if len(guests) != 1 {
		t.Fatalf("expected 1 guest, got %d", len(guests))
	}
	if guests[0].VMID != 301 {
		t.Fatalf("expected worker guest, got %#v", guests[0])
	}
}

func TestSearchFiltersListViews(t *testing.T) {
	model := New(proxmox.Client{})
	model.query = "nas"
	model.data.users = []proxmox.User{
		{UserID: "root@pam", Email: "root@example.test"},
		{UserID: "backup@pam", Email: "nas@example.test"},
	}
	model.data.updates = []proxmox.PackageUpdate{
		{Name: "libssl3", Candidate: "2"},
		{Name: "nfs-common", Candidate: "nas-fix"},
	}
	model.data.guests = []proxmox.Guest{{VMID: 300, Name: "wp-preprod"}}
	model.data.backups = []proxmox.BackupJob{
		{ID: "daily", Storage: "backup-nas", VMID: "300"},
		{ID: "local", Storage: "local", VMID: "302"},
	}
	model.data.backupVolumes = []proxmox.BackupVolume{
		{VMID: 300, VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst"},
		{VMID: 302, VolID: "local:backup/vzdump-qemu-302.vma.zst"},
	}

	if got := model.filteredUsers(); len(got) != 1 || got[0].UserID != "backup@pam" {
		t.Fatalf("unexpected filtered users: %#v", got)
	}
	if got := model.filteredUpdates(); len(got) != 1 || got[0].Name != "nfs-common" {
		t.Fatalf("unexpected filtered updates: %#v", got)
	}
	if got := model.filteredBackupJobs(); len(got) != 1 || got[0].ID != "daily" {
		t.Fatalf("unexpected filtered backup jobs: %#v", got)
	}
	if got := model.filteredBackupVolumes(); len(got) != 1 || got[0].VMID != 300 {
		t.Fatalf("unexpected filtered backup volumes: %#v", got)
	}
}

func TestNextFilter(t *testing.T) {
	model := New(proxmox.Client{})

	model.nextFilter()
	if model.filter != "running" {
		t.Fatalf("expected running, got %q", model.filter)
	}
	model.nextFilter()
	if model.filter != "stopped" {
		t.Fatalf("expected stopped, got %q", model.filter)
	}
	model.nextFilter()
	if model.filter != "all" {
		t.Fatalf("expected all, got %q", model.filter)
	}
}

func TestConfigPanelGroupsHardwareDiskAndNetwork(t *testing.T) {
	model := New(proxmox.Client{})
	model.showConfig = true
	model.configVMID = 300
	model.config = proxmox.GuestConfig{
		"cores":  2,
		"memory": 4096,
		"scsi0":  "local-lvm:vm-300-disk-0,size=32G",
		"net0":   "virtio=AA:BB:CC,bridge=vmbr0",
	}

	view := model.configPanel(80, 30)
	for _, want := range []string{"HARDWARE", "DISKS", "NETWORK", "cores", "scsi0", "net0"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in config panel, got %q", want, view)
		}
	}
}

func TestUserPanelShowsUsers(t *testing.T) {
	model := New(proxmox.Client{})
	model.viewMode = viewUsers
	model.data.users = []proxmox.User{{UserID: "root@pam", Enable: true, TFAEnabled: true, Email: "root@example.test"}}

	view := model.userPanel(80, 20)
	for _, want := range []string{"USERS", "TFA", "root@pam", "true", "root@example.test"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in user panel, got %q", want, view)
		}
	}
}

func TestFooterShowsDisableTFAConfirmation(t *testing.T) {
	model := New(proxmox.Client{})
	model.modalMode = modalNone
	model.inputMode = inputDisableTFA
	model.input = "root@pam"

	if got := model.footerText(); !strings.Contains(got, "disable TFA") || !strings.Contains(got, "root@pam") {
		t.Fatalf("unexpected footer: %q", got)
	}
}

func TestFooterShowsSnapshotRAMToggle(t *testing.T) {
	model := New(proxmox.Client{})
	model.modalMode = modalSnapshot

	got := model.footerText()
	if !strings.Contains(got, "ram:on") || !strings.Contains(got, "snapshot modal") {
		t.Fatalf("unexpected footer: %q", got)
	}
}

func TestModalRendersSnapshotActions(t *testing.T) {
	model := New(proxmox.Client{})
	model.openModal(modalSnapshot, 300, "", "before-upgrade")

	view := model.modal(80)
	for _, want := range []string{"SNAPSHOT", "VMID 300", "before-upgrade", "Snap", "Annuler"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in modal, got %q", want, view)
		}
	}
}

func TestModalRendersBackupStorageSelect(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.storages = []proxmox.Storage{{Name: "local", Active: true}, {Name: "backup-nas", Active: true}}
	model.openModal(modalBackup, 300, "", model.defaultBackupStorage())

	view := model.modal(80)
	for _, want := range []string{"BACKUP", "VMID 300", "Storage", "< backup-nas >", "Backup", "Annuler"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in modal, got %q", want, view)
		}
	}
}

func TestModalRendersScheduleVMIDs(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.guests = []proxmox.Guest{{VMID: 300, Name: "wp-preprod"}, {VMID: 302, Name: "worker"}}
	model.data.storages = []proxmox.Storage{{Name: "local", Active: true}, {Name: "backup-nas", Active: true}}
	model.openModal(modalSchedule, 0, "daily", "backup-nas")
	model.modalInput = "02:30"
	model.modalVMIDs = "300,302"

	view := model.modal(80)
	for _, want := range []string{"BACKUP SCHEDULE", "Job daily", "Storage", "< backup-nas >", "Schedule", "02:30", "VMIDS", "wp-preprod", "Save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in modal, got %q", want, view)
		}
	}
}

func TestStartupModalWarnsExperimentalRootActions(t *testing.T) {
	model := New(proxmox.Client{})

	view := model.modal(80)
	for _, want := range []string{"EXPERIMENTAL", "still experimental", "executed as root", "enter continue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in startup modal, got %q", want, view)
		}
	}
}

func TestHelpModalRendersCommandsAndCredit(t *testing.T) {
	model := New(proxmox.Client{})
	model.openModal(modalHelp, 0, "", "")

	view := model.modal(80)
	for _, want := range []string{"HELP", "Navigation", "Backups", "n              new snapshot", "e              edit schedule and VMIDs", "?, h           open / close help", "dev by @clinux"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in help modal, got %q", want, view)
		}
	}
}

func TestUpdatesAndBackupsPanels(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.updates = []proxmox.PackageUpdate{{Name: "libssl3", Current: "1", Candidate: "2"}}
	model.data.guests = []proxmox.Guest{{VMID: 300, Name: "wp-preprod"}}
	model.data.backups = []proxmox.BackupJob{{ID: "daily", Storage: "backup-nas", Schedule: "03:00", Enabled: true, VMID: "300,302"}}
	model.data.backupVolumes = []proxmox.BackupVolume{{VMID: 300, VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst", Size: 1024, CTime: 1790413200}}

	updates := model.updatesPanel(100, 20)
	if !strings.Contains(updates, "APT UPDATES (1)") || !strings.Contains(updates, "libssl3") {
		t.Fatalf("unexpected updates panel: %q", updates)
	}

	backups := model.backupsPanel(100, 20)
	if !strings.Contains(backups, "BACKUP VOLUMES (1)") || !strings.Contains(backups, "wp-preprod") || !strings.Contains(backups, "2026-") {
		t.Fatalf("unexpected backups panel: %q", backups)
	}

	model.backupMode = "jobs"
	jobs := model.backupsPanel(100, 20)
	if !strings.Contains(jobs, "BACKUP JOBS (1)") || !strings.Contains(jobs, "backup-nas") {
		t.Fatalf("unexpected backup jobs panel: %q", jobs)
	}
}
func TestEscFromSectionReturnsGuests(t *testing.T) {
	model := NewWithStartup(proxmox.Client{}, false)
	model.viewMode = viewBackups
	model.message = "backup jobs"

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("expected no quit command")
	}
	next := updated.(Model)
	if next.viewMode != viewGuests {
		t.Fatalf("expected guests view, got %q", next.viewMode)
	}
	if next.message != "" {
		t.Fatalf("expected cleared message, got %q", next.message)
	}
}

func TestViewCentersModalOverlay(t *testing.T) {
	model := NewWithStartup(proxmox.Client{}, false)
	model.width = 100
	model.height = 30
	model.openModal(modalSnapshot, 300, "", "before-upgrade")

	view := model.View()
	if !strings.Contains(view, "SNAPSHOT") || !strings.Contains(view, "VMID 300") {
		t.Fatalf("expected snapshot modal in view, got %q", view)
	}
	if strings.LastIndex(view, "SNAPSHOT") > strings.LastIndex(view, "snapshot modal") {
		t.Fatalf("expected modal overlay before footer, got %q", view)
	}
}
