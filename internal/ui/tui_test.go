package ui

import (
	"strings"
	"testing"

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
	model.inputMode = inputDisableTFA
	model.input = "root@pam"

	if got := model.footerText(); !strings.Contains(got, "disable TFA") || !strings.Contains(got, "root@pam") {
		t.Fatalf("unexpected footer: %q", got)
	}
}

func TestFooterShowsSnapshotRAMToggle(t *testing.T) {
	model := New(proxmox.Client{})
	model.inputMode = inputSnapshot
	model.input = "before-upgrade"

	got := model.footerText()
	if !strings.Contains(got, "ram:on") || !strings.Contains(got, "before-upgrade") {
		t.Fatalf("unexpected footer: %q", got)
	}
}

func TestUpdatesAndBackupsPanels(t *testing.T) {
	model := New(proxmox.Client{})
	model.data.updates = []proxmox.PackageUpdate{{Name: "libssl3", Current: "1", Candidate: "2"}}
	model.data.backups = []proxmox.BackupJob{{ID: "daily", Storage: "backup-nas", Schedule: "03:00", Enabled: true, VMID: "300,302"}}
	model.data.backupVolumes = []proxmox.BackupVolume{{VMID: 300, VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst", Size: 1024}}

	updates := model.updatesPanel(100, 20)
	if !strings.Contains(updates, "APT UPDATES (1)") || !strings.Contains(updates, "libssl3") {
		t.Fatalf("unexpected updates panel: %q", updates)
	}

	backups := model.backupsPanel(100, 20)
	if !strings.Contains(backups, "BACKUP VOLUMES (1)") || !strings.Contains(backups, "vzdump-qemu-300") {
		t.Fatalf("unexpected backups panel: %q", backups)
	}

	model.backupMode = "jobs"
	jobs := model.backupsPanel(100, 20)
	if !strings.Contains(jobs, "BACKUP JOBS (1)") || !strings.Contains(jobs, "backup-nas") {
		t.Fatalf("unexpected backup jobs panel: %q", jobs)
	}
}
