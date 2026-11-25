package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pvc/internal/command"
	"pvc/internal/proxmox"
)

const testUPID = "UPID:pve:0001A2B3:00C4D5E6:67890ABC:vzdump::root@pam:"

// fakeRunner records every command and answers with canned stdout selected by
// substring. Unknown commands succeed with an empty output.
type fakeRunner struct {
	calls   []string
	stdout  map[string]string
	failing map[string]error
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) (command.Result, error) {
	key := command.Key(name, args...)
	r.calls = append(r.calls, key)
	for part, err := range r.failing {
		if strings.Contains(key, part) {
			return command.Result{}, err
		}
	}
	for part, out := range r.stdout {
		if strings.Contains(key, part) {
			return command.Result{Stdout: []byte(out)}, nil
		}
	}
	if strings.Contains(key, "/status/current") {
		// The guest type probe that precedes every guest action: a QEMU VM.
		return command.Result{Stdout: []byte(`{"name":"vm","status":"stopped"}`)}, nil
	}
	return command.Result{}, nil
}

func (r *fakeRunner) called(part string) bool {
	for _, call := range r.calls {
		if strings.Contains(call, part) {
			return true
		}
	}
	return false
}

func newTestModel(runner *fakeRunner) Model {
	m := NewWithStartup(proxmox.Client{Runner: runner, Node: "pve"}, false)
	m.loading = false
	m.data.guests = []proxmox.Guest{
		{VMID: 300, Name: "wordpress", Type: proxmox.GuestTypeQEMU, Status: "stopped"},
		{VMID: 301, Name: "worker", Type: proxmox.GuestTypeQEMU, Status: "running"},
		{VMID: 400, Name: "ct", Type: proxmox.GuestTypeLXC, Status: "running"},
	}
	m.data.storages = []proxmox.Storage{{Name: "backup-nas", Active: true, Enabled: true}}
	return m
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m Model, keys ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		next, c := m.Update(keyMsg(k))
		var ok bool
		m, ok = next.(Model)
		if !ok {
			t.Fatalf("Update returned %T", next)
		}
		cmd = c
	}
	return m, cmd
}

// deliver feeds a message to Update and returns the new model and command.
func deliver(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T", next)
	}
	return model, cmd
}

// execute runs a command and feeds its message back into Update.
func execute(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	return deliver(t, m, cmd())
}

func TestUpdateWindowSize(t *testing.T) {
	m, _ := deliver(t, newTestModel(&fakeRunner{}), tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("unexpected size %dx%d", m.width, m.height)
	}
}

func TestUpdateGuestNavigationFiltersAndSearch(t *testing.T) {
	m := newTestModel(&fakeRunner{})

	m, _ = press(t, m, "j", "j", "j")
	if m.selected != 2 {
		t.Fatalf("selection should clamp at last guest, got %d", m.selected)
	}
	m, _ = press(t, m, "k", "up")
	if m.selected != 0 {
		t.Fatalf("expected selection 0, got %d", m.selected)
	}

	m, _ = press(t, m, "f")
	if m.filter != "running" || len(m.filteredGuests()) != 2 {
		t.Fatalf("filter=%q guests=%d", m.filter, len(m.filteredGuests()))
	}
	m, _ = press(t, m, "tab")
	if m.guestTypeFilter == "all" {
		t.Fatal("tab should change the guest type filter")
	}
	m, _ = press(t, m, "tab", "tab")
	if m.guestTypeFilter != "all" {
		t.Fatalf("expected type filter back to all, got %q", m.guestTypeFilter)
	}
	m, _ = press(t, m, "f", "f")
	if m.filter != "all" {
		t.Fatalf("expected filter all, got %q", m.filter)
	}

	m, _ = press(t, m, "t")
	if m.sortBy == "id" {
		t.Fatal("t should change the sort order")
	}

	m, _ = press(t, m, "/", "w", "o", "r", "k", "backspace", "k")
	if m.inputMode != inputSearch || m.input != "work" {
		t.Fatalf("mode=%q input=%q", m.inputMode, m.input)
	}
	m, _ = press(t, m, "enter")
	if m.query != "work" || m.inputMode != inputNone {
		t.Fatalf("query=%q mode=%q", m.query, m.inputMode)
	}
	if got := m.filteredGuests(); len(got) != 1 || got[0].VMID != 301 {
		t.Fatalf("unexpected guests %#v", got)
	}

	m, _ = press(t, m, "/", "esc")
	if m.inputMode != inputNone || m.query != "work" {
		t.Fatalf("esc should only close the input, query=%q", m.query)
	}
	m, _ = press(t, m, "c")
	if m.query != "" {
		t.Fatalf("c should clear the query, got %q", m.query)
	}
}

func TestUpdateQuit(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c", "esc"} {
		t.Run(k, func(t *testing.T) {
			_, cmd := press(t, newTestModel(&fakeRunner{}), k)
			if cmd == nil {
				t.Fatal("expected quit command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("expected QuitMsg, got %T", cmd())
			}
		})
	}
}

func TestUpdateViewSwitching(t *testing.T) {
	m := newTestModel(&fakeRunner{})

	m, cmd := press(t, m, "u")
	if m.viewMode != viewUsers || !m.loading || cmd == nil {
		t.Fatalf("users view: mode=%q loading=%t cmd=%t", m.viewMode, m.loading, cmd != nil)
	}
	m, _ = press(t, m, "esc")
	if m.viewMode != viewGuests {
		t.Fatalf("esc should return to guests, got %q", m.viewMode)
	}

	m, cmd = press(t, m, "p")
	if m.viewMode != viewUpdates || cmd == nil {
		t.Fatalf("updates view: mode=%q cmd=%t", m.viewMode, cmd != nil)
	}
	m, cmd = press(t, m, "b")
	if m.viewMode != viewBackups || cmd == nil {
		t.Fatalf("backups view: mode=%q cmd=%t", m.viewMode, cmd != nil)
	}
	m, _ = press(t, m, "tab")
	if m.backupMode != "jobs" {
		t.Fatalf("tab should switch to jobs, got %q", m.backupMode)
	}
	m, _ = press(t, m, "tab")
	if m.backupMode != "volumes" {
		t.Fatalf("tab should switch back to volumes, got %q", m.backupMode)
	}
	m, _ = press(t, m, "g")
	if m.viewMode != viewGuests {
		t.Fatalf("g should return to guests, got %q", m.viewMode)
	}
}

func TestUpdateRefreshPerView(t *testing.T) {
	tests := []struct {
		view string
		want string
	}{
		{viewGuests, "pvesh get /nodes"},
		{viewUsers, "pvesh get /access/users"},
		{viewUpdates, "apt list"},
		{viewBackups, "pvesh get /cluster/backup"},
	}
	for _, tt := range tests {
		t.Run(tt.view, func(t *testing.T) {
			runner := &fakeRunner{}
			m := newTestModel(runner)
			m.viewMode = tt.view
			m, cmd := press(t, m, "r")
			if !m.loading || cmd == nil {
				t.Fatalf("loading=%t cmd=%t", m.loading, cmd != nil)
			}
			cmd()
			if !runner.called(tt.want) {
				t.Fatalf("expected a call containing %q, got %#v", tt.want, runner.calls)
			}
		})
	}
}

func TestUpdateHelpModal(t *testing.T) {
	m, _ := press(t, newTestModel(&fakeRunner{}), "?")
	if m.modalMode != modalHelp {
		t.Fatalf("expected help modal, got %q", m.modalMode)
	}
	m, _ = press(t, m, "esc")
	if m.modalMode != modalNone {
		t.Fatalf("expected modal closed, got %q", m.modalMode)
	}
}

func TestUpdateStartupModalClosesOnEnter(t *testing.T) {
	m := NewWithStartup(proxmox.Client{}, true)
	m, cmd := press(t, m, "x")
	if m.modalMode != modalStartup || cmd != nil {
		t.Fatal("other keys must not close the startup modal")
	}
	m, _ = press(t, m, "enter")
	if m.modalMode != modalNone {
		t.Fatalf("expected modal closed, got %q", m.modalMode)
	}
}

func TestUpdateLoadMessagesKeepOtherSections(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.data.users = []proxmox.User{{UserID: "root@pam"}}
	m.data.updates = []proxmox.PackageUpdate{{Name: "libssl3"}}
	m.loading = true
	m.selected = 5

	m, _ = deliver(t, m, loadMsg(snapshot{guests: []proxmox.Guest{{VMID: 1}}}))
	if m.loading || len(m.data.guests) != 1 || len(m.data.users) != 1 || len(m.data.updates) != 1 {
		t.Fatalf("unexpected state loading=%t data=%#v", m.loading, m.data)
	}
	if m.selected != 0 {
		t.Fatalf("selection should be clamped, got %d", m.selected)
	}
}

func TestUpdateTick(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m, cmd := deliver(t, m, tickMsg{})
	if !m.loading || cmd == nil {
		t.Fatalf("tick should start a refresh: loading=%t cmd=%t", m.loading, cmd != nil)
	}
	m, cmd = deliver(t, m, tickMsg{})
	if !m.loading || cmd == nil {
		t.Fatal("tick while loading must still reschedule")
	}
}

func TestUpdateOpenGuestConfig(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"qemu/300/config": `{"memory":2048}`}}
	m := newTestModel(runner)

	m, cmd := press(t, m, "enter")
	if !m.showConfig || m.configVMID != 300 || cmd == nil {
		t.Fatalf("showConfig=%t vmid=%d", m.showConfig, m.configVMID)
	}
	m, _ = execute(t, m, cmd)
	if m.config == nil || m.configErr != nil || !strings.Contains(m.message, "config loaded for 300") {
		t.Fatalf("config=%v err=%v message=%q", m.config, m.configErr, m.message)
	}

	// A config for another guest is ignored.
	m, _ = deliver(t, m, configMsg{vmid: 999, err: errors.New("boom")})
	if m.configErr != nil {
		t.Fatal("stale config message must be ignored")
	}

	m, _ = press(t, m, "esc")
	if m.showConfig {
		t.Fatal("esc should close the config panel")
	}
}

func TestUpdateStartGuestConfirmFlow(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)

	m, _ = press(t, m, "+")
	if m.modalMode != modalConfirm || m.modalAction != "start-guest" || m.modalTargetVMID != 300 {
		t.Fatalf("mode=%q action=%q vmid=%d", m.modalMode, m.modalAction, m.modalTargetVMID)
	}
	m, _ = press(t, m, "esc")
	if m.modalMode != modalNone || len(runner.calls) != 0 {
		t.Fatalf("cancel must not run anything, calls=%#v", runner.calls)
	}

	m, _ = press(t, m, "+")
	m, cmd := press(t, m, "enter")
	if m.modalMode != modalNone || cmd == nil {
		t.Fatal("enter should confirm and run the action")
	}
	m, cmd = execute(t, m, cmd)
	if !runner.called("pvesh create /nodes/pve/qemu/300/status/start") {
		t.Fatalf("start not called: %#v", runner.calls)
	}
	if !strings.Contains(m.message, "start-guest completed") || cmd == nil {
		t.Fatalf("message=%q cmd=%t", m.message, cmd != nil)
	}
}

func TestUpdateStopGuestConfirmFlow(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)
	m, _ = press(t, m, "j", "-")
	if m.modalAction != "stop-guest" || m.modalTargetVMID != 301 {
		t.Fatalf("action=%q vmid=%d", m.modalAction, m.modalTargetVMID)
	}
	m, cmd := press(t, m, "enter")
	execute(t, m, cmd)
	if !runner.called("pvesh create /nodes/pve/qemu/301/status/stop") {
		t.Fatalf("stop not called: %#v", runner.calls)
	}
}

func TestUpdateGuestActionErrorIsShown(t *testing.T) {
	runner := &fakeRunner{failing: map[string]error{"status/start": errors.New("denied")}}
	m := newTestModel(runner)
	m, _ = press(t, m, "+")
	m, cmd := press(t, m, "enter")
	m, next := execute(t, m, cmd)
	// The real error is shown, not a misleading "guest not found".
	if next != nil || !strings.Contains(m.message, "denied") || strings.Contains(m.message, "not found") {
		t.Fatalf("message=%q next=%t", m.message, next != nil)
	}
}

func TestUpdateActionsIgnoredOutsideGuests(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewUsers
	m, _ = press(t, m, "+", "-")
	if m.modalMode != modalNone {
		t.Fatalf("start/stop must only work in guests view, got modal %q", m.modalMode)
	}
}

func TestUpdateSnapshotsFlow(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"qemu/300/snapshot": `[{"name":"snap1"},{"name":"current"}]`,
	}}
	m := newTestModel(runner)

	m, cmd := press(t, m, "s")
	if !m.showSnaps || m.snapshotVMID != 300 {
		t.Fatalf("showSnaps=%t vmid=%d", m.showSnaps, m.snapshotVMID)
	}
	m, _ = execute(t, m, cmd)
	if len(m.snapshots) != 2 || !strings.Contains(m.message, "2 snapshots loaded") {
		t.Fatalf("snapshots=%#v message=%q", m.snapshots, m.message)
	}

	// Snapshots of another guest are ignored.
	m, _ = deliver(t, m, snapshotsMsg{vmid: 999, snapshots: nil})
	if len(m.snapshots) != 2 {
		t.Fatal("stale snapshots message must be ignored")
	}

	m, _ = press(t, m, "d")
	if m.modalAction != "delete-snapshot" || m.modalInput != "snap1" {
		t.Fatalf("action=%q target=%q", m.modalAction, m.modalInput)
	}
	m, cmd = press(t, m, "enter")
	m, next := execute(t, m, cmd)
	if !runner.called("pvesh delete /nodes/pve/qemu/300/snapshot/snap1") {
		t.Fatalf("delete not called: %#v", runner.calls)
	}
	if next == nil || !strings.Contains(m.message, "delete-snapshot completed") {
		t.Fatalf("message=%q", m.message)
	}

	m, _ = press(t, m, "o")
	if m.modalAction != "restore-snapshot" {
		t.Fatalf("action=%q", m.modalAction)
	}
	m, cmd = press(t, m, "enter")
	execute(t, m, cmd)
	if !runner.called("snapshot/snap1/rollback") {
		t.Fatalf("rollback not called: %#v", runner.calls)
	}

	// The synthetic "current" entry can never be deleted or restored.
	m.selectedSnapshot = 1
	m, _ = press(t, m, "d")
	if m.modalMode != modalNone {
		t.Fatal("current must not be deletable")
	}
	m, _ = press(t, m, "o")
	if m.modalMode != modalNone {
		t.Fatal("current must not be restorable")
	}

	m, _ = press(t, m, "esc")
	if m.showSnaps {
		t.Fatal("esc should close snapshots")
	}
}

func TestUpdateCreateSnapshotRejectsInvalidName(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)

	m, _ = press(t, m, "n")
	if m.modalMode != modalSnapshot || m.modalTargetVMID != 300 {
		t.Fatalf("mode=%q vmid=%d", m.modalMode, m.modalTargetVMID)
	}
	m, _ = press(t, m, "b", "a", "d", " ", "n")
	// First enter jumps to the confirm field, the second one submits.
	m, _ = press(t, m, "enter")
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.modalMode != modalNone || !strings.Contains(m.message, "invalid snapshot name") {
		t.Fatalf("mode=%q message=%q cmd=%t", m.modalMode, m.message, cmd != nil)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid name must not reach pvesh: %#v", runner.calls)
	}
}

func TestUpdateCreateSnapshotRunsCommand(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)
	m, _ = press(t, m, "n")
	m, _ = press(t, m, "o", "k")
	m, _ = press(t, m, "enter")
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("expected a command, message=%q", m.message)
	}
	m, next := execute(t, m, cmd)
	if !runner.called("snapname ok") {
		t.Fatalf("snapshot not created: %#v", runner.calls)
	}
	if !m.showSnaps || next == nil || !strings.Contains(m.message, "snapshot ok created for 300") {
		t.Fatalf("showSnaps=%t message=%q", m.showSnaps, m.message)
	}
}

func TestUpdateModalCancelField(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)
	m, _ = press(t, m, "n")
	m, _ = press(t, m, "tab", "tab")
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.modalMode != modalNone || len(runner.calls) != 0 {
		t.Fatalf("cancel field must close the modal without running anything, mode=%q", m.modalMode)
	}
}

func TestUpdateRunBackupFlow(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"vzdump": `"` + testUPID + `"`}}
	m := newTestModel(runner)

	m, _ = press(t, m, "m")
	if m.modalMode != modalBackup || m.modalTargetVMID != 300 {
		t.Fatalf("mode=%q vmid=%d", m.modalMode, m.modalTargetVMID)
	}
	m, _ = press(t, m, "enter")
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("expected backup command, message=%q", m.message)
	}
	m, next := execute(t, m, cmd)
	if !runner.called("pvesh create /nodes/pve/vzdump -vmid 300") {
		t.Fatalf("vzdump not called: %#v", runner.calls)
	}
	if m.viewMode != viewBackups || next == nil || !strings.Contains(m.message, testUPID) {
		t.Fatalf("view=%q message=%q", m.viewMode, m.message)
	}
}

func TestUpdateBackupVolumesActions(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)
	m.viewMode = viewBackups
	m.data.backupVolumes = []proxmox.BackupVolume{
		{VMID: 300, VolID: "backup-nas:backup/vzdump-qemu-300.vma.zst", Format: "vma.zst"},
	}

	m, _ = press(t, m, "d")
	if m.modalAction != "delete-backup" {
		t.Fatalf("action=%q", m.modalAction)
	}
	m, cmd := press(t, m, "enter")
	m, next := execute(t, m, cmd)
	if !runner.called("pvesh delete /nodes/pve/storage/backup-nas/content/") {
		t.Fatalf("delete not called: %#v", runner.calls)
	}
	if next == nil || !strings.Contains(m.message, "delete-backup completed") {
		t.Fatalf("message=%q", m.message)
	}

	m, _ = press(t, m, "o")
	if m.modalAction != "restore-backup" || !strings.Contains(m.modalBody, "OVERWRITTEN") {
		t.Fatalf("action=%q body=%q", m.modalAction, m.modalBody)
	}
	m, cmd = press(t, m, "enter")
	execute(t, m, cmd)
	if !runner.called("qmrestore backup-nas:backup/vzdump-qemu-300.vma.zst 300 --force") {
		t.Fatalf("restore not called: %#v", runner.calls)
	}
}

func TestUpdateBackupJobScheduleFlow(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)
	m.viewMode = viewBackups
	m.backupMode = "jobs"
	m.data.backups = []proxmox.BackupJob{{ID: "daily", Storage: "backup-nas", Schedule: "02:30", VMID: "300"}}

	m, _ = press(t, m, "e")
	if m.modalMode != modalSchedule || m.modalJobID != "daily" || m.modalInput != "02:30" || m.modalVMIDs != "300" {
		t.Fatalf("mode=%q job=%q input=%q vmids=%q", m.modalMode, m.modalJobID, m.modalInput, m.modalVMIDs)
	}
	// Move to the schedule field, retype it, then jump to the confirm field.
	m, _ = press(t, m, "tab")
	m, _ = press(t, m, "backspace", "backspace", "backspace", "backspace", "backspace")
	m, _ = press(t, m, "0", "4", ":", "1", "5")
	if m.modalInput != "04:15" {
		t.Fatalf("schedule input=%q", m.modalInput)
	}
	m, _ = press(t, m, "enter")
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatalf("expected update command, message=%q", m.message)
	}
	m, next := execute(t, m, cmd)
	if !runner.called("pvesh set /cluster/backup/daily -schedule 04:15 -storage backup-nas -vmid 300") {
		t.Fatalf("job not updated: %#v", runner.calls)
	}
	if m.backupMode != "jobs" || next == nil || !strings.Contains(m.message, "scheduled 04:15") {
		t.Fatalf("message=%q", m.message)
	}
}

func TestUpdateScheduleRequiresValues(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.openModal(modalSchedule, 0, "daily", "backup-nas")
	m.modalInput = ""
	m.modalField = 3
	m, cmd := press(t, m, "enter")
	if cmd != nil || !strings.Contains(m.message, "schedule is required") {
		t.Fatalf("message=%q cmd=%t", m.message, cmd != nil)
	}

	m.openModal(modalSchedule, 0, "daily", "backup-nas")
	m.modalInput = "02:30"
	m.modalField = 3
	m, cmd = press(t, m, "enter")
	if cmd != nil || !strings.Contains(m.message, "VMIDs are required") {
		t.Fatalf("message=%q cmd=%t", m.message, cmd != nil)
	}
}

func TestUpdateScheduleGuestChecklist(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.openModal(modalSchedule, 0, "daily", "backup-nas")
	m.modalField = 2

	m, _ = press(t, m, " ")
	if m.modalVMIDs != "300" {
		t.Fatalf("vmids=%q", m.modalVMIDs)
	}
	m, _ = press(t, m, "down", " ")
	if m.modalVMIDs != "300,301" {
		t.Fatalf("vmids=%q", m.modalVMIDs)
	}
	m, _ = press(t, m, " ")
	if m.modalVMIDs != "300" {
		t.Fatalf("toggle off failed, vmids=%q", m.modalVMIDs)
	}
	m, _ = press(t, m, "up")
	if m.modalGuest != 0 {
		t.Fatalf("guest cursor=%d", m.modalGuest)
	}
	if label := m.modalGuestLabel(); !strings.Contains(label, "[x] 300") {
		t.Fatalf("label=%q", label)
	}
}

func TestUpdateModalNavigation(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.openModal(modalBackup, 300, "", "backup-nas")

	m, _ = press(t, m, "tab")
	if m.modalField != 1 {
		t.Fatalf("field=%d", m.modalField)
	}
	m, _ = press(t, m, "tab", "tab")
	if m.modalField != 0 {
		t.Fatalf("field should wrap, got %d", m.modalField)
	}
	m, _ = press(t, m, "left")
	if m.modalField != 0 {
		t.Fatalf("left on storage must not move field, got %d", m.modalField)
	}
	m.modalField = 1
	m, _ = press(t, m, "left")
	if m.modalField != 0 {
		t.Fatalf("field=%d", m.modalField)
	}
	m, _ = press(t, m, "esc")
	if m.modalMode != modalNone {
		t.Fatalf("esc should close modal, got %q", m.modalMode)
	}
}

func TestUpdateDisableTFAFlow(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"tfa list": `[]`}}
	m := newTestModel(runner)
	m.viewMode = viewUsers
	m.data.users = []proxmox.User{{UserID: "root@pam", TFAEnabled: true}, {UserID: "bob@pve", TFAEnabled: true}}

	m, cmd := press(t, m, "s")
	if m.tfaUserID != "root@pam" || cmd == nil {
		t.Fatalf("tfa user=%q", m.tfaUserID)
	}
	m, _ = execute(t, m, cmd)

	// esc cancels: nothing is disabled.
	m, cmd = press(t, m, "x")
	if m.modalMode != modalConfirm || m.modalAction != "disable-tfa" || m.modalInput != "root@pam" || cmd != nil {
		t.Fatalf("mode=%q action=%q target=%q", m.modalMode, m.modalAction, m.modalInput)
	}
	if !strings.Contains(m.modalBody, "root@pam") {
		t.Fatalf("the question must name the user: %q", m.modalBody)
	}
	m, cmd = press(t, m, "esc")
	if cmd != nil || m.modalMode != modalNone || runner.called("tfa delete") {
		t.Fatal("esc must not disable TFA")
	}
	// Any other key leaves the modal open and does nothing either.
	m, _ = press(t, m, "x")
	m, cmd = press(t, m, "y", "o", "tab")
	if cmd != nil || m.modalMode != modalConfirm || runner.called("tfa delete") {
		t.Fatal("only enter confirms")
	}

	m, cmd = press(t, m, "enter")
	if cmd == nil || m.modalMode != modalNone {
		t.Fatalf("expected disable command, message=%q", m.message)
	}
	m, next := execute(t, m, cmd)
	if !runner.called("pveum user tfa delete root@pam") {
		t.Fatalf("disable not called: %#v", runner.calls)
	}
	if next == nil || !strings.Contains(m.message, "TFA disabled for root@pam") {
		t.Fatalf("message=%q", m.message)
	}
	// The list shows it at once, without a refresh, and only for that user.
	if m.data.users[0].TFAEnabled || !m.data.users[1].TFAEnabled {
		t.Fatalf("users = %+v", m.data.users)
	}
	if len(m.tfaEntries) != 0 {
		t.Fatalf("stale entries = %v", m.tfaEntries)
	}
}

func TestUpdateUserSelectionAndLoad(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{
		"/access/users": `[{"userid":"root@pam"},{"userid":"bob@pve"}]`,
		"tfa list":      `[{"id":"1","type":"totp"}]`,
	}}
	m := newTestModel(runner)
	m, cmd := press(t, m, "u")
	m, _ = execute(t, m, cmd)
	if m.loading || len(m.data.users) != 2 {
		t.Fatalf("loading=%t users=%d", m.loading, len(m.data.users))
	}
	m, _ = press(t, m, "j")
	if m.selectedUser != 1 {
		t.Fatalf("selectedUser=%d", m.selectedUser)
	}
}

func TestUpdateAsyncResultMessages(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"snapshots error", snapshotsMsg{vmid: 0, err: boom}, "boom"},
		{"create snapshot error", createSnapshotMsg{err: boom}, "boom"},
		{"backup error", runBackupMsg{err: boom}, "boom"},
		{"schedule error", updateBackupScheduleMsg{err: boom}, "boom"},
		{"tfa list error", userTFAMsg{err: boom}, "boom"},
		{"tfa disable error", disableTFAMsg{err: boom}, "boom"},
		{"users error", usersMsg{err: boom}, "boom"},
		{"action error", destructiveActionMsg{action: "delete-backup", err: boom}, "boom"},
		{"tfa list ok", userTFAMsg{entries: []proxmox.TFAEntry{{ID: "1"}}}, "1 TFA entries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(&fakeRunner{})
			m, cmd := deliver(t, m, tt.msg)
			if !strings.Contains(m.message, tt.want) {
				t.Fatalf("message=%q, want %q", m.message, tt.want)
			}
			if strings.Contains(tt.name, "error") && cmd != nil {
				t.Fatal("an error must not trigger a follow-up command")
			}
		})
	}
}

func TestUpdateStateMessages(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.loading = true

	m, _ = deliver(t, m, updatesMsg{updates: []proxmox.PackageUpdate{{Name: "a"}}})
	if m.loading || len(m.data.updates) != 1 {
		t.Fatalf("loading=%t updates=%d", m.loading, len(m.data.updates))
	}
	m.loading = true
	m, _ = deliver(t, m, backupsMsg{jobs: []proxmox.BackupJob{{ID: "j"}}, volumes: []proxmox.BackupVolume{{VolID: "v"}}})
	if m.loading || len(m.data.backups) != 1 || len(m.data.backupVolumes) != 1 {
		t.Fatalf("loading=%t data=%#v", m.loading, m.data)
	}
}

func TestViewRendersEverySection(t *testing.T) {
	for _, view := range []string{viewGuests, viewUsers, viewUpdates, viewBackups} {
		for _, loading := range []bool{false, true} {
			m := newTestModel(&fakeRunner{})
			m.viewMode = view
			m.loading = loading
			m.width, m.height = 120, 40
			if out := m.View(); strings.TrimSpace(out) == "" {
				t.Fatalf("empty view for %s loading=%t", view, loading)
			}
		}
	}
}

func TestCreateSnapshotUsesLongTimeout(t *testing.T) {
	if snapshotTimeout < 5*time.Minute {
		t.Fatalf("snapshot creation with RAM needs a long timeout, got %s", snapshotTimeout)
	}
	if snapshotTimeout <= shortActionTimeout {
		t.Fatal("snapshot timeout must exceed the short action timeout")
	}
}

func TestUpgradeModalNamesNonInteractiveUpgrade(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.modalMode = modalUpgrade
	m.upgradeRunning = true
	m.upgradePhase = upgradeApply
	m.width, m.height = 120, 40
	if out := m.View(); !strings.Contains(out, "non-interactive") || !strings.Contains(out, "dist-upgrade") || strings.Contains(out, "apt-get -y dist-upgrade") {
		t.Fatal("upgrade modal should show the non-interactive dist-upgrade label")
	}
}

func TestUpdateRebootAndShutdownConfirmFlow(t *testing.T) {
	tests := []struct {
		key    string
		action string
		call   string
	}{
		{"R", "reboot-guest", "pvesh create /nodes/pve/qemu/301/status/reboot"},
		{"S", "shutdown-guest", "pvesh create /nodes/pve/qemu/301/status/shutdown"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			runner := &fakeRunner{}
			m := newTestModel(runner)
			m, _ = press(t, m, "j") // 301 is running

			m, _ = press(t, m, tt.key)
			if m.modalMode != modalConfirm || m.modalAction != tt.action || m.modalTargetVMID != 301 {
				t.Fatalf("mode=%q action=%q vmid=%d", m.modalMode, m.modalAction, m.modalTargetVMID)
			}
			m, _ = press(t, m, "esc")
			if len(runner.calls) != 0 {
				t.Fatalf("cancel must not run anything: %v", runner.calls)
			}

			m, _ = press(t, m, tt.key)
			m, cmd := press(t, m, "enter")
			m, next := execute(t, m, cmd)
			if !runner.called(tt.call) {
				t.Fatalf("%s not called: %v", tt.call, runner.calls)
			}
			if next == nil || !strings.Contains(m.message, tt.action+" completed") {
				t.Fatalf("message=%q", m.message)
			}
		})
	}
}

func TestUpdateRebootNeedsARunningGuestInGuestsView(t *testing.T) {
	m := newTestModel(&fakeRunner{})

	m, _ = press(t, m, "R") // 300 is stopped
	if m.modalMode != modalNone || !strings.Contains(m.message, "300 is not running") {
		t.Fatalf("mode=%q message=%q", m.modalMode, m.message)
	}
	m, _ = press(t, m, "S")
	if m.modalMode != modalNone {
		t.Fatalf("mode=%q", m.modalMode)
	}

	m.viewMode = viewUsers
	m, _ = press(t, m, "j", "R", "S")
	if m.modalMode != modalNone {
		t.Fatal("R and S only work in the guests view")
	}
}

func TestUpdateRebootTaskIsTracked(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"status/reboot": `"` + testUPID + `"`}}
	m := newTestModel(runner)
	m, _ = press(t, m, "j", "R")
	m, cmd := press(t, m, "enter")
	m, next := execute(t, m, cmd)
	if len(m.tasks) != 1 || next == nil || !strings.Contains(m.message, "reboot-guest started") {
		t.Fatalf("tasks=%v message=%q", m.tasks, m.message)
	}
}

func TestHelpAndFooterMentionRebootAndShutdown(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.modalMode = modalHelp
	m.width, m.height = 140, 60
	out := stripANSI(m.View())
	for _, want := range []string{"reboot (clean)", "shut down (clean)"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q", want)
		}
	}

	// The footer shows what applies to the selected guest.
	m.modalMode = modalNone
	m, _ = press(t, m, "j") // 301 is running
	if footer := m.footerText(); !strings.Contains(footer, "R reboot") || !strings.Contains(footer, "S shutdown") || !strings.Contains(footer, "- stop") {
		t.Errorf("a running guest offers stop, shutdown and reboot: %q", footer)
	}
	m, _ = press(t, m, "k") // 300 is stopped
	if footer := m.footerText(); !strings.Contains(footer, "+ start") || strings.Contains(footer, "R reboot") {
		t.Errorf("a stopped guest only offers start: %q", footer)
	}
}

func TestLoadingTFAEntriesSyncsTheIndicator(t *testing.T) {
	m := newTestModel(&fakeRunner{})
	m.viewMode = viewUsers
	m.data.users = []proxmox.User{{UserID: "a@pve"}, {UserID: "b@pve", TFAEnabled: true}, {UserID: "c@pve", TFAEnabled: true}}

	m, _ = deliver(t, m, userTFAMsg{userid: "a@pve", entries: []proxmox.TFAEntry{{ID: "1", Type: "totp"}}})
	m, _ = deliver(t, m, userTFAMsg{userid: "b@pve", entries: nil})
	// A failed read says nothing: the indicator stays.
	m, _ = deliver(t, m, userTFAMsg{userid: "c@pve", err: errors.New("boom")})
	got := []bool{m.data.users[0].TFAEnabled, m.data.users[1].TFAEnabled, m.data.users[2].TFAEnabled}
	if got[0] != true || got[1] != false || got[2] != true {
		t.Fatalf("indicators = %v", got)
	}
}

func TestUpdateGuestAgentFlow(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"/qemu/300/config": `{"agent":"enabled=0,fstrim_cloned_disks=0,type=virtio"}`}}
	m := newTestModel(runner)

	guest, ok := m.selectedGuest()
	if !ok || guest.VMID != 300 {
		t.Fatalf("selected guest = %+v", guest)
	}
	m, cmd := press(t, m, "A")
	if cmd == nil || runner.called("config -agent") {
		t.Fatal("A reads the option, it changes nothing")
	}
	m, _ = execute(t, m, cmd)
	if m.modalMode != modalConfirm || m.modalAction != "enable-agent" || m.modalTargetVMID != 300 {
		t.Fatalf("mode=%q action=%q vmid=%d", m.modalMode, m.modalAction, m.modalTargetVMID)
	}
	if !strings.Contains(m.modalBody, "300") || !strings.Contains(m.modalBody, "must be stopped") ||
		!strings.Contains(m.modalBody, "dnf install qemu-guest-agent") || !strings.Contains(m.modalBody, "apt install qemu-guest-agent") {
		t.Fatalf("body = %q", m.modalBody)
	}

	// esc cancels: nothing is written.
	m, cmd = press(t, m, "esc")
	if cmd != nil || m.modalMode != modalNone || runner.called("config -agent") {
		t.Fatal("esc must not change the agent")
	}

	m, cmd = press(t, m, "A")
	m, _ = execute(t, m, cmd)
	m, cmd = press(t, m, "enter")
	if cmd == nil || m.modalMode != modalNone {
		t.Fatalf("expected the set command, message=%q", m.message)
	}
	m, _ = execute(t, m, cmd)
	if !runner.called("pvesh set /nodes/pve/qemu/300/config -agent enabled=1,fstrim_cloned_disks=0,type=virtio") {
		t.Fatalf("calls = %#v", runner.calls)
	}
	if !strings.Contains(m.message, "guest agent enabled for 300") {
		t.Fatalf("message = %q", m.message)
	}
}

func TestUpdateGuestAgentOfAnEnabledVMAsksToDisable(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"/qemu/300/config": `{"agent":"1"}`}}
	m := newTestModel(runner)
	m, cmd := press(t, m, "A")
	m, _ = execute(t, m, cmd)
	if m.modalAction != "disable-agent" {
		t.Fatalf("action = %q", m.modalAction)
	}
}

func TestUpdateGuestAgentRefusesAContainer(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestModel(runner)
	m, _ = press(t, m, "down", "down")
	if guest, _ := m.selectedGuest(); guest.Type != proxmox.GuestTypeLXC {
		t.Fatalf("selected guest = %+v", guest)
	}
	m, cmd := press(t, m, "A")
	if cmd != nil || m.modalMode != modalNone || len(runner.calls) != 0 {
		t.Fatalf("a container has no agent: calls=%v", runner.calls)
	}
	if !strings.Contains(m.message, "QEMU") {
		t.Fatalf("message = %q", m.message)
	}
}
