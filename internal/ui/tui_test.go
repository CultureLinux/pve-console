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
	for _, want := range []string{"Users", "2FA", "root@pam", "root@example.test"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in user panel, got %q", want, view)
		}
	}
}

func TestDisableTFAConfirmationIsADangerModal(t *testing.T) {
	model := New(proxmox.Client{})
	model.modalMode = modalNone
	model.viewMode = viewUsers
	model.data.users = []proxmox.User{{UserID: "root@pam"}}
	model, _ = press(t, model, "x")

	view := model.modal(80)
	for _, want := range []string{"Disable two-factor", "root@pam", "This cannot be undone", "Disable 2FA", "esc Cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in the TFA confirmation, got %q", want, view)
		}
	}
	if strings.Contains(view, "Type the user id") {
		t.Fatalf("nothing must be typed to confirm: %q", view)
	}
	if got := model.footerText(); !strings.Contains(got, "enter confirm") || !strings.Contains(got, "esc cancel") {
		t.Fatalf("unexpected footer: %q", got)
	}
}

func TestSnapshotModalMentionsTheRAMState(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.guests = []proxmox.Guest{{VMID: 300, Name: "wp", Type: proxmox.GuestTypeQEMU}, {VMID: 400, Name: "ct", Type: proxmox.GuestTypeLXC}}
	model.openModal(modalSnapshot, 300, "", "")
	if view := model.modal(80); !strings.Contains(view, "RAM state") {
		t.Fatalf("a QEMU snapshot saves the RAM state, got %q", view)
	}

	model.openModal(modalSnapshot, 400, "", "")
	if view := model.modal(80); strings.Contains(view, "RAM state") {
		t.Fatalf("a container has no RAM state, got %q", view)
	}
}

func TestModalRendersSnapshotActions(t *testing.T) {
	model := New(proxmox.Client{})
	model.openModal(modalSnapshot, 300, "", "before-upgrade")

	view := model.modal(80)
	for _, want := range []string{"New snapshot", "VMID 300", "before-upgrade", "Create", "Cancel"} {
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
	for _, want := range []string{"Run a backup", "VMID 300", "Storage", "‹ backup-nas ›", "Start backup", "Cancel"} {
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
	for _, want := range []string{"Backup schedule", "job daily", "Storage", "‹ backup-nas ›", "Schedule", "02:30", "GUESTS", "wp-preprod", "Save"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in modal, got %q", want, view)
		}
	}
}

func TestStartupModalWarnsExperimentalRootActions(t *testing.T) {
	model := New(proxmox.Client{})

	view := model.modal(80)
	for _, want := range []string{"experimental", "runs as root", "continue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in startup modal, got %q", want, view)
		}
	}
}

func TestHelpModalRendersCommandsAndCredit(t *testing.T) {
	model := New(proxmox.Client{})
	model.openModal(modalHelp, 0, "", "")

	view := model.modal(80)
	for _, want := range []string{"Help", "NAVIGATION", "BACKUPS", "new snapshot", "edit a job schedule", "stop (forced)", "dev by @clinux"} {
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
	if !strings.Contains(updates, "APT updates") || !strings.Contains(updates, "libssl3") {
		t.Fatalf("unexpected updates panel: %q", updates)
	}

	backups := model.backupsPanel(100, 20)
	if !strings.Contains(backups, "Volumes") || !strings.Contains(backups, "wp-preprod") || !strings.Contains(backups, "2026-") {
		t.Fatalf("unexpected backups panel: %q", backups)
	}

	model.backupMode = "jobs"
	jobs := model.backupsPanel(100, 20)
	if !strings.Contains(jobs, "Jobs") || !strings.Contains(jobs, "backup-nas") || !strings.Contains(jobs, "03:00") {
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
	if !strings.Contains(view, "New snapshot") || !strings.Contains(view, "VMID 300") {
		t.Fatalf("expected snapshot modal in view, got %q", view)
	}
	lines := strings.Split(view, "\n")
	row := -1
	for i, line := range lines {
		if strings.Contains(line, "New snapshot") {
			row = i
			break
		}
	}
	if row <= headerRows || row >= len(lines)-footerRows {
		t.Fatalf("the modal must sit between the header and the footer, found at row %d of %d", row, len(lines))
	}
}

func TestEnterOutsideGuestsDoesNotOpenConfig(t *testing.T) {
	model := NewWithStartup(proxmox.Client{}, false)
	model.viewMode = viewUpdates
	model.data.guests = []proxmox.Guest{{VMID: 300, Name: "wp-preprod"}}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected no command")
	}
	next := updated.(Model)
	if next.showConfig {
		t.Fatalf("expected config modal to stay closed")
	}
}

func TestPanelsShowALoaderWhileLoading(t *testing.T) {
	model := NewWithStartup(proxmox.Client{}, false)
	model.loading = true
	model.viewMode = viewUpdates

	view := model.updatesPanel(80, 20)
	if !strings.Contains(view, "checking apt") {
		t.Fatalf("expected a loader, got %q", view)
	}
}

func TestLoaderKeepsTheProjectsLittleFace(t *testing.T) {
	for frame, want := range []string{"(o_o)", "(O_o)", "(o_O)", "(^_^)", "(o_o)"} {
		if got := petFace(frame); got != want {
			t.Fatalf("petFace(%d) = %q, want %q", frame, got, want)
		}
	}
	if got := petFace(-3); got != "(o_o)" {
		t.Fatalf("a negative frame must not panic, got %q", got)
	}
	if view := stripANSI(loading("checking apt")); !strings.Contains(view, "checking apt") {
		t.Fatalf("unexpected loader %q", view)
	}
}

func TestUpgradeModalShowsOutput(t *testing.T) {
	model := NewWithStartup(proxmox.Client{}, false)
	model.modalMode = modalUpgrade
	model.upgradePhase = upgradeApply
	model.upgradeOutput = "Preparing packages\nDone"

	view := model.modal(100)
	for _, want := range []string{"Upgrade", "apt dist-upgrade", "Preparing packages", "Done", "close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in upgrade modal, got %q", want, view)
		}
	}
}
