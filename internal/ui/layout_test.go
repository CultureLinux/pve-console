package ui

import (
	"fmt"
	"strings"
	"testing"

	"pvc/internal/proxmox"
	"pvc/internal/update"
	"pvc/internal/version"
)

// richModel is a model with data in every section, including awkward names.
func richModel() Model {
	m := newTestModel(&fakeRunner{})
	m.data.node = proxmox.NodeInfo{Name: "pve", ProxmoxVersion: "pve-manager/9.0.3/abc", Kernel: "6.14"}
	m.data.guests = []proxmox.Guest{
		{VMID: 100, Name: "wordpress", Type: proxmox.GuestTypeQEMU, Status: "running", CPU: 0.12, Memory: 1 << 30, MaxMem: 4 << 30, Uptime: 90000},
		{VMID: 101, Name: "日本語の名前のゲスト-with-a-really-long-name-that-cannot-possibly-fit", Type: proxmox.GuestTypeQEMU, Status: "running", CPU: 1.4, Memory: 9 << 30, MaxMem: 8 << 30},
		{VMID: 102, Name: "minio", Type: proxmox.GuestTypeQEMU, Status: "stopped"},
		{VMID: 200, Name: "ct", Type: proxmox.GuestTypeLXC, Status: "paused"},
	}
	m.data.storages = []proxmox.Storage{
		{Name: "local", Usage: 0.45, Active: true},
		{Name: "a-storage-with-a-very-long-name-indeed", Usage: 1.2, Active: true},
		{Name: "broken", Active: false},
	}
	m.data.updates = []proxmox.PackageUpdate{{Name: "libssl3", Current: "3.0.1", Candidate: "3.0.2"}}
	m.data.users = []proxmox.User{{UserID: "root@pam", Enable: true, TFAEnabled: true, Email: "root@example.test", Comment: "the administrator account of this node"}}
	m.data.tokens = []proxmox.Token{
		{UserID: "root@pam", ID: "grafana", Comment: "Grafana: metrics", PrivSep: true},
		{UserID: "alice@pve", ID: "a-token-with-a-rather-long-name-indeed", Comment: "a comment that is far too long to fit in its column", Expire: 1000},
	}
	m.data.acls = []proxmox.ACLEntry{{Path: "/", RoleID: "PVEAuditor", Type: "token", UGID: "root@pam!grafana", Propagate: true}}
	m.data.backups = []proxmox.BackupJob{{ID: "daily", Storage: "backup-nas", Schedule: "02:30", Enabled: true, VMID: "100,101,102,200,201,202,203"}}
	m.data.backupVolumes = []proxmox.BackupVolume{{VMID: 100, VolID: "backup-nas:backup/vzdump-qemu-100-2026_01_01-02_30_00.vma.zst", Size: 3 << 30, CTime: 1700000000, Format: "vma.zst"}}
	m.snapshots = []proxmox.Snapshot{{Name: "before-upgrade", SnapTime: 1700000000}, {Name: "current"}}
	m.config = proxmox.GuestConfig{"cores": 2, "memory": 4096, "scsi0": "local-lvm:vm-100-disk-0,size=32G", "net0": "virtio=AA:BB:CC,bridge=vmbr0"}
	m.configVMID = 100
	m.tfaEntries = []proxmox.TFAEntry{{ID: "1", Type: "totp", Description: "phone"}}
	m.tfaUserID = "root@pam"
	m.message = "backup of 100: OK"
	return m
}

type screen struct {
	name string
	set  func(*Model)
}

func screens() []screen {
	return []screen{
		{"guests", func(m *Model) {}},
		{"guests filtered", func(m *Model) { m.filter, m.guestTypeFilter, m.sortBy, m.query = "running", "qemu", "name", "wo" }},
		{"guests empty result", func(m *Model) { m.query = "no-such-guest" }},
		{"guests loading", func(m *Model) { m.data.guests, m.loading = nil, true }},
		{"guests error", func(m *Model) { m.data.err = fmt.Errorf("pvesh failed: %s", strings.Repeat("boom ", 40)) }},
		{"snapshots", func(m *Model) { m.showSnaps, m.snapshotVMID = true, 100 }},
		{"snapshots error", func(m *Model) { m.showSnaps, m.snapErr = true, fmt.Errorf("nope") }},
		{"config", func(m *Model) { m.showConfig = true }},
		{"config error", func(m *Model) { m.showConfig, m.config, m.configErr = true, nil, fmt.Errorf("denied") }},
		{"config loading", func(m *Model) { m.showConfig, m.config = true, nil }},
		{"users", func(m *Model) { m.viewMode = viewUsers }},
		{"tokens", func(m *Model) { m.viewMode = viewTokens }},
		{"tokens second", func(m *Model) { m.viewMode, m.selectedToken = viewTokens, 1 }},
		{"tokens loading", func(m *Model) { m.viewMode, m.data.tokens, m.loading = viewTokens, nil, true }},
		{"tokens empty", func(m *Model) { m.viewMode, m.data.tokens = viewTokens, nil }},
		{"tokens error", func(m *Model) {
			m.viewMode, m.data.tokens, m.data.tokenErr = viewTokens, nil, fmt.Errorf("pvesh failed: %s", strings.Repeat("boom ", 40))
		}},
		{"updates", func(m *Model) { m.viewMode = viewUpdates }},
		{"updates up to date", func(m *Model) { m.viewMode, m.data.updates = viewUpdates, nil }},
		{"backups volumes", func(m *Model) { m.viewMode = viewBackups }},
		{"backups jobs", func(m *Model) { m.viewMode, m.backupMode = viewBackups, "jobs" }},
		{"backups empty", func(m *Model) { m.viewMode, m.data.backupVolumes = viewBackups, nil }},
		{"search", func(m *Model) { m.inputMode, m.input = inputSearch, "wo" }},
		{"modal startup", func(m *Model) { m.modalMode = modalStartup }},
		{"modal help", func(m *Model) { m.modalMode = modalHelp }},
		{"modal snapshot", func(m *Model) { m.openModal(modalSnapshot, 100, "", "snap") }},
		{"modal backup", func(m *Model) { m.openModal(modalBackup, 100, "", "backup-nas") }},
		{"modal schedule", func(m *Model) {
			m.openModal(modalSchedule, 0, "daily", "backup-nas")
			m.modalInput, m.modalVMIDs, m.modalField = "02:30", "100,102", 2
		}},
		{"modal confirm danger", func(m *Model) { m.openConfirm("stop-guest", "STOP GUEST", "100", 100, "Stop QEMU 100 (wordpress) ?") }},
		{"modal confirm warn", func(m *Model) { m.openConfirm("reboot-guest", "REBOOT GUEST", "100", 100, "Reboot QEMU 100 ?") }},
		{"modal upgrade running", func(m *Model) { m.modalMode, m.upgradeRunning = modalUpgrade, true }},
		{"modal upgrade output", func(m *Model) {
			m.modalMode, m.upgradeOutput = modalUpgrade, strings.Repeat("a long line of apt output ", 20)+strings.Repeat("\nmore", 60)
		}},
		{"modal upgrade confirm", func(m *Model) {
			var updates []proxmox.PackageUpdate
			for i := 0; i < 20; i++ {
				updates = append(updates, proxmox.PackageUpdate{Name: fmt.Sprintf("a-rather-long-package-name-%d", i)})
			}
			updates = append(updates, proxmox.PackageUpdate{Name: "proxmox-kernel-6.14"})
			m.openConfirm("dist-upgrade", "UPGRADE PACKAGES", "", 0, upgradeConfirmBody(updates))
		}},
		{"modal upgrade step 1", func(m *Model) {
			m.modalMode, m.upgradePhase, m.upgradeRunning = modalUpgrade, upgradeIndex, true
			m.upgradeOutput = "Hit:1 http://deb.debian.org bookworm InRelease\nGet:2 http://download.proxmox.com/debian/pve bookworm InRelease"
		}},
		{"modal upgrade step 2 running", func(m *Model) {
			m.modalMode, m.upgradePhase, m.upgradeRunning = modalUpgrade, upgradeApply, true
			m.upgradeOutput = strings.Repeat("Unpacking a-rather-long-package-name (1.2.3) over (1.2.2) ...\n", 80)
		}},
		{"modal upgrade reboot", func(m *Model) {
			m.modalMode, m.upgradePhase = modalUpgrade, upgradeApply
			m.upgradeOutput = strings.Repeat("Setting up proxmox-kernel-6.14.8-3-pve-signed (6.14.8-3) ...\n", 30)
			m.upgradeReboot = "kernel 6.14.8-3-pve installed, kernel 6.14.8-2-pve running; the system asks for it (/var/run/reboot-required)"
		}},
		{"modal upgrade up to date", func(m *Model) {
			m.modalMode, m.upgradePhase, m.upgradeNote = modalUpgrade, upgradeIndex, "Everything is up to date."
		}},
		{"header with an update", func(m *Model) {
			m.versionText = "v0.2.0"
			m.upd = updateState{phase: updateKnown, hasInfo: true, info: update.Info{Current: "v0.2.0", Latest: "v0.2.1", Available: true}}
		}},
		{"header with a restart to do", func(m *Model) { m.versionText, m.upd.phase = "v0.2.0", updateInstalled }},
		{"modal update checking", func(m *Model) { m.modalMode, m.versionText, m.upd.phase = modalUpdate, "v0.2.0", updateChecking }},
		{"modal update available", func(m *Model) {
			m.modalMode, m.versionText = modalUpdate, "v0.2.0"
			m.upd = updateState{phase: updateKnown, hasInfo: true, info: update.Info{
				Kind: version.KindStable, Current: "v0.2.0", Latest: "v0.2.1", Available: true,
				Notes: strings.Repeat("- a rather long line of release notes that must not overflow the modal at all\n", 20),
			}}
		}},
		{"modal update edge", func(m *Model) {
			m.modalMode, m.versionText = modalUpdate, "edge 3f9a2c1"
			m.upd = updateState{phase: updateKnown, hasInfo: true, info: update.Info{Kind: version.KindEdge, Current: "edge 3f9a2c1", Latest: "edge aaaaaaa", Available: true}}
		}},
		{"modal update up to date", func(m *Model) {
			m.modalMode, m.versionText = modalUpdate, "v0.2.0"
			m.upd = updateState{phase: updateKnown, hasInfo: true, info: update.Info{Kind: version.KindStable, Current: "v0.2.0", Latest: "v0.2.0"}}
		}},
		{"modal update installing", func(m *Model) {
			m.modalMode, m.versionText = modalUpdate, "v0.2.0"
			m.upd = updateState{phase: updateInstalling, info: update.Info{Latest: "v0.2.1"}, steps: []string{"downloading " + update.Asset + " (v0.2.1) from a very long address", "checking the SHA-256 sum"}}
		}},
		{"modal update installed", func(m *Model) {
			m.modalMode, m.versionText = modalUpdate, "v0.2.0"
			m.upd = updateState{phase: updateInstalled, info: update.Info{Latest: "v0.2.1"}, result: update.Result{Path: "/usr/local/sbin/pve-console", Previous: "/usr/local/sbin/pve-console.prev"}}
		}},
		{"modal update error", func(m *Model) {
			m.modalMode = modalUpdate
			m.upd = updateState{phase: updateKnown, err: fmt.Errorf("unable to read the latest release: %s", strings.Repeat("GitHub answered 500. ", 10))}
		}},
		{"modal update disabled", func(m *Model) {
			m.modalMode = modalUpdate
			m.upd = updateState{phase: updateKnown, err: update.ErrDisabled}
		}},
		{"modal upgrade error", func(m *Model) { m.modalMode, m.upgradeErr = modalUpgrade, fmt.Errorf("apt failed") }},
		{"modal tfa", func(m *Model) {
			m.viewMode = viewUsers
			m.openConfirm("disable-tfa", "DISABLE TWO-FACTOR", "root@pam", 0, "Remove every two-factor entry of root@pam ?")
		}},
		{"errors", func(m *Model) { fillErrors(m); m.viewMode = viewLog }},
		{"errors filtered", func(m *Model) {
			fillErrors(m)
			m.viewMode, m.logSource, m.selectedLog = viewLog, sourceConsole, 1
		}},
		{"errors searched out", func(m *Model) { fillErrors(m); m.viewMode, m.query = viewLog, "zzzz" }},
		{"errors empty", func(m *Model) { m.viewMode = viewLog }},
		{"errors long detail", func(m *Model) {
			m.viewMode = viewLog
			var detail []string
			for i := 0; i < 90; i++ {
				detail = append(detail, strings.Repeat("a wrapped detail line ", 12))
			}
			*m = m.logProblem(sourceProxmox, "command failed", strings.Repeat("context ", 30), strings.Repeat("日本語 message ", 60), detail)
		}},
		{"errors tab with unread", func(m *Model) { fillErrors(m) }},
		{"tasks running", func(m *Model) { m.tasks = []trackedTask{{upid: "a"}, {upid: "b"}} }},
		{"long message", func(m *Model) { m.message = "FAILED: " + strings.Repeat("very long error ", 40) }},
	}
}

var sizes = [][2]int{{60, 14}, {70, 18}, {80, 24}, {99, 30}, {100, 30}, {120, 34}, {200, 60}}

// Every screen, at every size, must have exactly the height of the terminal and
// no line wider than it. The old overlay broke both.
func TestEveryScreenFitsTheTerminal(t *testing.T) {
	for _, sc := range screens() {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s/%dx%d", sc.name, size[0], size[1]), func(t *testing.T) {
				m := richModel()
				m.width, m.height = size[0], size[1]
				sc.set(&m)

				lines := strings.Split(m.View(), "\n")
				if len(lines) != size[1] {
					t.Fatalf("%d lines, want %d", len(lines), size[1])
				}
				for i, line := range lines {
					if w := textWidth(line); w > size[0] {
						t.Fatalf("line %d is %d columns wide, the terminal has %d: %q", i, w, size[0], stripANSI(line))
					}
				}
			})
		}
	}
}

func TestPanelsAreAlignedOnWideTerminals(t *testing.T) {
	m := richModel()
	m.width, m.height = 120, 34
	lines := strings.Split(stripANSI(m.View()), "\n")
	// Rows of the body start and end with a border, so both panels line up.
	for i := headerRows; i < len(lines)-footerRows; i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "╭") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "╰") {
			t.Fatalf("row %d does not start on a border: %q", i, line)
		}
		if textWidth(line) != 120 {
			t.Fatalf("row %d has width %d", i, textWidth(line))
		}
	}
}

func TestTooSmallTerminalExplainsItself(t *testing.T) {
	m := richModel()
	for _, size := range [][2]int{{59, 30}, {120, 13}, {20, 5}} {
		m.width, m.height = size[0], size[1]
		out := m.View()
		if !strings.Contains(out, "Terminal too small") || strings.Contains(out, "\n") {
			t.Fatalf("%v: %q", size, out)
		}
		if textWidth(out) > size[0] {
			t.Fatalf("%v: the notice is wider than the terminal", size)
		}
	}
}

func TestDetailPanelIsHiddenOnNarrowTerminals(t *testing.T) {
	m := richModel()
	m.height = 30

	m.width = detailMinWidth - 1
	narrow := stripANSI(m.View())
	m.width = detailMinWidth
	wide := stripANSI(m.View())

	if strings.Contains(narrow, "QEMU VM") || !strings.Contains(wide, "QEMU VM") {
		t.Fatal("the detail panel appears from detailMinWidth columns")
	}
}

func TestSelectionStaysVisibleInALongList(t *testing.T) {
	m := richModel()
	m.width, m.height = 120, 20
	m.data.guests = nil
	for i := 0; i < 80; i++ {
		m.data.guests = append(m.data.guests, proxmox.Guest{VMID: 1000 + i, Name: fmt.Sprintf("guest-%02d", i), Type: proxmox.GuestTypeQEMU, Status: "running"})
	}
	for _, selected := range []int{0, 5, 40, 79} {
		m.selected = selected
		out := stripANSI(m.View())
		if want := fmt.Sprintf("guest-%02d", selected); !strings.Contains(out, glyphSelection+"● "+fmt.Sprintf("%d", 1000+selected)) || !strings.Contains(out, want) {
			t.Fatalf("selection %d is not on screen:\n%s", selected, out)
		}
	}
}

func TestConfigPanelScrolls(t *testing.T) {
	m := richModel()
	m.config = proxmox.GuestConfig{}
	for i := 0; i < 60; i++ {
		m.config[fmt.Sprintf("opt%02d", i)] = i
	}
	m.showConfig = true
	m.width, m.height = 100, 20

	top := stripANSI(m.View())
	if !strings.Contains(top, "opt00") || strings.Contains(top, "opt59") {
		t.Fatalf("the top of the configuration is shown first:\n%s", top)
	}
	m.configScroll = 1000
	bottom := stripANSI(m.View())
	if !strings.Contains(bottom, "opt59") || strings.Contains(bottom, "opt00") {
		t.Fatalf("scrolling far shows the end:\n%s", bottom)
	}
	if !strings.Contains(bottom, " of ") {
		t.Fatal("a scrolled panel shows its position")
	}
}

func TestConfigScrollKeys(t *testing.T) {
	m := richModel()
	m.showConfig = true
	m, _ = press(t, m, "j", "j", "down")
	if m.configScroll != 3 || m.selected != 0 {
		t.Fatalf("scroll=%d selected=%d: the keys scroll the panel, not the list", m.configScroll, m.selected)
	}
	m, _ = press(t, m, "k", "up", "up", "up", "up")
	if m.configScroll != 0 {
		t.Fatalf("scroll=%d must not go below zero", m.configScroll)
	}

	m.showConfig = false
	m, _ = press(t, m, "j")
	if m.selected != 1 {
		t.Fatalf("outside the panel the keys move the selection, selected=%d", m.selected)
	}
}

func TestOpeningTheConfigurationStartsAtTheTop(t *testing.T) {
	m := richModel()
	m.configScroll = 12
	m, _ = press(t, m, "enter")
	if !m.showConfig || m.configScroll != 0 {
		t.Fatalf("showConfig=%t scroll=%d", m.showConfig, m.configScroll)
	}
}

// fillErrors puts one error of each kind in the journal.
func fillErrors(m *Model) {
	*m = m.logError("create snapshot t1 for guest 100", pveshFailure("400 Parameter verification failed.\nsnapname: invalid"))
	*m = m.logConsole("invalid input", "create snapshot", "invalid snapshot name \"bad name\"")
	*m = m.logProblem(sourceProxmox, "task failed", "backup of 103", "backup of 103: FAILED: job errors", []string{"task: x", "  ERROR: boom"})
	*m = m.logOK(clock(), sourceProxmox, "task ok", "backup of 100", "OK", []string{"task: y", "exit status: OK"})
	*m = m.logDone("disable TFA of alice@pve", "done")
}
